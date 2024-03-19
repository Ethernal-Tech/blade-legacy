package evm

import (
	"errors"
	"math/big"
	"strings"

	"sync"

	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/helper/hex"
	"github.com/0xPolygon/polygon-edge/state/runtime"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/holiman/uint256"
)

var statePool = sync.Pool{
	New: func() interface{} {
		return new(state)
	},
}

func acquireState() *state {
	aquiredState, ok := statePool.Get().(*state)
	if !ok {
		return nil
	}

	return aquiredState
}

func releaseState(s *state) {
	s.reset()
	statePool.Put(s)
}

const stackSize = 1024

var (
	errOutOfGas              = runtime.ErrOutOfGas
	errRevert                = runtime.ErrExecutionReverted
	errGasUintOverflow       = errors.New("gas uint64 overflow")
	errWriteProtection       = errors.New("write protection")
	errInvalidJump           = errors.New("invalid jump destination")
	errOpCodeNotFound        = errors.New("opcode not found")
	errReturnDataOutOfBounds = errors.New("return data out of bounds")
)

type state struct {
	ip   int
	code []byte
	tmp  []byte

	host   runtime.Host
	msg    *runtime.Contract // change with msg
	config *chain.ForksInTime

	// memory
	memory      []byte
	lastGasCost uint64

	// stack
	stack OptimizedStack

	err  error
	stop bool

	gas                uint64
	currentConsumedGas uint64

	// bitvec bitvec
	bitmap bitmap

	returnData []byte
	ret        []byte
}

func (c *state) reset() {
	c.ip = 0
	c.gas = 0
	c.currentConsumedGas = 0
	c.lastGasCost = 0
	c.stop = false
	c.err = nil

	// reset bitmap
	c.bitmap.reset()

	// reset memory
	for i := range c.memory {
		c.memory[i] = 0
	}

	c.stack.reset()
	c.tmp = c.tmp[:0]
	c.ret = c.ret[:0]
	c.code = c.code[:0]
	// c.returnData = c.returnData[:0]
	c.memory = c.memory[:0]
}

func (c *state) validJumpdest(dest uint256.Int) bool {
	udest, overflow := dest.Uint64WithOverflow()

	if overflow || udest >= uint64(len(c.code)) {
		return false
	}

	return c.bitmap.isSet(udest)
}

func (c *state) Halt() {
	c.stop = true
}

func (c *state) exit(err error) {
	if err == nil {
		return
	}

	c.stop = true
	c.err = err
}

func (c *state) push(val uint256.Int) {
	c.stack.push(val)
}

func (c *state) stackAtLeast(n int) bool {
	return c.stack.sp >= n
}

func (c *state) popHash() types.Hash {
	v := c.pop()

	return types.BytesToHash(v.Bytes())
}

func (c *state) popAddr() (types.Address, bool) {
	b, err := c.stack.pop()
	if err != nil {
		return types.Address{}, false
	}

	return types.BytesToAddress(b.Bytes()), true
}

func (c *state) stackSize() int {
	return c.stack.sp
}

func (c *state) top() *uint256.Int {
	v, _ := c.stack.top()

	return v
}

func (c *state) pop() uint256.Int {
	v, _ := c.stack.pop()

	return v
}

func (c *state) peekAt(n int) uint256.Int {
	return c.stack.peekAt(n)
}

func (c *state) swap(n int) {
	c.stack.swap(n)
}

func (c *state) consumeGas(gas uint64) bool {
	c.currentConsumedGas += gas

	if c.gas < gas {
		c.exit(errOutOfGas)

		return false
	}

	c.gas -= gas

	return true
}

func (c *state) resetReturnData() {
	c.returnData = c.returnData[:0]
}

// Run executes the virtual machine
func (c *state) Run() ([]byte, error) {
	var (
		vmerr error

		op OpCode
		ok bool
	)

	tracer := c.host.GetTracer()

	for !c.stop {
		op, ok = c.CurrentOpCode()
		gasCopy, ipCopy := c.gas, uint64(c.ip)

		if tracer != nil {
			c.captureState(int(op))
		}

		if !ok {
			c.Halt()

			break
		}

		inst := dispatchTable[op]
		if inst.inst == nil {
			c.exit(errOpCodeNotFound)
			c.captureExecution(op.String(), uint64(c.ip), gasCopy, 0)

			break
		}

		// check if the depth of the stack is enough for the instruction
		if c.stack.sp < inst.stack {
			c.exit(&runtime.StackUnderflowError{StackLen: c.stack.sp, Required: inst.stack})
			c.captureExecution(op.String(), uint64(c.ip), gasCopy, inst.gas)

			break
		}

		// consume the gas of the instruction
		if !c.consumeGas(inst.gas) {
			c.exit(errOutOfGas)
			c.captureExecution(op.String(), uint64(c.ip), gasCopy, inst.gas)

			break
		}

		// execute the instruction
		inst.inst(c)

		if tracer != nil {
			c.captureExecution(op.String(), ipCopy, gasCopy, gasCopy-c.gas)
		}
		// check if stack size exceeds the max size
		if c.stack.sp > stackSize {
			c.exit(&runtime.StackOverflowError{StackLen: c.stack.sp, Limit: stackSize})

			break
		}

		c.ip++
	}

	if err := c.err; err != nil {
		vmerr = err
	}

	return c.ret, vmerr
}

func (c *state) inStaticCall() bool {
	return c.msg.Static
}

func uint256ToHash(b *uint256.Int) types.Hash {
	return types.BytesToHash(b.Bytes())
}

func bigToHash(b *big.Int) types.Hash {
	return types.BytesToHash(b.Bytes())
}

func (c *state) Len() int {
	return len(c.memory)
}

// allocateMemory allocates memory to enable accessing in the range of [offset, offset+size]
// throws error if the given offset and size are negative
// consumes gas if memory needs to be expanded
func (c *state) allocateMemory(offset, size uint256.Int) bool {
	if !offset.IsUint64() || !size.IsUint64() {
		c.exit(errReturnDataOutOfBounds)

		return false
	}

	if size.Sign() == 0 {
		return true
	}

	o := offset.Uint64()
	s := size.Uint64()

	if o > 0xffffffffe0 || s > 0xffffffffe0 {
		c.exit(errReturnDataOutOfBounds)

		return false
	}

	if newSize, currentSize := o+s, uint64(len(c.memory)); currentSize < newSize {
		w := (newSize + 31) / 32
		newCost := 3*w + w*w/512
		cost := newCost - c.lastGasCost
		c.lastGasCost = newCost

		if !c.consumeGas(cost) {
			c.exit(errOutOfGas)

			return false
		}

		// resize the memory
		c.memory = common.ExtendByteSlice(c.memory, int(w*32))
	}

	return true
}

func (c *state) get2(dst []byte, offset, length uint256.Int) ([]byte, bool) {
	if length.Sign() == 0 {
		return nil, true
	}

	if !c.allocateMemory(offset, length) {
		return nil, false
	}

	o := offset.Uint64()
	l := length.Uint64()

	dst = append(dst, c.memory[o:o+l]...)

	return dst, true
}

func (c *state) Show() string {
	str := []string{}

	for i := 0; i < len(c.memory); i += 16 {
		j := i + 16
		if j > len(c.memory) {
			j = len(c.memory)
		}

		str = append(str, hex.EncodeToHex(c.memory[i:j]))
	}

	return strings.Join(str, "\n")
}

func (c *state) CurrentOpCode() (OpCode, bool) {
	if codeSize := len(c.code); c.ip >= codeSize {
		return STOP, false
	}

	return OpCode(c.code[c.ip]), true
}

func (c *state) captureState(opCode int) {
	tracer := c.host.GetTracer()
	if tracer == nil {
		return
	}

	bigIntArray := make([]*big.Int, 0, len(c.stack.data))

	for _, num := range c.stack.data {
		// Convert uint256 to *big.Int and append to bigIntArray as temp solution
		bigNum := num.ToBig() // Adjust conversion based on your uint256 implementation
		bigIntArray = append(bigIntArray, bigNum)
	}

	tracer.CaptureState(
		c.memory,
		bigIntArray,
		opCode,
		c.msg.Address,
		c.stack.sp,
		c.host,
		c,
	)
}

func (c *state) captureExecution(
	opCode string,
	ip uint64,
	gas uint64,
	consumedGas uint64,
) {
	tracer := c.host.GetTracer()
	if tracer == nil {
		return
	}

	tracer.ExecuteState(
		c.msg.Address,
		ip,
		opCode,
		gas,
		consumedGas,
		c.returnData,
		c.msg.Depth,
		c.err,
		c.host,
	)
}
