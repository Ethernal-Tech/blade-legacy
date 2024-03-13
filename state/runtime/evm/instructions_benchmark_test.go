package evm

import (
	"testing"

	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/holiman/uint256"
)

type (
	instructionOperation func(c *state)
)

func BenchmarkStack(b *testing.B) {
	s, closeFn := getState(&chain.ForksInTime{})
	defer closeFn()

	op1 := uint256.NewInt(1)
	op2 := uint256.NewInt(2)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		s.push(*op1)
		s.push(*op2)
		s.pop()
		s.pop()
	}

	b.StopTimer()
}

func operationBenchmark(b *testing.B, s *state, op instructionOperation, op1 uint256.Int, op2 uint256.Int) {
	b.Helper()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		s.push(op1)
		s.push(op2)
		op(s)
		s.pop()
	}

	b.StopTimer()
}

func BenchmarkInstruction_opAdd(b *testing.B) {
	s, closeFn := getState(&chain.ForksInTime{})
	defer closeFn()

	s.gas = 9223372036854775807
	op1 := uint256.NewInt(9223372036854775807)
	op2 := uint256.NewInt(9223372036854775807)

	operationBenchmark(b, s, opAdd, *op1, *op2)
}

func BenchmarkInstruction_opMul(b *testing.B) {
	s, closeFn := getState(&chain.ForksInTime{})
	defer closeFn()

	s.gas = 9223372036854775807
	op1 := uint256.NewInt(9223372036854775807)
	op2 := uint256.NewInt(9223372036854775807)

	operationBenchmark(b, s, opMul, *op1, *op2)
}

func BenchmarkInstruction_opSub(b *testing.B) {
	s, closeFn := getState(&chain.ForksInTime{})
	defer closeFn()

	s.gas = 9223372036854775807
	op1 := uint256.NewInt(9223372036854775807)
	op2 := uint256.NewInt(9223372036854775807)

	operationBenchmark(b, s, opSub, *op1, *op2)
}

func BenchmarkInstruction_opDiv(b *testing.B) {
	s, closeFn := getState(&chain.ForksInTime{})
	defer closeFn()

	s.gas = 9223372036854775807
	op1 := uint256.NewInt(9223372036854775807)
	op2 := uint256.NewInt(9223372036854775807)

	operationBenchmark(b, s, opDiv, *op1, *op2)
}

func BenchmarkInstruction_opSDiv(b *testing.B) {
	s, closeFn := getState(&chain.ForksInTime{})
	defer closeFn()

	s.gas = 9223372036854775807
	op1 := uint256.NewInt(9223372036854775807)
	op2 := uint256.NewInt(9223372036854775807)

	operationBenchmark(b, s, opSDiv, *op1, *op2)
}

func BenchmarkInstruction_opMod(b *testing.B) {
	s, closeFn := getState(&chain.ForksInTime{})
	defer closeFn()

	s.gas = 9223372036854775807

	op1 := uint256.NewInt(9223372036854775807)
	op2 := uint256.NewInt(9223372036854775807)

	operationBenchmark(b, s, opMod, *op1, *op2)
}

func BenchmarkInstruction_opSMod(b *testing.B) {
	s, closeFn := getState(&chain.ForksInTime{})
	defer closeFn()

	s.gas = 9223372036854775807
	op1 := uint256.NewInt(9223372036854775807)
	op2 := uint256.NewInt(9223372036854775807)

	operationBenchmark(b, s, opSMod, *op1, *op2)
}

func BenchmarkInstruction_opExp(b *testing.B) {
	s, closeFn := getState(&chain.ForksInTime{})
	defer closeFn()

	s.gas = 9223372036854775807
	op1 := uint256.NewInt(9223372036854775807)
	op2 := uint256.NewInt(9223372036854775807)

	operationBenchmark(b, s, opExp, *op1, *op2)
}

func BenchmarkInstruction_opAnd(b *testing.B) {
	s, closeFn := getState(&chain.ForksInTime{})
	defer closeFn()

	s.gas = 9223372036854775807
	op1 := uint256.NewInt(9223372036854775807)
	op2 := uint256.NewInt(9223372036854775807)

	operationBenchmark(b, s, opAnd, *op1, *op2)
}

func BenchmarkInstruction_opOr(b *testing.B) {
	s, closeFn := getState(&chain.ForksInTime{})
	defer closeFn()

	s.gas = 9223372036854775807
	op1 := uint256.NewInt(9223372036854775807)
	op2 := uint256.NewInt(9223372036854775807)

	operationBenchmark(b, s, opOr, *op1, *op2)
}

func BenchmarkInstruction_opXor(b *testing.B) {
	s, closeFn := getState(&chain.ForksInTime{})
	defer closeFn()

	s.gas = 9223372036854775807
	op1 := uint256.NewInt(9223372036854775807)
	op2 := uint256.NewInt(9223372036854775807)

	operationBenchmark(b, s, opXor, *op1, *op2)
}

func BenchmarkInstruction_opByte(b *testing.B) {
	s, closeFn := getState(&chain.ForksInTime{})
	defer closeFn()

	s.gas = 9223372036854775807
	op1 := uint256.NewInt(9223372036854775807)
	op2 := uint256.NewInt(9223372036854775807)

	operationBenchmark(b, s, opByte, *op1, *op2)
}

func BenchmarkInstruction_opEq(b *testing.B) {
	s, closeFn := getState(&chain.ForksInTime{})
	defer closeFn()

	s.gas = 9223372036854775807
	op1 := uint256.NewInt(9223372036854775807)
	op2 := uint256.NewInt(9223372036854775807)

	operationBenchmark(b, s, opEq, *op1, *op2)
}

func BenchmarkInstruction_opLt(b *testing.B) {
	s, closeFn := getState(&chain.ForksInTime{})
	defer closeFn()

	s.gas = 9223372036854775807
	op1 := uint256.NewInt(9223372036854775807)
	op2 := uint256.NewInt(9223372036854775807)

	operationBenchmark(b, s, opLt, *op1, *op2)
}

func BenchmarkInstruction_opGt(b *testing.B) {
	s, closeFn := getState(&chain.ForksInTime{})
	defer closeFn()

	s.gas = 9223372036854775807
	op1 := uint256.NewInt(9223372036854775807)
	op2 := uint256.NewInt(9223372036854775807)

	operationBenchmark(b, s, opGt, *op1, *op2)
}

func BenchmarkInstruction_opSlt(b *testing.B) {
	s, closeFn := getState(&chain.ForksInTime{})
	defer closeFn()

	s.gas = 9223372036854775807
	op1 := uint256.NewInt(9223372036854775807)
	op2 := uint256.NewInt(9223372036854775807)

	operationBenchmark(b, s, opSlt, *op1, *op2)
}

func BenchmarkInstruction_opSgt(b *testing.B) {
	s, closeFn := getState(&chain.ForksInTime{})
	defer closeFn()

	s.gas = 9223372036854775807
	op1 := uint256.NewInt(9223372036854775807)
	op2 := uint256.NewInt(9223372036854775807)

	operationBenchmark(b, s, opSgt, *op1, *op2)
}
