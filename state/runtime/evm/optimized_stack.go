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
type OptimizedStack struct {
	sp   int           // Stack pointer to track the top of the stack
	data []uint256.Int // Slice to store the stack's elements
}

// reset clears the stack by resetting the stack pointer to 0 and truncating
// the data slice to zero length.
func (s *OptimizedStack) reset() {
	s.sp = 0
	s.data = s.data[:0] // Efficiently clears the slice without allocating new memory
}

// push adds a new element of type uint256.Int to the top of the stack.
// It appends the element to the data slice and increments the stack pointer.
func (s *OptimizedStack) push(val uint256.Int) {
	s.data = append(s.data, val)
	s.sp++
}

// pop removes and returns the top element of the stack.
// If the stack is empty, it returns a zero value of uint256.Int and an error.
func (s *OptimizedStack) pop() (uint256.Int, error) {
	if s.sp == 0 {
		// The stack is empty, return a zero value and an underflow error
		return uint256.Int{0}, &runtime.StackUnderflowError{}
	}

	o := s.data[s.sp-1]    // Get the top element
	s.sp--                 // Decrement the stack pointer
	s.data = s.data[:s.sp] // Truncate the slice to remove the top element

	return o, nil
}

// top returns the top element of the stack without removing it. If the stack
// is empty, it returns nil and an error.
func (s *OptimizedStack) top() (*uint256.Int, error) {
	if s.sp == 0 {
		// The stack is empty, return nil and an underflow error
		return nil, &runtime.StackUnderflowError{}
	}

	topIndex := len(s.data) - 1 // Calculate the index of the top element

	return &s.data[topIndex], nil // Return a pointer to the top element
}

// peekAt returns the element at the nth position from the top of the stack,
// without modifying the stack. It does not perform bounds checking and it
// returns the value of the element, not the reference.
func (s *OptimizedStack) peekAt(n int) uint256.Int {
	return s.data[s.sp-n]
}

// swap exchanges the top element of the stack with the element at the n-th position
// from the top. It does not perform bounds checking and assumes valid input.
func (s *OptimizedStack) swap(n int) {
	s.data[s.sp-1], s.data[s.sp-n-1] = s.data[s.sp-n-1], s.data[s.sp-1]
}
