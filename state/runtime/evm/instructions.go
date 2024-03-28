//nolint:forcetypeassert
package evm

import (
	"errors"
	"math/big"
	"math/bits"
	"sync"

	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/helper/keccak"
	"github.com/0xPolygon/polygon-edge/state/runtime"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/holiman/uint256"
)

type instruction func(c *state)

const (
	ColdAccountAccessCostEIP2929 = uint64(2600) // COLD_ACCOUNT_ACCESS_COST
	ColdStorageReadCostEIP2929   = uint64(2100) // COLD_SLOAD_COST_EIP2929
	WarmStorageReadCostEIP2929   = uint64(100)  // WARM_STORAGE_READ_COST
)

var (
	zero        = big.NewInt(0)
	one         = big.NewInt(1)
	wordSize    = big.NewInt(32)
	wordSize256 = uint256.NewInt(32)
)

func equalOrOverflowsUint256(b *big.Int) bool {
	return b.BitLen() > 8
}

var bufPool = sync.Pool{
	New: func() interface{} {
		// Store pointer to avoid heap allocation in caller
		// Please check SA6002 in StaticCheck for details
		buf := make([]byte, 128)

		return &buf
	},
}

var (
	_W = bits.UintSize
	_S = _W / 8
)

func min(i, j uint64) uint64 {
	if i < j {
		return i
	}

	return j
}

func (c *state) calculateGasForEIP2929(addr types.Address) uint64 {
	var gas uint64
	if c.host.ContainsAccessListAddress(addr) {
		gas = WarmStorageReadCostEIP2929
	} else {
		gas = ColdAccountAccessCostEIP2929

		c.host.AddAddressToAccessList(addr)
	}

	return gas
}

// Generic WriteToSlice function that calls optimized function when
// applicable or generic one.
func WriteToSlice(z uint256.Int, dest []byte) {
	if len(dest) == 32 {
		WriteToSlice32(z, dest)
	} else {
		z.WriteToSlice(dest)
	}
}

// Optimized write to slice when destination size is 32 bytes. This way
// the CPU does not execute code in loop achieving greater paralelization
func WriteToSlice32(z uint256.Int, dest []byte) {
	dest[31] = byte(z[0] >> uint64(8*0))
	dest[30] = byte(z[0] >> uint64(8*1))
	dest[29] = byte(z[0] >> uint64(8*2))
	dest[28] = byte(z[0] >> uint64(8*3))
	dest[27] = byte(z[0] >> uint64(8*4))
	dest[26] = byte(z[0] >> uint64(8*5))
	dest[25] = byte(z[0] >> uint64(8*6))
	dest[24] = byte(z[0] >> uint64(8*7))
	dest[23] = byte(z[1] >> uint64(8*0))
	dest[22] = byte(z[1] >> uint64(8*1))
	dest[21] = byte(z[1] >> uint64(8*2))
	dest[20] = byte(z[1] >> uint64(8*3))
	dest[19] = byte(z[1] >> uint64(8*4))
	dest[18] = byte(z[1] >> uint64(8*5))
	dest[17] = byte(z[1] >> uint64(8*6))
	dest[16] = byte(z[1] >> uint64(8*7))
	dest[15] = byte(z[2] >> uint64(8*0))
	dest[14] = byte(z[2] >> uint64(8*1))
	dest[13] = byte(z[2] >> uint64(8*2))
	dest[12] = byte(z[2] >> uint64(8*3))
	dest[11] = byte(z[2] >> uint64(8*4))
	dest[10] = byte(z[2] >> uint64(8*5))
	dest[9] = byte(z[2] >> uint64(8*6))
	dest[8] = byte(z[2] >> uint64(8*7))
	dest[7] = byte(z[3] >> uint64(8*0))
	dest[6] = byte(z[3] >> uint64(8*1))
	dest[5] = byte(z[3] >> uint64(8*2))
	dest[4] = byte(z[3] >> uint64(8*3))
	dest[3] = byte(z[3] >> uint64(8*4))
	dest[2] = byte(z[3] >> uint64(8*5))
	dest[1] = byte(z[3] >> uint64(8*6))
	dest[0] = byte(z[3] >> uint64(8*7))
}

func opAdd(c *state) {
	a := c.pop()
	b := c.top()

	b.Add(&a, b)
}

func opMul(c *state) {
	a := c.pop()
	b := c.top()

	b.Mul(&a, b)
}

func opSub(c *state) {
	a := c.pop()
	b := c.top()

	b.Sub(&a, b)
}

func opDiv(c *state) {
	a := c.pop()
	b := c.top()

	b.Div(&a, b)
}

func opSDiv(c *state) {
	a := c.pop()
	b := c.top()

	b.SDiv(&a, b)
}

func opMod(c *state) {
	a := c.pop()
	b := c.top()

	b.Mod(&a, b)
}

func opSMod(c *state) {
	a := c.pop()
	b := c.top()

	b.SMod(&a, b)
}

func opExp(c *state) {
	x := c.pop()
	y := c.top()

	var gas uint64
	if c.config.EIP158 {
		gas = 50
	} else {
		gas = 10
	}

	gasCost := uint64((y.BitLen()+7)/8) * gas
	if !c.consumeGas(gasCost) {
		return
	}

	y.Exp(&x, y)
}

func opAddMod(c *state) {
	a := c.pop()
	b := c.pop()
	z := c.top()

	z.AddMod(&a, &b, z)
}

func opMulMod(c *state) {
	a := c.pop()
	b := c.pop()
	z := c.top()

	z.MulMod(&a, &b, z)
}

func opAnd(c *state) {
	a := c.pop()
	b := c.top()

	b.And(&a, b)
}

func opOr(c *state) {
	a := c.pop()
	b := c.top()

	b.Or(&a, b)
}

func opXor(c *state) {
	a := c.pop()
	b := c.top()

	b.Xor(&a, b)
}

func opByte(c *state) {
	x := c.pop()
	y := c.top()

	y.Byte(&x)
}

func opNot(c *state) {
	a := c.top()
	a.Not(a)
}

func opIsZero(c *state) {
	a := c.top()

	if a.IsZero() {
		a.SetOne()
	} else {
		a.SetUint64(0)
	}
}

func opEq(c *state) {
	a := c.pop()
	b := c.top()

	if a.Eq(b) {
		b.SetOne()
	} else {
		b.SetUint64(0)
	}
}

func opLt(c *state) {
	a := c.pop()
	b := c.top()

	if a.Lt(b) {
		b.SetOne()
	} else {
		b.SetUint64(0)
	}
}

func opGt(c *state) {
	a := c.pop()
	b := c.top()

	if a.Gt(b) {
		b.SetOne()
	} else {
		b.SetUint64(0)
	}
}

func opSlt(c *state) {
	a := c.pop()
	b := c.top()

	if a.Slt(b) {
		b.SetOne()
	} else {
		b.SetUint64(0)
	}
}

func opSgt(c *state) {
	a := c.pop()
	b := c.top()

	if a.Sgt(b) {
		b.SetOne()
	} else {
		b.SetUint64(0)
	}
}

func opSignExtension(c *state) {
	ext := c.pop()
	x := c.top()

	x.ExtendSign(x, &ext)
}

func opShl(c *state) {
	if !c.config.Constantinople {
		c.exit(errOpCodeNotFound)

		return
	}

	shift := c.pop()
	value := c.top()

	if shift.LtUint64(256) {
		value.Lsh(value, uint(shift.Uint64()))
	} else {
		value.Clear()
	}
}

func opShr(c *state) {
	if !c.config.Constantinople {
		c.exit(errOpCodeNotFound)

		return
	}

	shift := c.pop()
	value := c.top()

	if shift.LtUint64(256) {
		value.Rsh(value, uint(shift.Uint64()))
	} else {
		value.Clear()
	}
}

func opSar(c *state) {
	if !c.config.Constantinople {
		c.exit(errOpCodeNotFound)

		return
	}

	shift := c.pop()
	value := c.top()

	// This seams like unneeded since uint256 is maximum value
	if equalOrOverflowsUint256(shift.ToBig()) {
		if value.Sign() >= 0 {
			value.SetUint64(0)
		} else {
			value.SetAllOne()
		}
	} else {
		value.SRsh(value, uint(shift.Uint64()))
	}
}

// memory operations
func opMLoad(c *state) {
	v := c.top()

	var ok bool
	c.tmp, ok = c.get2(c.tmp[:0], *v, *wordSize256)

	// ### Error handling?
	if !ok {
		return
	}

	v.SetBytes(c.tmp)
}

func opMStore(c *state) {
	offset := c.pop()
	val := c.pop()

	if !c.allocateMemory(offset, *wordSize256) {
		return
	}

	o := offset.Uint64()

	WriteToSlice(val, c.memory[o:o+32])
}

func opMStore8(c *state) {
	offset := c.pop()
	val := c.pop()

	if !c.allocateMemory(offset, *uint256.NewInt(1)) {
		return
	}

	c.memory[offset.Uint64()] = byte(val.Uint64() & 0xff)
}

// --- storage ---

func opSload(c *state) {
	loc := c.top()

	var gas uint64

	if c.config.Berlin {
		storageKey := uint256ToHash(loc)
		if _, slotPresent := c.host.ContainsAccessListSlot(c.msg.Address, storageKey); !slotPresent {
			gas = ColdStorageReadCostEIP2929

			c.host.AddSlotToAccessList(c.msg.Address, storageKey)
		} else {
			gas = WarmStorageReadCostEIP2929
		}
	} else if c.config.Istanbul {
		// eip-1884
		gas = 800
	} else if c.config.EIP150 {
		gas = 200
	} else {
		gas = 50
	}

	if !c.consumeGas(gas) {
		return
	}

	val := c.host.GetStorage(c.msg.Address, uint256ToHash(loc))
	loc.SetBytes(val.Bytes())
}

func opSStore(c *state) {
	if c.inStaticCall() {
		c.exit(errWriteProtection)

		return
	}

	if c.config.Istanbul && c.gas <= 2300 {
		c.exit(errOutOfGas)

		return
	}

	key := c.popHash()
	val := c.popHash()

	legacyGasMetering := !c.config.Istanbul && (c.config.Petersburg || !c.config.Constantinople)

	status := c.host.SetStorage(c.msg.Address, key, val, c.config)
	cost := uint64(0)

	if c.config.Berlin {
		if _, slotPresent := c.host.ContainsAccessListSlot(c.msg.Address, key); !slotPresent {
			cost = ColdStorageReadCostEIP2929

			c.host.AddSlotToAccessList(c.msg.Address, key)
		}
	}

	switch status {
	case runtime.StorageUnchanged:
		if c.config.Berlin {
			cost += WarmStorageReadCostEIP2929
		} else if c.config.Istanbul {
			// eip-2200
			cost += 800
		} else if legacyGasMetering {
			cost += 5000
		} else {
			cost += 200
		}

	case runtime.StorageModified:
		cost += 5000
		if c.config.Berlin {
			cost -= ColdStorageReadCostEIP2929
		}

	case runtime.StorageModifiedAgain:
		if c.config.Berlin {
			cost += WarmStorageReadCostEIP2929
		} else if c.config.Istanbul {
			// eip-2200
			cost += 800
		} else if legacyGasMetering {
			cost += 5000
		} else {
			cost += 200
		}

	case runtime.StorageAdded:
		cost += 20000

	case runtime.StorageDeleted:
		cost += 5000
		if c.config.Berlin {
			cost -= ColdStorageReadCostEIP2929
		}
	}

	if !c.consumeGas(cost) {
		return
	}
}

const sha3WordGas uint64 = 6

func opSha3(c *state) {
	offset := c.pop()
	length := c.pop()

	var ok bool
	if c.tmp, ok = c.get2(c.tmp[:0], offset, length); !ok {
		return
	}

	size := length.Uint64()
	if !c.consumeGas(((size + 31) / 32) * sha3WordGas) {
		return
	}

	c.tmp = keccak.Keccak256(c.tmp[:0], c.tmp)

	v := uint256.Int{0}
	v.SetBytes(c.tmp)
	c.push(v)
}

func opPop(c *state) {
	c.pop()
}

// context operations

func opAddress(c *state) {
	v := uint256.Int{0}
	v.SetBytes(c.msg.Address.Bytes())
	c.push(v)
}

func opBalance(c *state) {
	addr, _ := c.popAddr()

	var gas uint64
	if c.config.Berlin {
		gas = c.calculateGasForEIP2929(addr)
	} else if c.config.Istanbul {
		// eip-1884
		gas = 700
	} else if c.config.EIP150 {
		gas = 400
	} else {
		gas = 20
	}

	if !c.consumeGas(gas) {
		return
	}

	balance := c.host.GetBalance(addr)
	uintBalance, _ := uint256.FromBig(balance)

	c.push(*uintBalance)
}

func opSelfBalance(c *state) {
	if !c.config.Istanbul {
		c.exit(errOpCodeNotFound)

		return
	}

	balance := c.host.GetBalance(c.msg.Address)
	uintBalance, _ := uint256.FromBig(balance)

	c.push(*uintBalance)
}

func opChainID(c *state) {
	if !c.config.Istanbul {
		c.exit(errOpCodeNotFound)

		return
	}

	x := uint256.NewInt(uint64(c.host.GetTxContext().ChainID))

	c.push(*x)
}

func opOrigin(c *state) {
	x := uint256.Int{0}
	x.SetBytes(c.host.GetTxContext().Origin.Bytes())

	c.push(x)
}

func opCaller(c *state) {
	x := uint256.Int{0}
	x.SetBytes(c.msg.Caller.Bytes())

	c.push(x)
}

func opCallValue(c *state) {
	if value := c.msg.Value; value != nil {
		uintValue, _ := uint256.FromBig(value)
		c.push(*uintValue)
	} else {
		c.push(uint256.Int{0})
	}
}

func opCallDataLoad(c *state) {
	offset := c.top()

	bufPtr := bufPool.Get().(*[]byte)
	buf := *bufPtr
	c.setBytes(buf[:32], c.msg.Input, 32, *offset)
	offset.SetBytes(buf[:32])
	bufPool.Put(bufPtr)
}

func opCallDataSize(c *state) {
	x := uint256.NewInt(uint64(len(c.msg.Input)))
	c.push(*x)
}

func opCodeSize(c *state) {
	x := uint256.NewInt(uint64(len(c.code)))
	c.push(*x)
}

func opExtCodeSize(c *state) {
	addr, _ := c.popAddr()

	var gas uint64
	if c.config.Berlin {
		gas = c.calculateGasForEIP2929(addr)
	} else if c.config.EIP150 {
		gas = 700
	} else {
		gas = 20
	}

	if !c.consumeGas(gas) {
		return
	}

	x := uint256.NewInt(uint64(c.host.GetCodeSize(addr)))
	c.push(*x)
}

func opGasPrice(c *state) {
	x := uint256.Int{0}
	x.SetBytes(c.host.GetTxContext().GasPrice.Bytes())
	c.push(x)
}

func opReturnDataSize(c *state) {
	if !c.config.Byzantium {
		c.exit(errOpCodeNotFound)
	} else {
		x := uint256.NewInt(uint64(len(c.returnData)))
		c.push(*x)
	}
}

func opExtCodeHash(c *state) {
	if !c.config.Constantinople {
		c.exit(errOpCodeNotFound)

		return
	}

	address, _ := c.popAddr()

	var gas uint64
	if c.config.Berlin {
		gas = c.calculateGasForEIP2929(address)
	} else if c.config.Istanbul {
		gas = 700
	} else {
		gas = 400
	}

	if !c.consumeGas(gas) {
		return
	}

	v := uint256.Int{0}
	if !c.host.Empty(address) {
		v.SetBytes(c.host.GetCodeHash(address).Bytes())
	}

	c.push(v)
}

func opPC(c *state) {
	c.push(*uint256.NewInt(uint64(c.ip)))
}

func opMSize(c *state) {
	c.push(*uint256.NewInt(uint64(len(c.memory))))
}

func opGas(c *state) {
	c.push(*uint256.NewInt(c.gas))
}

func (c *state) setBytes(dst, input []byte, size uint64, dataOffset uint256.Int) {
	if !dataOffset.IsUint64() {
		// overflow, copy 'size' 0 bytes to dst
		for i := uint64(0); i < size; i++ {
			dst[i] = 0
		}

		return
	}

	inputSize := uint64(len(input))
	begin := min(dataOffset.Uint64(), inputSize)

	copySize := min(size, inputSize-begin)
	if copySize > 0 {
		copy(dst, input[begin:begin+copySize])
	}

	if size-copySize > 0 {
		dst = dst[copySize:]
		for i := uint64(0); i < size-copySize; i++ {
			dst[i] = 0
		}
	}
}

const copyGas uint64 = 3

func opExtCodeCopy(c *state) {
	address, _ := c.popAddr()
	memOffset := c.pop()
	codeOffset := c.pop()
	length := c.pop()

	if !c.allocateMemory(memOffset, length) {
		return
	}

	size := length.Uint64()
	if !c.consumeGas(((size + 31) / 32) * copyGas) {
		return
	}

	var gas uint64
	if c.config.Berlin {
		gas = c.calculateGasForEIP2929(address)
	} else if c.config.EIP150 {
		gas = 700
	} else {
		gas = 20
	}

	if !c.consumeGas(gas) {
		return
	}

	code := c.host.GetCode(address)
	if size != 0 {
		c.setBytes(c.memory[memOffset.Uint64():], code, size, codeOffset)
	}
}

func opCallDataCopy(c *state) {
	memOffset := c.pop()
	dataOffset := c.pop()
	length := c.pop()

	if !c.allocateMemory(memOffset, length) {
		return
	}

	size := length.Uint64()
	if !c.consumeGas(((size + 31) / 32) * copyGas) {
		return
	}

	if size != 0 {
		c.setBytes(c.memory[memOffset.Uint64():], c.msg.Input, size, dataOffset)
	}
}

func opReturnDataCopy(c *state) {
	if !c.config.Byzantium {
		c.exit(errOpCodeNotFound)

		return
	}

	var (
		memOffset  = c.pop()
		dataOffset = c.pop()
		length     = c.pop()
	)

	// Check if:
	// 1. the dataOffset is uint64 (overflow check)
	// 2. the sum of dataOffset and length overflows uint64
	// 3. the length of return data has enough space to receive offset + length bytes
	end := new(big.Int).Add(dataOffset.ToBig(), length.ToBig())
	endAddress := end.Uint64()

	if !dataOffset.IsUint64() ||
		!end.IsUint64() ||
		uint64(len(c.returnData)) < endAddress {
		c.exit(errReturnDataOutOfBounds)

		return
	}

	// if length is 0, return immediately since no need for the data copying nor memory allocation
	if length.Sign() == 0 {
		return
	}

	if !c.allocateMemory(memOffset, length) {
		// Error code is set inside the allocateMemory call
		return
	}

	ulength := length.Uint64()
	if !c.consumeGas(((ulength + 31) / 32) * copyGas) {
		// Error code is set inside the consumeGas
		return
	}

	data := c.returnData[dataOffset.Uint64():endAddress]
	copy(c.memory[memOffset.Uint64():memOffset.Uint64()+ulength], data)
}

func opCodeCopy(c *state) {
	memOffset := c.pop()
	dataOffset := c.pop()
	length := c.pop()

	if length.Uint64() <= 0 {
		return
	}

	if !c.allocateMemory(memOffset, length) {
		return
	}

	size := length.Uint64()
	if !c.consumeGas(((size + 31) / 32) * copyGas) {
		return
	}

	if size != 0 {
		c.setBytes(c.memory[memOffset.Uint64():], c.code, size, dataOffset)
	}
}

// block information

func opBlockHash(c *state) {
	num := c.top()

	num64, overflow := num.Uint64WithOverflow()

	if overflow {
		num.SetUint64(0)

		return
	}

	n := int64(num64)
	lastBlock := c.host.GetTxContext().Number

	if lastBlock-257 < n && n < lastBlock {
		num.SetBytes(c.host.GetBlockHash(n).Bytes())
	} else {
		num.SetUint64(0)
	}
}

func opCoinbase(c *state) {
	v := new(uint256.Int).SetBytes20(c.host.GetTxContext().Coinbase.Bytes())
	c.push(*v)
}

func opTimestamp(c *state) {
	v := new(uint256.Int).SetUint64(uint64(c.host.GetTxContext().Timestamp))
	c.push(*v)
}

func opNumber(c *state) {
	v := new(uint256.Int).SetUint64((uint64)(c.host.GetTxContext().Number))
	c.push(*v)
}

func opDifficulty(c *state) {
	v := new(uint256.Int).SetBytes(c.host.GetTxContext().Difficulty.Bytes())
	c.push(*v)
}

func opGasLimit(c *state) {
	v := new(uint256.Int).SetUint64((uint64)(c.host.GetTxContext().GasLimit))
	c.push(*v)
}

func opBaseFee(c *state) {
	if !c.config.London {
		c.exit(errOpCodeNotFound)

		return
	}

	c.push(*uint256.MustFromBig(c.host.GetTxContext().BaseFee))
}

func opSelfDestruct(c *state) {
	if c.inStaticCall() {
		c.exit(errWriteProtection)

		return
	}

	address, _ := c.popAddr()

	// try to remove the gas first
	var gas uint64

	// EIP150 reprice fork
	if c.config.EIP150 {
		gas = 5000

		if c.config.EIP158 {
			// if empty and transfers value
			if c.host.Empty(address) && c.host.GetBalance(c.msg.Address).Sign() != 0 {
				gas += 25000
			}
		} else if !c.host.AccountExists(address) {
			gas += 25000
		}
	}

	// EIP 2929 gas
	if c.config.Berlin && !c.host.ContainsAccessListAddress(address) {
		gas += ColdAccountAccessCostEIP2929

		c.host.AddAddressToAccessList(address)
	}

	if !c.consumeGas(gas) {
		return
	}

	c.host.Selfdestruct(c.msg.Address, address)
	c.Halt()
}

func opJump(c *state) {
	dest := c.pop()
	if c.validJumpdest(dest) {
		c.ip = int(dest.Uint64() - 1)
	} else {
		c.exit(errInvalidJump)
	}
}

func opJumpi(c *state) {
	dest := c.pop()
	cond := c.pop()

	if cond.Sign() != 0 {
		if c.validJumpdest(dest) {
			c.ip = int(dest.Uint64() - 1)
		} else {
			c.exit(errInvalidJump)
		}
	}
}

func opJumpDest(c *state) {
}

func opPush0(c *state) {
	if !c.config.EIP3855 {
		c.exit(errOpCodeNotFound)

		return
	}

	c.push(uint256.Int{0})
}

func opPush(n int) instruction {
	return func(c *state) {
		ins := c.code
		ip := c.ip

		d := uint256.Int{0}
		if ip+1+n > len(ins) {
			d.SetBytes(append(ins[ip+1:], make([]byte, n)...))
		} else {
			d.SetBytes(ins[ip+1 : ip+1+n])
		}

		c.push(d)

		c.ip += n
	}
}

func opDup(n int) instruction {
	return func(c *state) {
		if !c.stackAtLeast(n) {
			c.exit(&runtime.StackUnderflowError{StackLen: c.stack.sp, Required: n})
		} else {
			val := c.peekAt(n)
			c.push(val)
		}
	}
}

func opSwap(n int) instruction {
	return func(c *state) {
		if !c.stackAtLeast(n + 1) {
			c.exit(&runtime.StackUnderflowError{StackLen: c.stack.sp, Required: n + 1})
		} else {
			c.swap(n)
		}
	}
}

func opLog(size int) instruction {
	size = size - 1

	return func(c *state) {
		if c.inStaticCall() {
			c.exit(errWriteProtection)

			return
		}

		if !c.stackAtLeast(2 + size) {
			c.exit(&runtime.StackUnderflowError{StackLen: c.stack.sp, Required: 2 + size})

			return
		}

		mStart := c.pop()
		mSize := c.pop()

		topics := make([]types.Hash, size)
		for i := 0; i < size; i++ {
			v := c.pop()
			topics[i] = bigToHash(v.ToBig())
		}

		var ok bool

		c.tmp, ok = c.get2(c.tmp[:0], mStart, mSize)
		if !ok {
			return
		}

		c.host.EmitLog(c.msg.Address, topics, c.tmp)

		if !c.consumeGas(uint64(size) * 375) {
			return
		}

		if !c.consumeGas(mSize.Uint64() * 8) {
			return
		}
	}
}

func opStop(c *state) {
	c.Halt()
}

func opCreate(op OpCode) instruction {
	return func(c *state) {
		if c.inStaticCall() {
			c.exit(errWriteProtection)

			return
		}

		if op == CREATE2 {
			if !c.config.Constantinople {
				c.exit(errOpCodeNotFound)

				return
			}
		}

		// reset the return data
		c.resetReturnData()

		contract, err := c.buildCreateContract(op)
		if err != nil {
			c.push(uint256.Int{0})

			if contract != nil {
				c.gas += contract.Gas
			}

			return
		}

		if contract == nil {
			return
		}

		contract.Type = runtime.Create

		// Correct call
		result := c.host.Callx(contract, c.host)

		v := uint256.Int{0}
		if op == CREATE && c.config.Homestead && errors.Is(result.Err, runtime.ErrCodeStoreOutOfGas) {
			v.SetUint64(0)
		} else if op == CREATE && result.Failed() && !errors.Is(result.Err, runtime.ErrCodeStoreOutOfGas) {
			v.SetUint64(0)
		} else if op == CREATE2 && result.Failed() {
			v.SetUint64(0)
		} else {
			v.SetBytes(contract.Address.Bytes())
		}

		c.push(v)
		c.gas += result.GasLeft

		if result.Reverted() {
			c.returnData = append(c.returnData[:0], result.ReturnValue...)
		}
	}
}

func opCall(op OpCode) instruction {
	return func(c *state) {
		c.resetReturnData()

		if op == CALL && c.inStaticCall() {
			if val := c.peekAt(3); val.BitLen() > 0 {
				c.exit(errWriteProtection)

				return
			}
		}

		if op == DELEGATECALL && !c.config.Homestead {
			c.exit(errOpCodeNotFound)

			return
		}

		if op == STATICCALL && !c.config.Byzantium {
			c.exit(errOpCodeNotFound)

			return
		}

		var callType runtime.CallType

		switch op {
		case CALL:
			callType = runtime.Call

		case CALLCODE:
			callType = runtime.CallCode

		case DELEGATECALL:
			callType = runtime.DelegateCall

		case STATICCALL:
			callType = runtime.StaticCall

		default:
			panic("not expected") //nolint:gocritic
		}

		contract, offset, size, err := c.buildCallContract(op)
		if err != nil {
			c.push(uint256.Int{0})

			if contract != nil {
				c.gas += contract.Gas
			}

			return
		}

		if contract == nil {
			return
		}

		contract.Type = callType

		result := c.host.Callx(contract, c.host)

		if result.Succeeded() {
			c.push(*uint256.NewInt(1))
		} else {
			c.push(uint256.Int{0})
		}

		if result.Succeeded() || result.Reverted() {
			if len(result.ReturnValue) != 0 && size > 0 {
				copy(c.memory[offset:offset+size], result.ReturnValue)
			}
		}

		c.gas += result.GasLeft
		c.returnData = append(c.returnData[:0], result.ReturnValue...)
	}
}

func (c *state) buildCallContract(op OpCode) (*runtime.Contract, uint64, uint64, error) {
	// Pop input arguments
	initialGas := c.pop()
	addr, _ := c.popAddr()

	var value *big.Int
	if op == CALL || op == CALLCODE {
		v := c.pop()
		value = v.ToBig()
	}

	// input range
	inOffset := c.pop()
	inSize := c.pop()

	// output range
	retOffset := c.pop()
	retSize := c.pop()

	// Get the input arguments
	args, ok := c.get2(nil, inOffset, inSize)
	if !ok {
		return nil, 0, 0, nil
	}
	// Check if the memory return offsets are out of bounds
	if !c.allocateMemory(retOffset, retSize) {
		return nil, 0, 0, nil
	}

	var gasCost uint64
	if c.config.Berlin {
		gasCost = c.calculateGasForEIP2929(addr)
	} else if c.config.EIP150 {
		gasCost = 700
	} else {
		gasCost = 40
	}

	transfersValue := (op == CALL || op == CALLCODE) && value != nil && value.Sign() != 0

	if op == CALL {
		if c.config.EIP158 {
			if transfersValue && c.host.Empty(addr) {
				gasCost += 25000
			}
		} else if !c.host.AccountExists(addr) {
			gasCost += 25000
		}
	}

	if transfersValue {
		gasCost += 9000
	}

	var gas uint64

	ok = initialGas.IsUint64()

	if c.config.EIP150 {
		availableGas := c.gas - gasCost
		availableGas = availableGas - availableGas/64

		if !ok || availableGas < initialGas.Uint64() {
			gas = availableGas
		} else {
			gas = initialGas.Uint64()
		}
	} else {
		if !ok {
			c.exit(errOutOfGas)

			return nil, 0, 0, nil
		}
		gas = initialGas.Uint64()
	}

	gasCostTmp, isOverflow := common.SafeAddUint64(gasCost, gas)
	if isOverflow {
		c.exit(errGasUintOverflow)

		return nil, 0, 0, nil
	}

	gasCost = gasCostTmp

	// Consume gas cost
	if !c.consumeGas(gasCost) {
		return nil, 0, 0, nil
	}

	if transfersValue {
		gas += 2300
	}

	parent := c

	contract := runtime.NewContractCall(
		c.msg.Depth+1,
		parent.msg.Origin,
		parent.msg.Address,
		addr,
		value,
		gas,
		c.host.GetCode(addr),
		args,
	)

	if op == STATICCALL || parent.msg.Static {
		contract.Static = true
	}

	if op == CALLCODE || op == DELEGATECALL {
		contract.Address = parent.msg.Address
		if op == DELEGATECALL {
			contract.Value = parent.msg.Value
			contract.Caller = parent.msg.Caller
		}
	}

	if transfersValue {
		if c.host.GetBalance(c.msg.Address).Cmp(value) < 0 {
			return contract, 0, 0, types.ErrInsufficientFunds
		}
	}

	return contract, retOffset.Uint64(), retSize.Uint64(), nil
}

func (c *state) buildCreateContract(op OpCode) (*runtime.Contract, error) {
	// Pop input arguments
	value := c.pop()
	offset := c.pop()
	length := c.pop()

	var salt *big.Int
	if op == CREATE2 {
		v := c.pop()
		salt = v.ToBig()
	}

	// check if the value can be transferred
	hasTransfer := value.Sign() != 0

	// Calculate and consume gas cost

	// Both CREATE and CREATE2 use memory
	var input []byte

	var ok bool

	input, ok = c.get2(input[:0], offset, length) // Does the memory check
	if !ok {
		return nil, nil
	}

	if op == CREATE2 {
		// Consume sha3 gas cost
		size := length.Uint64()
		if !c.consumeGas(((size + 31) / 32) * sha3WordGas) {
			return nil, nil
		}
	}

	if hasTransfer {
		if c.host.GetBalance(c.msg.Address).Cmp(value.ToBig()) < 0 {
			return nil, types.ErrInsufficientFunds
		}
	}

	// Calculate and consume gas for the call
	gas := c.gas

	// CREATE2 uses by default EIP150
	if c.config.EIP150 || op == CREATE2 {
		gas -= gas / 64
	}

	if !c.consumeGas(gas) {
		return nil, nil
	}

	// Calculate address
	var address types.Address
	if op == CREATE {
		address = crypto.CreateAddress(c.msg.Address, c.host.GetNonce(c.msg.Address))
	} else {
		address = crypto.CreateAddress2(c.msg.Address, bigToHash(salt), input)
	}

	contract := runtime.NewContractCreation(
		c.msg.Depth+1,
		c.msg.Origin,
		c.msg.Address,
		address,
		value.ToBig(),
		gas,
		input,
	)

	return contract, nil
}

func opHalt(op OpCode) instruction {
	return func(c *state) {
		if op == REVERT && !c.config.Byzantium {
			c.exit(errOpCodeNotFound)

			return
		}

		offset := c.pop()
		size := c.pop()

		var ok bool
		c.ret, ok = c.get2(c.ret[:0], offset, size)

		if !ok {
			return
		}

		if op == REVERT {
			c.exit(errRevert)
		} else {
			c.Halt()
		}
	}
}

var (
	tt256   = new(big.Int).Lsh(big.NewInt(1), 256)   // 2 ** 256
	tt256m1 = new(big.Int).Sub(tt256, big.NewInt(1)) // 2 ** 256 - 1
)

func toU256(x *big.Int) *big.Int {
	if x.Sign() < 0 || x.BitLen() > 256 {
		x.And(x, tt256m1)
	}

	return x
}

func to256(x *big.Int) *big.Int {
	if x.BitLen() > 255 {
		x.Sub(x, tt256)
	}

	return x
}
