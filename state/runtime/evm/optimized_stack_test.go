package evm

import (
	"testing"

	"github.com/holiman/uint256"
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

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if poppedValue != *value {
		t.Errorf("Expected popped value to be %v, got %v", value, poppedValue)
	}

	if stack.sp != 0 {
		t.Errorf("Expected stack pointer to be 0 after pop, got %d", stack.sp)
	}
}

// TestUnderflow tests the underflow condition when popping from an empty stack.
func TestOptimizedStackUnderflow(t *testing.T) {
	var stack OptimizedStack

	_, err := stack.pop()
	if err == nil {
		t.Errorf("Expected an underflow error when popping from an empty stack, got nil")
	}
}

// TestTop tests the top function without modifying the stack.
func TestOptimizedStackTop(t *testing.T) {
	var stack OptimizedStack

	value := uint256.NewInt(10)

	stack.push(*value)

	topValue, err := stack.top()

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if *topValue != *value {
		t.Errorf("Expected top value to be %v, got %v", value, *topValue)
	}

	if stack.sp != 1 {
		t.Errorf("Expected stack pointer to remain 1 after top, got %d", stack.sp)
	}
}

// TestReset tests the reset function to ensure it clears the stack.
func TestOptimizedStackReset(t *testing.T) {
	var stack OptimizedStack

	stack.push(*uint256.NewInt(0))
	stack.reset()

	if stack.sp != 0 || len(stack.data) != 0 {
		t.Errorf("Expected stack to be empty after reset, got sp: %d, len(data): %d", stack.sp, len(stack.data))
	}
}

// TestPeekAt tests the peekAt function for retrieving elements without modifying the stack.
func TestOptimizedStackPeekAt(t *testing.T) {
	var stack OptimizedStack

	value1 := uint256.NewInt(1)
	value2 := uint256.NewInt(2)

	stack.push(*value1)
	stack.push(*value2)

	peekedValue := stack.peekAt(2)

	if peekedValue != *value1 {
		t.Errorf("Expected to peek at value %v, got %v", value1, peekedValue)
	}

	// Verify stack state hasn't changed after peekAt
	if stack.sp != 2 {
		t.Errorf("Expected stack pointer to remain 2 after peekAt, got %d", stack.sp)
	}
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
	if stack.data[stack.sp-1] != *value1 || stack.data[stack.sp-2] != *value2 {
		t.Errorf("Expected top two values to be swapped to %v and %v, got %v and %v", value1, value2, stack.data[stack.sp-1], stack.data[stack.sp-2])
	}
}
