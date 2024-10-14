package evm

import (
	"testing"

	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

func BenchmarkOptimizedStack_Push(b *testing.B) {
	stack := NewOptimizedStack(int32(b.N))
	val := uint256.NewInt(42)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		stack.push(*val)
	}
}

func BenchmarkOptimizedStack_PushPop(b *testing.B) {
	stackSize := 10
	stack := NewOptimizedStack(int32(stackSize))
	val := uint256.NewInt(42)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for j := 0; j < stackSize; j++ {
			stack.push(*val)
		}

		for j := 0; j < stackSize; j++ {
			_, _ = stack.pop()
		}
	}
}

func BenchmarkOptimizedStack_Top(b *testing.B) {
	stackSize := 100
	stack := NewOptimizedStack(int32(stackSize))
	val := uint256.NewInt(42)

	for i := 0; i < stackSize; i++ {
		stack.push(*val)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = stack.top()
	}
}

func BenchmarkOptimizedStack_PeekAt(b *testing.B) {
	stackSize := 100
	stack := NewOptimizedStack(int32(stackSize))
	val := uint256.NewInt(42)

	for i := 0; i < stackSize; i++ {
		stack.push(*val)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = stack.peekAt(50)
	}
}

func BenchmarkOptimizedStack_Swap(b *testing.B) {
	stackSize := 100
	stack := NewOptimizedStack(int32(stackSize))
	val := uint256.NewInt(42)

	for i := 0; i < stackSize; i++ {
		stack.push(*val)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		require.NoError(b, stack.swap(50))
	}
}
