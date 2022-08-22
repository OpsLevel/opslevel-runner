package pkg

import (
	"github.com/rocktavious/autopilot"
	"github.com/rs/zerolog/log"
	"testing"
)

func TestSetOutcomeVarLogProcessor(t *testing.T) {
	// Arrange
	p := NewSetOutcomeVarLogProcessor(nil, log.Logger, "1", "1", "1")
	// Act
	p.Process("set-outcome-var ")
	p.Process("::set-outcome-var hello-world=42")
	p.Process("::start-multiline-outcome-var multi-var-name")
	p.Process("{")
	p.Process("  \"hello\":\"world\",")
	p.Process("  \"foo\":\"bar\"")
	p.Process("}")
	p.Process("::end-multiline-outcome-var")
	p.Process("::set-var foo=bar")
	p.Process("::set-outcome-var opslevel_testing=best")
	// Assert
	autopilot.Equals(t, 3, len(p.vars))
	autopilot.Equals(t, "42", p.vars["hello-world"])
	autopilot.Equals(t, "best", p.vars["opslevel_testing"])
	autopilot.Equals(t, `{
  "hello":"world",
  "foo":"bar"
}`, p.vars["multi"])
}
