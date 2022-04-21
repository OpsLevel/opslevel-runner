package pkg

import (
	"testing"
	"github.com/rocktavious/autopilot"
)

func TestSetOutcomeVarLogProcessor(t *testing.T) {
	// Arrange
	p := NewSetOutcomeVarLogProcessor()
	// Act
	p.Process("set-outcome-var ")
	p.Process("::set-outcome-var hello-world=42")
	p.Process("::set-var foo=bar")
	p.Process("::set-outcome-var opslevel_testing=best")
	// Assert
	autopilot.Equals(t, 2, len(p.vars))
	autopilot.Equals(t, "42", p.vars[0].Value)
	autopilot.Equals(t, "opslevel_testing", p.vars[1].Key)
}