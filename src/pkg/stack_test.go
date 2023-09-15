package pkg

import (
	"testing"

	"github.com/rocktavious/autopilot"
)

func TestStack(t *testing.T) {
	// Arrange
	stack := NewStack[string]("")
	// Act
	stack.Push("one")
	stack.Push("two")
	peek1 := stack.Peek()
	stack.Push("three")
	stack.Push("four")
	length1 := stack.Length()
	peek2 := stack.Peek()
	pop1 := stack.Pop()
	stack.Pop()
	pop2 := stack.Pop()
	peek3 := stack.Peek()
	stack.Pop()
	length2 := stack.Length()
	pop3 := stack.Pop()
	// Assert
	autopilot.Equals(t, "two", peek1)
	autopilot.Equals(t, 4, length1)
	autopilot.Equals(t, "four", peek2)
	autopilot.Equals(t, "four", pop1)
	autopilot.Equals(t, "two", pop2)
	autopilot.Equals(t, "one", peek3)
	autopilot.Equals(t, 0, length2)
	autopilot.Equals(t, "", pop3)
}
