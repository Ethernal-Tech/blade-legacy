package evm

import (
	"github.com/0xPolygon/polygon-edge/state/runtime"
	"github.com/holiman/uint256"
)

// OptimizedStack represents a stack data structure for uint256 integers.
// It utilizes a dynamic array (slice) to store the data and an integer (sp)
// to keep track of the current stack pointer.
// OptimizedStack uint256 integers for improved operations on values on the
// stack and minimizes heap allocations.
type OptimizedStack []uint256.Int

// NewOptimizedStack creates a new instance of OptimizedStack with an initialized
// internal stack slice to avoid unnecessary reallocations.
func NewOptimizedStack(capacity int32) *OptimizedStack {
	stack := make(OptimizedStack, 0, capacity)
	return &stack
}

// size returns the numer of elements on the stack
func (s *OptimizedStack) size() int {
	return len(*s)
}

// reset clears the stack by resetting the stack pointer to 0 and truncating
// the data slice to zero length.
func (s *OptimizedStack) reset() {
	*s = (*s)[:0] // Efficiently clears the slice without allocating new memory
}

// push adds a new element of type uint256.Int to the top of the stack.
// It appends the element to the data slice and increments the stack pointer.
func (s *OptimizedStack) push(val uint256.Int) {
	*s = append(*s, val)
}

// pop removes and returns the top element of the stack.
// If the stack is empty, it returns a zero value of uint256.Int and an error.
func (s *OptimizedStack) pop() (uint256.Int, error) {
	sp := s.size()
	if sp == 0 {
		// The stack is empty, return a zero value and an underflow error
		return uint256.Int{0}, &runtime.StackUnderflowError{}
	}

	sp--           // Decrement the stack pointer
	o := (*s)[sp]  // Get the top element
	*s = (*s)[:sp] // Truncate the slice to remove the top element

	return o, nil
}

// top returns the top element of the stack without removing it. If the stack
// is empty, it returns nil and an error.
func (s *OptimizedStack) top() (*uint256.Int, error) {
	sp := s.size()
	if sp == 0 {
		// The stack is empty, return nil and an underflow error
		return nil, &runtime.StackUnderflowError{}
	}

	topIndex := sp - 1 // Calculate the index of the top element

	return &(*s)[topIndex], nil // Return a pointer to the top element
}

// peekAt returns the element at the nth position from the top of the stack,
// without modifying the stack. It returns the value of the element, not the
// reference.
func (s *OptimizedStack) peekAt(n int) (uint256.Int, error) {
	if n < 0 || n > s.size() {
		// Return nil and an error if n is out of bounds
		return *uint256.NewInt(0), &runtime.StackOutOfBoundsError{StackLen: s.size(), RequestedIndex: n}
	}

	return (*s)[s.size()-n], nil
}

// swap exchanges the top element of the stack with the element at the n-th position
// from the top.
func (s *OptimizedStack) swap(n int) error {
	size := s.size()
	if size == 0 {
		return &runtime.StackOutOfBoundsError{StackLen: s.size(), RequestedIndex: n}
	}

	if n < 0 || n >= size {
		return &runtime.StackOutOfBoundsError{StackLen: s.size(), RequestedIndex: n}
	}

	sp := size - 1

	(*s)[sp], (*s)[sp-n] = (*s)[sp-n], (*s)[sp]

	return nil
}
