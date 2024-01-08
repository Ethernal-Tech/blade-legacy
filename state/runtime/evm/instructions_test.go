package evm

import (
	"math"
	"math/big"
	"testing"

	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/state/runtime"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var (
	two   = big.NewInt(2)
	three = big.NewInt(3)
	four  = big.NewInt(4)
	five  = big.NewInt(5)

	allEnabledForks = chain.AllForksEnabled.At(0)
)

type OperandsLogical []struct {
	operands       []*big.Int
	expectedResult bool
}

func testLogicalOperation(t *testing.T, f instruction, tests OperandsLogical) {
	t.Helper()

	s, closeFn := getState()
	defer closeFn()

	for _, i := range tests {
		for _, operand := range i.operands {
			s.push(operand)
		}

		f(s)

		if i.expectedResult {
			assert.Equal(t, uint64(1), s.pop().Uint64())
		} else {
			assert.Equal(t, uint64(0), s.pop().Uint64())
		}
	}
}

type OperandsArithmetic []struct {
	operands       []*big.Int
	expectedResult *big.Int
}

func testArithmeticOperation(t *testing.T, f instruction, tests OperandsArithmetic) {
	t.Helper()

	s, closeFn := getState()
	defer closeFn()

	s.config = &chain.ForksInTime{Constantinople: true}

	for _, i := range tests {
		for _, operand := range i.operands {
			s.push(operand)
		}

		f(s)

		assert.EqualValues(t, i.expectedResult.Uint64(), s.pop().Uint64())
	}
}

func TestAdd(t *testing.T) {
	testArithmeticOperation(t, opAdd, OperandsArithmetic{
		{[]*big.Int{one, one}, two},
		{[]*big.Int{zero, one}, one},
	})
}

func TestMul(t *testing.T) {
	testArithmeticOperation(t, opMul, OperandsArithmetic{
		{[]*big.Int{two, two}, big.NewInt(4)},
		{[]*big.Int{three, two}, big.NewInt(6)},
	})
}

func TestSub(t *testing.T) {
	testArithmeticOperation(t, opSub, OperandsArithmetic{
		{[]*big.Int{one, two}, one},
		{[]*big.Int{zero, two}, two},
	})
}

func TestDiv(t *testing.T) {
	testArithmeticOperation(t, opDiv, OperandsArithmetic{
		{[]*big.Int{two, two}, one},
		{[]*big.Int{one, two}, two},
		{[]*big.Int{one, zero}, zero},
	})
}

func TestSDiv(t *testing.T) {
	testArithmeticOperation(t, opSDiv, OperandsArithmetic{
		{[]*big.Int{two, two}, one},
		{[]*big.Int{one, two}, two},
		{[]*big.Int{zero, one}, zero},
	})
}

func TestMod(t *testing.T) {
	testArithmeticOperation(t, opMod, OperandsArithmetic{
		{[]*big.Int{two, three}, one},
		{[]*big.Int{two, two}, zero},
		{[]*big.Int{one, three}, zero},
	})
}

func TestSMod(t *testing.T) {
	testArithmeticOperation(t, opSMod, OperandsArithmetic{
		{[]*big.Int{two, three}, one},
		{[]*big.Int{two, two}, zero},
		{[]*big.Int{one, three}, zero},
	})
}

func TestAddMod(t *testing.T) {
	testArithmeticOperation(t, opAddMod, OperandsArithmetic{
		{[]*big.Int{three, one, two}, zero},
		{[]*big.Int{two, one, two}, one},
	})
}

func TestMulMod(t *testing.T) {
	testArithmeticOperation(t, opMulMod, OperandsArithmetic{
		{[]*big.Int{three, two, four}, two},
		{[]*big.Int{two, two, four}, zero},
	})
}

func TestOpAnd(t *testing.T) {
	testLogicalOperation(t, opAnd, OperandsLogical{
		{[]*big.Int{one, one}, true},
		{[]*big.Int{one, zero}, false},
		{[]*big.Int{zero, one}, false},
		{[]*big.Int{zero, zero}, false},
	})
}

func TestOpOr(t *testing.T) {
	testLogicalOperation(t, opOr, OperandsLogical{
		{[]*big.Int{one, one}, true},
		{[]*big.Int{one, zero}, true},
		{[]*big.Int{zero, one}, true},
		{[]*big.Int{zero, zero}, false},
	})
}

func TestXor(t *testing.T) {
	testLogicalOperation(t, opXor, OperandsLogical{
		{[]*big.Int{one, one}, false},
		{[]*big.Int{one, zero}, true},
		{[]*big.Int{zero, one}, true},
		{[]*big.Int{zero, zero}, false},
	})
}

func TestByte(t *testing.T) {
	testArithmeticOperation(t, opByte, OperandsArithmetic{
		{[]*big.Int{one, big.NewInt(31)}, one},
		{[]*big.Int{five, big.NewInt(31)}, five},
		{[]*big.Int{two, big.NewInt(32)}, zero},
		{[]*big.Int{one, big.NewInt(30)}, zero},
	})
}

func TestShl(t *testing.T) {
	testArithmeticOperation(t, opShl, OperandsArithmetic{
		{[]*big.Int{three, one}, big.NewInt(6)},
		{[]*big.Int{three, zero}, three},
	})
}

func TestShr(t *testing.T) {
	testArithmeticOperation(t, opShr, OperandsArithmetic{
		{[]*big.Int{five, one}, two},
		{[]*big.Int{five, two}, one},
		{[]*big.Int{five, zero}, five},
	})
}

func TestSar(t *testing.T) {
	testArithmeticOperation(t, opSar, OperandsArithmetic{
		{[]*big.Int{five, one}, two},
		{[]*big.Int{five, two}, one},
		{[]*big.Int{five, zero}, five},
	})
}

func TestPush0(t *testing.T) {
	t.Run("single push0 success", func(t *testing.T) {
		s, closeFn := getState()
		s.config = &allEnabledForks
		defer closeFn()

		opPush0(s)
		assert.Equal(t, zero, s.pop())
	})

	t.Run("single push0 (EIP-3855 disabled)", func(t *testing.T) {
		s, closeFn := getState()
		disabledEIP3855Fork := chain.AllForksEnabled.Copy().RemoveFork(chain.EIP3855).At(0)
		s.config = &disabledEIP3855Fork
		defer closeFn()

		opPush0(s)
		assert.Error(t, errOpCodeNotFound, s.err)
	})

	t.Run("within stack size push0", func(t *testing.T) {
		s, closeFn := getState()
		s.config = &allEnabledForks
		defer closeFn()

		for i := 0; i < stackSize; i++ {
			opPush0(s)
			require.NoError(t, s.err)
		}

		for i := 0; i < stackSize; i++ {
			require.Equal(t, zero, s.pop())
		}
	})
}

func TestGt(t *testing.T) {
	testLogicalOperation(t, opGt, OperandsLogical{
		{[]*big.Int{one, one}, false},
		{[]*big.Int{two, one}, false},
		{[]*big.Int{one, two}, true},
	})
}

func TestLt(t *testing.T) {
	testLogicalOperation(t, opLt, OperandsLogical{
		{[]*big.Int{one, one}, false},
		{[]*big.Int{two, one}, true},
		{[]*big.Int{one, two}, false},
	})
}

func TestEq(t *testing.T) {
	testLogicalOperation(t, opEq, OperandsLogical{
		{[]*big.Int{zero, zero}, true},
		{[]*big.Int{one, zero}, false},
		{[]*big.Int{zero, one}, false},
		{[]*big.Int{one, one}, true},
	})
}

func TestSlt(t *testing.T) {
	testLogicalOperation(t, opSlt, OperandsLogical{
		{[]*big.Int{one, one}, false},
		{[]*big.Int{zero, one}, false},
		{[]*big.Int{one, zero}, true},
	})
}

func TestSgt(t *testing.T) {
	testLogicalOperation(t, opSgt, OperandsLogical{
		{[]*big.Int{one, one}, false},
		{[]*big.Int{zero, one}, true},
		{[]*big.Int{one, zero}, false},
	})
}

func TestIsZero(t *testing.T) {
	testLogicalOperation(t, opIsZero, OperandsLogical{
		{[]*big.Int{one, one}, false},
		{[]*big.Int{zero, zero}, true},
		{[]*big.Int{two, two}, false},
	})
}

func TestMStore(t *testing.T) {
	s, closeFn := getState()
	defer closeFn()

	s.push(big.NewInt(10))   // value
	s.push(big.NewInt(1024)) // offset

	s.gas = 1000
	opMStore(s)

	assert.Len(t, s.memory, 1024+32)
}

func TestMStore8(t *testing.T) {
	s, closeFn := getState()
	defer closeFn()

	s.push(big.NewInt(10))
	s.push(big.NewInt(1024))

	s.gas = 1000
	opMStore8(s)

	assert.Len(t, s.memory, 1056)
}

func TestBalance(t *testing.T) {
	s, cancelFn := getState()
	defer cancelFn()

	t.Run("Istanbul", func(t *testing.T) {
		gasLeft := uint64(300)
		s.config = &chain.ForksInTime{
			Istanbul: true,
		}
		mockHost := mockHost{}
		mockHost.On("GetBalance", mock.Anything).Return(big.NewInt(100))

		s.host = &mockHost
		s.gas = 1000
		opBalance(s)

		assert.Equal(t, int64(100), s.pop().Int64())
		assert.Equal(t, gasLeft, s.gas)
	})

	t.Run("EIP150", func(t *testing.T) {
		gasLeft := uint64(600)
		s.config = &chain.ForksInTime{
			EIP150: true,
		}
		mockHost := mockHost{}
		mockHost.On("GetBalance", mock.Anything).Return(big.NewInt(100))

		s.host = &mockHost
		s.gas = 1000
		opBalance(s)

		assert.Equal(t, int64(100), s.pop().Int64())
		assert.Equal(t, gasLeft, s.gas)
	})

	t.Run("OtherForks", func(t *testing.T) {
		gasLeft := uint64(980)

		s.config = &chain.ForksInTime{
			London: true,
		}
		mockHost := mockHost{}
		mockHost.On("GetBalance", mock.Anything).Return(big.NewInt(100))

		s.host = &mockHost
		s.gas = 1000
		opBalance(s)

		assert.Equal(t, int64(100), s.pop().Int64())
		assert.Equal(t, gasLeft, s.gas)
	})
}

func TestSelfBalance(t *testing.T) {
	s, cancelFn := getState()
	defer cancelFn()

	s.config = &chain.ForksInTime{
		Istanbul: true,
	}
	mockHost := mockHost{}
	mockHost.On("GetBalance", mock.Anything).Return(big.NewInt(100)).Once()

	s.msg = &runtime.Contract{Address: types.StringToAddress("0x1")}
	s.host = &mockHost
	s.gas = 1000
	opSelfBalance(s)

	assert.Equal(t, int64(100), s.pop().Int64())
}

func TestChainID(t *testing.T) {
	s, cancelFn := getState()
	defer cancelFn()

	s.config = &chain.ForksInTime{
		Istanbul: true,
	}
	mockHost := mockHost{}
	mockHost.On("GetTxContext").Return(runtime.TxContext{ChainID: 4}).Once()

	s.host = &mockHost
	s.gas = 1000
	opChainID(s)

	assert.Equal(t, int64(4), s.pop().Int64())
}

func TestOrigin(t *testing.T) {
	s, cancelFn := getState()
	defer cancelFn()

	mockHost := mockHost{}
	mockHost.On("GetTxContext").Return(runtime.TxContext{Origin: types.StringToAddress("0x1")}).Once()

	s.host = &mockHost
	s.gas = 1000
	opOrigin(s)
	addr, ok := s.popAddr()
	assert.True(t, ok)
	assert.Equal(t, types.StringToAddress("0x1").Bytes(), addr.Bytes())
}

func TestCaller(t *testing.T) {
	s, cancelFn := getState()
	defer cancelFn()

	s.msg = &runtime.Contract{Caller: types.StringToAddress("0x1")}
	opCaller(s)

	addr, ok := s.popAddr()
	assert.True(t, ok)
	assert.Equal(t, types.StringToAddress("0x1").Bytes(), addr.Bytes())
}

func TestCallValue(t *testing.T) {
	s, cancelFn := getState()
	defer cancelFn()

	t.Run("Msg Value non nil", func(t *testing.T) {
		s.msg = &runtime.Contract{Value: big.NewInt(10)}

		opCallValue(s)
		assert.Equal(t, big.NewInt(10), s.pop())
	})

	t.Run("Msg Value nil", func(t *testing.T) {
		s.msg = &runtime.Contract{}

		opCallValue(s)
		assert.Equal(t, uint64(0), s.pop().Uint64())
	})
}

func TestCallDataSize(t *testing.T) {
	s, cancelFn := getState()
	defer cancelFn()

	s.msg = &runtime.Contract{Input: make([]byte, 10)}

	opCallDataSize(s)
	assert.Equal(t, uint64(10), s.pop().Uint64())
}

func TestCodeSize(t *testing.T) {
	s, cancelFn := getState()
	defer cancelFn()

	s.code = make([]byte, 10)

	opCodeSize(s)
	assert.Equal(t, uint64(10), s.pop().Uint64())
}

func TestExtCodeSize(t *testing.T) {
	s, cancelFn := getState()
	defer cancelFn()

	t.Run("EIP150", func(t *testing.T) {
		gasLeft := uint64(300)
		s.config = &chain.ForksInTime{
			EIP150: true,
		}
		s.push(one)

		mockHost := mockHost{}
		mockHost.On("GetCodeSize", types.StringToAddress("0x1")).Return(10).Once()

		s.host = &mockHost
		s.gas = 1000

		opExtCodeSize(s)

		assert.Equal(t, gasLeft, s.gas)
		assert.Equal(t, uint64(10), s.pop().Uint64())
	})
	t.Run("NoForks", func(t *testing.T) {
		gasLeft := uint64(980)

		s.push(one)

		s.config = &chain.ForksInTime{EIP150: false}

		mockHost := mockHost{}
		mockHost.On("GetCodeSize", types.StringToAddress("0x1")).Return(10).Once()

		s.host = &mockHost
		s.gas = 1000

		opExtCodeSize(s)

		assert.Equal(t, gasLeft, s.gas)
		assert.Equal(t, uint64(10), s.pop().Uint64())
	})
}

func TestGasPrice(t *testing.T) {
	s, cancelFn := getState()
	defer cancelFn()

	mockHost := mockHost{}
	mockHost.On("GetTxContext").Return(runtime.TxContext{GasPrice: bigToHash(big.NewInt(10))}).Once()

	s.host = &mockHost

	opGasPrice(s)

	assert.Equal(t, bigToHash(big.NewInt(10)), s.popHash())
}

func TestReturnDataSize(t *testing.T) {
	s, cancelFn := getState()
	defer cancelFn()

	s.config = &chain.ForksInTime{
		Byzantium: true,
	}

	s.returnData = make([]byte, 1024)

	opReturnDataSize(s)

	assert.Equal(t, uint64(1024), s.pop().Uint64())
}

func TestExtCodeHash(t *testing.T) {
	s, cancelFn := getState()
	defer cancelFn()

	t.Run("Istanbul", func(t *testing.T) {
		gasLeft := uint64(300)
		s.config = &chain.ForksInTime{
			Constantinople: true,
			Istanbul:       true,
		}

		s.push(big.NewInt(1))

		s.gas = 1000

		mockHost := mockHost{}
		mockHost.On("Empty", types.StringToAddress("0x1")).Return(false).Once()
		mockHost.On("GetCodeHash", types.StringToAddress("0x1")).Return("0x1").Once()

		s.host = &mockHost

		opExtCodeHash(s)

		assert.Equal(t, s.gas, gasLeft)
		assert.Equal(t, uint64(1), s.pop().Uint64())
	})

	t.Run("NonIstanbul", func(t *testing.T) {
		gasLeft := uint64(600)
		s.config = &chain.ForksInTime{
			Constantinople: true,
		}

		s.push(big.NewInt(1))

		s.gas = 1000

		mockHost := mockHost{}
		mockHost.On("Empty", mock.Anything).Return(true).Once()

		s.host = &mockHost

		opExtCodeHash(s)
		assert.Equal(t, gasLeft, s.gas)
		assert.Equal(t, zero.Int64(), s.pop().Int64())
	})
}

func TestPCMSizeGas(t *testing.T) {
	s, cancelFn := getState()
	defer cancelFn()

	t.Run("PC", func(t *testing.T) {
		s.ip = 1
		opPC(s)

		assert.Equal(t, uint64(1), s.pop().Uint64())
	})

	t.Run("MSize", func(t *testing.T) {
		s.memory = make([]byte, 1024)

		opMSize(s)

		assert.Equal(t, uint64(1024), s.pop().Uint64())
	})

	t.Run("Gas", func(t *testing.T) {
		s.gas = 1000

		opGas(s)

		assert.Equal(t, uint64(1000), s.pop().Uint64())
	})
}

func TestExtCodeCopy(t *testing.T) {
	s, cancelFn := getState()
	defer cancelFn()
	t.Run("EIP150", func(t *testing.T) {
		leftGas := uint64(294)
		s.config = &chain.ForksInTime{
			EIP150: true,
		}
		mockHost := mockHost{}
		mockHost.On("GetCode", mock.Anything).Return("0x1").Once()

		s.host = &mockHost

		s.push(one)
		s.push(zero)
		s.push(big.NewInt(31))
		s.push(big.NewInt(32))

		s.gas = 1000

		opExtCodeCopy(s)

		assert.Equal(t, leftGas, s.gas)
		assert.Equal(t, big.NewInt(1).FillBytes(make([]byte, 32)), s.memory)
	})

	t.Run("NonEIP150Fork", func(t *testing.T) {
		leftGas := uint64(977)
		s.config = &chain.ForksInTime{
			EIP150: false,
		}
		mockHost := mockHost{}
		mockHost.On("GetCode", mock.Anything).Return("0x1").Once()

		s.host = &mockHost

		s.push(one)
		s.push(zero)
		s.push(big.NewInt(31))
		s.push(big.NewInt(32))

		s.gas = 1000

		opExtCodeCopy(s)

		assert.Equal(t, leftGas, s.gas)
		assert.Equal(t, big.NewInt(1).FillBytes(make([]byte, 32)), s.memory)
	})
}

func TestCallDataCopy(t *testing.T) {
	gasLeft := uint64(994)

	s, cancelFn := getState()
	defer cancelFn()

	s.push(big.NewInt(1))
	s.push(zero)
	s.push(big.NewInt(31))

	s.gas = 1000
	s.msg = &runtime.Contract{
		Input: big.NewInt(1).Bytes(),
	}

	opCallDataCopy(s)

	assert.Equal(t, gasLeft, s.gas)
	assert.Equal(t, big.NewInt(1).FillBytes(make([]byte, 32)), s.memory)
}

func TestCodeCopy(t *testing.T) {
	s, cancelFn := getState()
	defer cancelFn()

	s.push(big.NewInt(1))  //length
	s.push(zero)           //dataOffset
	s.push(big.NewInt(31)) //memOffset

	s.code = big.NewInt(1).Bytes()

	s.gas = 1000
	opCodeCopy(s)
	assert.Equal(t, big.NewInt(1).FillBytes(make([]byte, 32)), s.memory)
}

func TestBlockHash(t *testing.T) {
	s, cancelFn := getState()
	defer cancelFn()

	s.push(three)

	mockHost := mockHost{}
	mockHost.On("GetTxContext").Return(runtime.TxContext{Number: 5}).Once()
	mockHost.On("GetBlockHash", mock.Anything).Return(3).Once()

	s.host = &mockHost

	opBlockHash(s)

	assert.Equal(t, bigToHash(three), bigToHash(s.pop()))
}

func TestCoinBase(t *testing.T) {
	s, cancelFn := getState()
	defer cancelFn()

	mockHost := mockHost{}
	mockHost.On("GetTxContext").Return(runtime.TxContext{Coinbase: types.StringToAddress("0x1")}).Once()
	s.host = &mockHost

	opCoinbase(s)

	assert.Equal(t, types.StringToAddress("0x1").Bytes(), s.pop().FillBytes(make([]byte, 20)))
}

func TestTimeStamp(t *testing.T) {
	s, cancelFn := getState()
	defer cancelFn()

	mockHost := mockHost{}
	mockHost.On("GetTxContext").Return(runtime.TxContext{Timestamp: 335}).Once()
	s.host = &mockHost

	opTimestamp(s)

	assert.Equal(t, uint64(335), s.pop().Uint64())
}

func TestNumber(t *testing.T) {
	s, cancelFn := getState()
	defer cancelFn()

	mockHost := mockHost{}
	mockHost.On("GetTxContext").Return(runtime.TxContext{Number: 5}).Once()
	s.host = &mockHost

	opNumber(s)

	assert.Equal(t, uint64(5), s.pop().Uint64())
}

func TestDifficulty(t *testing.T) {
	s, cancelFn := getState()
	defer cancelFn()

	mockHost := mockHost{}
	mockHost.On("GetTxContext").Return(runtime.TxContext{Difficulty: bigToHash(five)}).Once()
	s.host = &mockHost

	opDifficulty(s)

	assert.Equal(t, bigToHash(five), bigToHash(s.pop()))
}

func TestGasLimit(t *testing.T) {
	s, cancelFn := getState()
	defer cancelFn()

	t.Run("NonLondonFork", func(t *testing.T) {
		s.config = &chain.ForksInTime{}
		mockHost := mockHost{}
		mockHost.On("GetTxContext").Return(runtime.TxContext{GasLimit: 11}).Once()
		s.host = &mockHost

		opBaseFee(s)
		assert.EqualError(t, errOpCodeNotFound, s.err.Error())
	})

	t.Run("LondonFork", func(t *testing.T) {
		s.config = &chain.ForksInTime{
			London: true,
		}

		mockHost := mockHost{}
		mockHost.On("GetTxContext").Return(runtime.TxContext{BaseFee: big.NewInt(11)}).Once()
		s.host = &mockHost

		opBaseFee(s)

		assert.Equal(t, uint64(11), s.pop().Uint64())
	})
}

func TestSelfDestruct(t *testing.T) {
	addr := types.StringToAddress("0x1")

	s, cancelFn := getState()
	defer cancelFn()

	s.msg = &runtime.Contract{Static: false, Address: types.StringToAddress("0x2")}

	s.config = &chain.ForksInTime{
		EIP150: true,
		EIP158: true,
	}

	s.gas = 100000
	s.push(big.NewInt(1))

	mockHost := mockHost{}
	mockHost.On("Empty", addr).Return(true).Once()
	mockHost.On("Selfdestruct", mock.Anything, mock.Anything)
	mockHost.On("GetBalance", types.StringToAddress("0x2")).Return(big.NewInt(100)).Once()
	s.host = &mockHost

	opSelfDestruct(s)

	assert.Equal(t, uint64(70000), s.gas)
	assert.True(t, s.stop)
}

func TestJump(t *testing.T) {
	s, cancelFn := getState()
	defer cancelFn()

	s.code = make([]byte, 10)
	s.bitmap = bitmap{big.NewInt(255).Bytes()}
	s.push(five)

	opJump(s)

	assert.Equal(t, 4, s.ip)
}

func TestJumpI(t *testing.T) {
	s, cancelFn := getState()
	defer cancelFn()

	s.code = make([]byte, 10)
	s.bitmap = bitmap{big.NewInt(255).Bytes()}
	s.push(one)
	s.push(five)

	opJumpi(s)

	assert.Equal(t, 4, s.ip)
}

func TestDup(t *testing.T) {
	s, cancelFn := getState()
	defer cancelFn()

	s.sp = 6

	for i := 0; i < 10; i++ {
		s.stack = append(s.stack, big.NewInt(int64(i)))
	}

	instr := opDup(4)
	instr(s)

	assert.Equal(t, uint64(2), s.pop().Uint64())
}

func TestSwap(t *testing.T) {
	s, cancelFn := getState()
	defer cancelFn()

	s.sp = 6

	for i := 0; i < 10; i++ {
		s.stack = append(s.stack, big.NewInt(int64(i)))
	}

	instr := opSwap(4)
	instr(s)

	assert.Equal(t, uint64(5), s.stack[1].Uint64())
	assert.Equal(t, uint64(1), s.stack[6-1].Uint64())
}

type mockHostForInstructions struct {
	mockHost
	nonce       uint64
	code        []byte
	callxResult *runtime.ExecutionResult
}

func (m *mockHostForInstructions) GetNonce(types.Address) uint64 {
	return m.nonce
}

func (m *mockHostForInstructions) Callx(*runtime.Contract, runtime.Host) *runtime.ExecutionResult {
	return m.callxResult
}

func (m *mockHostForInstructions) GetCode(addr types.Address) []byte {
	return m.code
}

var (
	addr1 = types.StringToAddress("1")
)

func TestCreate(t *testing.T) {
	type state struct {
		gas    uint64
		sp     int
		stack  []*big.Int
		memory []byte
		stop   bool
		err    error
	}

	addressToBigInt := func(addr types.Address) *big.Int {
		return new(big.Int).SetBytes(addr[:])
	}

	tests := []struct {
		name        string
		op          OpCode
		contract    *runtime.Contract
		config      *chain.ForksInTime
		initState   *state
		resultState *state
		mockHost    *mockHostForInstructions
	}{
		{
			name: "should succeed in case of CREATE",
			op:   CREATE,
			contract: &runtime.Contract{
				Static:  false,
				Address: addr1,
			},
			config: &chain.ForksInTime{},
			initState: &state{
				gas: 1000,
				sp:  3,
				stack: []*big.Int{
					big.NewInt(0x01), // length
					big.NewInt(0x00), // offset
					big.NewInt(0x00), // value
				},
				memory: []byte{
					byte(REVERT),
				},
			},
			resultState: &state{
				gas: 500,
				sp:  1,
				stack: []*big.Int{
					addressToBigInt(crypto.CreateAddress(addr1, 0)), // contract address
					big.NewInt(0x00),
					big.NewInt(0x00),
				},
				memory: []byte{
					byte(REVERT),
				},
			},
			mockHost: &mockHostForInstructions{
				nonce: 0,
				callxResult: &runtime.ExecutionResult{
					GasLeft: 500,
					GasUsed: 500,
				},
			},
		},
		{
			name: "should throw errWriteProtection in case of static call",
			op:   CREATE,
			contract: &runtime.Contract{
				Static: true,
			},
			config: &chain.ForksInTime{},
			initState: &state{
				gas: 1000,
				sp:  3,
				stack: []*big.Int{
					big.NewInt(0x01), // length
					big.NewInt(0x00), // offset
					big.NewInt(0x00), // value
				},
				memory: []byte{
					byte(REVERT),
				},
				stop: false,
				err:  nil,
			},
			// shouldn't change any states except for stop and err
			resultState: &state{
				gas: 1000,
				sp:  3,
				stack: []*big.Int{
					big.NewInt(0x01), // length
					big.NewInt(0x00), // offset
					big.NewInt(0x00), // value
				},
				memory: []byte{
					byte(REVERT),
				},
				stop: true,
				err:  errWriteProtection,
			},
			mockHost: &mockHostForInstructions{},
		},
		{
			name:     "should throw errOpCodeNotFound when op is CREATE2 and config.Constantinople is disabled",
			op:       CREATE2,
			contract: &runtime.Contract{},
			config: &chain.ForksInTime{
				Constantinople: false,
			},
			initState: &state{
				gas: 1000,
				sp:  3,
				stack: []*big.Int{
					big.NewInt(0x01), // length
					big.NewInt(0x00), // offset
					big.NewInt(0x00), // value
				},
				memory: []byte{
					byte(REVERT),
				},
				stop: false,
				err:  nil,
			},
			// shouldn't change any states except for stop and err
			resultState: &state{
				gas: 1000,
				sp:  3,
				stack: []*big.Int{
					big.NewInt(0x01), // length
					big.NewInt(0x00), // offset
					big.NewInt(0x00), // value
				},
				memory: []byte{
					byte(REVERT),
				},
				stop: true,
				err:  errOpCodeNotFound,
			},
			mockHost: &mockHostForInstructions{},
		},
		{
			name: "should set zero address if op is CREATE and contract call throws ErrCodeStoreOutOfGas",
			op:   CREATE,
			contract: &runtime.Contract{
				Static:  false,
				Address: addr1,
			},
			config: &chain.ForksInTime{
				Homestead: true,
			},
			initState: &state{
				gas: 1000,
				sp:  3,
				stack: []*big.Int{
					big.NewInt(0x01), // length
					big.NewInt(0x00), // offset
					big.NewInt(0x00), // value
				},
				memory: []byte{
					byte(REVERT),
				},
				stop: false,
				err:  nil,
			},
			// shouldn't change any states except for stop and err
			resultState: &state{
				gas: 1000,
				sp:  1,
				stack: []*big.Int{
					// need to init with 0x01 to add abs field in big.Int
					big.NewInt(0x01).SetInt64(0x00),
					big.NewInt(0x00),
					big.NewInt(0x00),
				},
				memory: []byte{
					byte(REVERT),
				},
				stop: false,
				err:  nil,
			},
			mockHost: &mockHostForInstructions{
				nonce: 0,
				callxResult: &runtime.ExecutionResult{
					GasLeft: 1000,
					Err:     runtime.ErrCodeStoreOutOfGas,
				},
			},
		},
		{
			name: "should set zero address if contract call throws error except for ErrCodeStoreOutOfGas",
			op:   CREATE,
			contract: &runtime.Contract{
				Static:  false,
				Address: addr1,
			},
			config: &chain.ForksInTime{
				Homestead: true,
			},
			initState: &state{
				gas: 1000,
				sp:  3,
				stack: []*big.Int{
					big.NewInt(0x01), // length
					big.NewInt(0x00), // offset
					big.NewInt(0x00), // value
				},
				memory: []byte{
					byte(REVERT),
				},
				stop: false,
				err:  nil,
			},
			// shouldn't change any states except for stop and err
			resultState: &state{
				gas: 1000,
				sp:  1,
				stack: []*big.Int{
					// need to init with 0x01 to add abs field in big.Int
					big.NewInt(0x01).SetInt64(0x00),
					big.NewInt(0x00),
					big.NewInt(0x00),
				},
				memory: []byte{
					byte(REVERT),
				},
				stop: false,
				err:  nil,
			},
			mockHost: &mockHostForInstructions{
				nonce: 0,
				callxResult: &runtime.ExecutionResult{
					GasLeft: 1000,
					Err:     errRevert,
				},
			},
		},
		{
			name: "should set zero address if contract call throws any error for CREATE2",
			op:   CREATE2,
			contract: &runtime.Contract{
				Static:  false,
				Address: addr1,
			},
			config: &chain.ForksInTime{
				Homestead:      true,
				Constantinople: true,
			},
			initState: &state{
				gas: 1000,
				sp:  4,
				stack: []*big.Int{
					big.NewInt(0x01), // salt
					big.NewInt(0x01), // length
					big.NewInt(0x00), // offset
					big.NewInt(0x00), // value
				},
				memory: []byte{
					byte(REVERT),
				},
				stop: false,
				err:  nil,
			},
			// during creation of code with length 1 for CREATE2 op code, 985 gas units are spent by buildCreateContract()
			resultState: &state{
				gas: 15,
				sp:  1,
				stack: []*big.Int{
					big.NewInt(0x01).SetInt64(0x00),
					big.NewInt(0x01),
					big.NewInt(0x00),
					big.NewInt(0x00),
				},
				memory: []byte{
					byte(REVERT),
				},
				stop: false,
				err:  nil,
			},
			mockHost: &mockHostForInstructions{
				nonce: 0,
				callxResult: &runtime.ExecutionResult{
					// if it is ErrCodeStoreOutOfGas then we set GasLeft to 0
					GasLeft: 0,
					Err:     runtime.ErrCodeStoreOutOfGas,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, closeFn := getState()
			defer closeFn()

			s.msg = tt.contract
			s.gas = tt.initState.gas
			s.sp = tt.initState.sp
			s.stack = tt.initState.stack
			s.memory = tt.initState.memory
			s.config = tt.config
			s.host = tt.mockHost

			opCreate(tt.op)(s)

			assert.Equal(t, tt.resultState.gas, s.gas, "gas in state after execution is not correct")
			assert.Equal(t, tt.resultState.sp, s.sp, "sp in state after execution is not correct")
			assert.Equal(t, tt.resultState.stack, s.stack, "stack in state after execution is not correct")
			assert.Equal(t, tt.resultState.memory, s.memory, "memory in state after execution is not correct")
			assert.Equal(t, tt.resultState.stop, s.stop, "stop in state after execution is not correct")
			assert.Equal(t, tt.resultState.err, s.err, "err in state after execution is not correct")
		})
	}
}

func Test_opReturnDataCopy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		config      *chain.ForksInTime
		initState   *state
		resultState *state
	}{
		{
			name: "should return error if Byzantium is not applied",
			config: &chain.ForksInTime{
				Byzantium: false,
			},
			initState: &state{},
			resultState: &state{
				config: &chain.ForksInTime{
					Byzantium: false,
				},
				stop: true,
				err:  errOpCodeNotFound,
			},
		},
		{
			name:   "should return error if memOffset is negative",
			config: &allEnabledForks,
			initState: &state{
				stack: []*big.Int{
					big.NewInt(1),  // length
					big.NewInt(0),  // dataOffset
					big.NewInt(-1), // memOffset
				},
				sp: 3,
			},
			resultState: &state{
				config: &allEnabledForks,
				stack: []*big.Int{
					big.NewInt(1),
					big.NewInt(0),
					big.NewInt(-1),
				},
				sp:   0,
				stop: true,
				err:  errReturnDataOutOfBounds,
			},
		},
		{
			name:   "should return error if dataOffset is negative",
			config: &allEnabledForks,
			initState: &state{
				stack: []*big.Int{
					big.NewInt(1),  // length
					big.NewInt(-1), // dataOffset
					big.NewInt(0),  // memOffset
				},
				sp:     3,
				memory: make([]byte, 1),
			},
			resultState: &state{
				config: &allEnabledForks,
				stack: []*big.Int{
					big.NewInt(1),
					big.NewInt(-1),
					big.NewInt(0),
				},
				sp:     0,
				memory: make([]byte, 1),
				stop:   true,
				err:    errReturnDataOutOfBounds,
			},
		},
		{
			name:   "should return error if length is negative",
			config: &allEnabledForks,
			initState: &state{
				stack: []*big.Int{
					big.NewInt(-1), // length
					big.NewInt(0),  // dataOffset
					big.NewInt(0),  // memOffset
				},
				sp: 3,
			},
			resultState: &state{
				config: &allEnabledForks,
				stack: []*big.Int{
					big.NewInt(-1),
					big.NewInt(0),
					big.NewInt(0),
				},
				sp:   0,
				stop: true,
				err:  errReturnDataOutOfBounds,
			},
		},
		{
			name:   "should copy data from returnData to memory",
			config: &allEnabledForks,
			initState: &state{
				stack: []*big.Int{
					big.NewInt(1), // length
					big.NewInt(0), // dataOffset
					big.NewInt(0), // memOffset
				},
				sp:         3,
				returnData: []byte{0xff},
				memory:     []byte{0x0},
				gas:        10,
			},
			resultState: &state{
				config: &allEnabledForks,
				stack: []*big.Int{
					big.NewInt(1),
					big.NewInt(0),
					big.NewInt(0),
				},
				sp:                 0,
				returnData:         []byte{0xff},
				memory:             []byte{0xff},
				gas:                7,
				lastGasCost:        0,
				currentConsumedGas: 3,
				stop:               false,
				err:                nil,
			},
		},
		{
			name:   "should expand memory and copy data returnData",
			config: &allEnabledForks,
			initState: &state{
				stack: []*big.Int{
					big.NewInt(5), // length
					big.NewInt(1), // dataOffset
					big.NewInt(2), // memOffset
				},
				sp:         3,
				returnData: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
				memory:     []byte{0x11, 0x22},
				gas:        20,
			},
			resultState: &state{
				config: &allEnabledForks,
				stack: []*big.Int{
					big.NewInt(6), // updated for end index
					big.NewInt(1),
					big.NewInt(2),
				},
				sp:         0,
				returnData: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
				memory: append(
					// 1 word (32 bytes)
					[]byte{0x11, 0x22, 0x02, 0x03, 0x04, 0x05, 0x06},
					make([]byte, 25)...,
				),
				gas:                14,
				lastGasCost:        3,
				currentConsumedGas: 6,
				stop:               false,
				err:                nil,
			},
		},
		{
			// this test case also verifies that code does not panic when the length is 0 and memOffset > len(memory)
			name:   "should not copy data if length is zero",
			config: &allEnabledForks,
			initState: &state{
				stack: []*big.Int{
					big.NewInt(0), // length
					big.NewInt(0), // dataOffset
					big.NewInt(4), // memOffset
				},
				sp:         3,
				returnData: []byte{0x01},
				memory:     []byte{0x02},
			},
			resultState: &state{
				config: &allEnabledForks,
				stack: []*big.Int{
					big.NewInt(0),
					big.NewInt(0),
					big.NewInt(4),
				},
				sp:         0,
				returnData: []byte{0x01},
				memory:     []byte{0x02},
				stop:       false,
				err:        nil,
			},
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			state, closeFn := getState()
			defer closeFn()

			state.gas = test.initState.gas
			state.sp = test.initState.sp
			state.stack = test.initState.stack
			state.memory = test.initState.memory
			state.returnData = test.initState.returnData
			state.config = test.config

			// assign nil to some fields in cached state object
			state.code = nil
			state.host = nil
			state.msg = nil
			state.evm = nil
			state.bitmap = bitmap{}
			state.ret = nil
			state.currentConsumedGas = 0

			opReturnDataCopy(state)

			assert.Equal(t, test.resultState, state)
		})
	}
}

func Test_opCall(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		op          OpCode
		contract    *runtime.Contract
		config      chain.ForksInTime
		initState   *state
		resultState *state
		mockHost    *mockHostForInstructions
	}{
		{
			// this test case also verifies that code does not panic when the outSize is 0 and outOffset > len(memory)
			name: "should not copy result into memory if outSize is 0",
			op:   STATICCALL,
			contract: &runtime.Contract{
				Static: true,
			},
			config: allEnabledForks,
			initState: &state{
				gas: 1000,
				sp:  6,
				stack: []*big.Int{
					big.NewInt(0x00), // outSize
					big.NewInt(0x02), // outOffset
					big.NewInt(0x00), // inSize
					big.NewInt(0x00), // inOffset
					big.NewInt(0x00), // address
					big.NewInt(0x00), // initialGas
				},
				memory: []byte{0x01},
			},
			resultState: &state{
				memory: []byte{0x01},
				stop:   false,
				err:    nil,
				gas:    300,
			},
			mockHost: &mockHostForInstructions{
				callxResult: &runtime.ExecutionResult{
					ReturnValue: []byte{0x03},
				},
			},
		},
		{
			name: "call cost overflow (EIP150 fork disabled)",
			op:   CALLCODE,
			contract: &runtime.Contract{
				Static: false,
			},
			config: chain.AllForksEnabled.RemoveFork(chain.EIP150).At(0),
			initState: &state{
				gas: 6640,
				sp:  7,
				stack: []*big.Int{
					big.NewInt(0x00),                        // outSize
					big.NewInt(0x00),                        // outOffset
					big.NewInt(0x00),                        // inSize
					big.NewInt(0x00),                        // inOffset
					big.NewInt(0x01),                        // value
					big.NewInt(0x03),                        // address
					big.NewInt(0).SetUint64(math.MaxUint64), // initialGas
				},
				memory: []byte{0x01},
			},
			resultState: &state{
				memory: []byte{0x01},
				stop:   true,
				err:    errGasUintOverflow,
				gas:    6640,
			},
			mockHost: &mockHostForInstructions{
				callxResult: &runtime.ExecutionResult{
					ReturnValue: []byte{0x03},
				},
			},
		},
		{
			name: "available gas underflow",
			op:   CALLCODE,
			contract: &runtime.Contract{
				Static: false,
			},
			config: allEnabledForks,
			initState: &state{
				gas: 6640,
				sp:  7,
				stack: []*big.Int{
					big.NewInt(0x00),                        // outSize
					big.NewInt(0x00),                        // outOffset
					big.NewInt(0x00),                        // inSize
					big.NewInt(0x00),                        // inOffset
					big.NewInt(0x01),                        // value
					big.NewInt(0x03),                        // address
					big.NewInt(0).SetUint64(math.MaxUint64), // initialGas
				},
				memory: []byte{0x01},
			},
			resultState: &state{
				memory: []byte{0x01},
				stop:   true,
				err:    errOutOfGas,
				gas:    6640,
			},
			mockHost: &mockHostForInstructions{
				callxResult: &runtime.ExecutionResult{
					ReturnValue: []byte{0x03},
				},
			},
		},
	}

	for _, tt := range tests {
		test := tt
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state, closeFn := getState()
			defer closeFn()

			state.gas = test.initState.gas
			state.msg = test.contract
			state.sp = test.initState.sp
			state.stack = test.initState.stack
			state.memory = test.initState.memory
			state.config = &test.config
			state.host = test.mockHost

			opCall(test.op)(state)

			assert.Equal(t, test.resultState.memory, state.memory, "memory in state after execution is incorrect")
			assert.Equal(t, test.resultState.stop, state.stop, "stop in state after execution is incorrect")
			assert.Equal(t, test.resultState.err, state.err, "err in state after execution is incorrect")
			assert.Equal(t, test.resultState.gas, state.gas, "gas in state after execution is incorrect")
		})
	}
}
