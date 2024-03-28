package evm

import (
	"testing"

	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// TestPushPop tests the push and pop operations of the stack.
func TestOptimizedStackPushPop(t *testing.T) {
	var stack OptimizedStack

	value := uint256.NewInt(10)

	stack.push(*value)

	if stack.sp != 1 {
		t.Errorf("Expected stack pointer to be 1, got %d", stack.sp)
	}

	poppedValue, err := stack.pop()

	require.NoError(t, err)

	require.Equal(t, poppedValue, *value)

	require.Zero(t, stack.sp, "Expected stack pointer to be 0 after pop.")
}

// TestUnderflow tests the underflow condition when popping from an empty stack.
func TestOptimizedStackUnderflow(t *testing.T) {
	var stack OptimizedStack

	_, err := stack.pop()

	require.Error(t, err, "Expected an underflow error when popping from an empty stack, got nil")
}

// TestTop tests the top function without modifying the stack.
func TestOptimizedStackTop(t *testing.T) {
	var stack OptimizedStack

	value := uint256.NewInt(10)

	stack.push(*value)

	topValue, err := stack.top()

	require.NoError(t, err)

	require.Equal(t, *topValue, *value)

	require.Equal(t, stack.sp, 1, "Expected stack pointer to remain 1 after top.")
}

// TestReset tests the reset function to ensure it clears the stack.
func TestOptimizedStackReset(t *testing.T) {
	var stack OptimizedStack

	stack.push(uint256.Int{0})
	stack.reset()

	require.Zero(t, stack.sp, "Expected stack to be empty after reset")
	require.Zero(t, len(stack.data), "Expected stack to be empty after reset")
}

// TestPeekAt tests the peekAt function for retrieving elements without modifying the stack.
func TestOptimizedStackPeekAt(t *testing.T) {
	var stack OptimizedStack

	value1 := uint256.NewInt(1)
	value2 := uint256.NewInt(2)

	stack.push(*value1)
	stack.push(*value2)

	peekedValue := stack.peekAt(2)

	require.Equal(t, peekedValue, *value1)

	require.Equal(t, stack.sp, 2)
}

// TestSwap tests the swap function to ensure it correctly swaps elements in the stack.
func TestOptimizedStackSwap(t *testing.T) {
	var stack OptimizedStack

	value1 := uint256.NewInt(1)
	value2 := uint256.NewInt(2)

	// Push two distinct values onto the stack
	stack.push(*value1)
	stack.push(*value2)

	// Swap the top two elements
	stack.swap(1)

	// Verify swap operation
	require.Equal(t, stack.data[stack.sp-1], *value1)
	require.Equal(t, stack.data[stack.sp-2], *value2)
}
