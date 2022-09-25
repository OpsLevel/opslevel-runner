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
}`, p.vars["multi-var-name"])
}

func TestMultilineOutcomeVarLogProcessorSkipsBashCommands(t *testing.T) {
	// Arrange
	p := NewSetOutcomeVarLogProcessor(nil, log.Logger, "1", "1", "1")
	// Act
	p.Process("+ echo ::start-multiline-outcome-var multi-var-name")
	p.Process("::start-multiline-outcome-var multi-var-name")
	p.Process("{")
	p.Process("  \"hello\":\"world\",")
	p.Process("  \"foo\":\"bar\"")
	p.Process("}")
	p.Process("+ echo ::end-multiline-outcome-var")
	p.Process("::end-multiline-outcome-var")
	// Assert
	autopilot.Equals(t, 1, len(p.vars))
	autopilot.Equals(t, `{
  "hello":"world",
  "foo":"bar"
}`, p.vars["multi-var-name"])
}

func TestMultilineMissingKeyOutcomeVarLogProcessor(t *testing.T) {
	// Arrange
	p := NewSetOutcomeVarLogProcessor(nil, log.Logger, "1", "1", "1")
	// Act

	p.Process("::start-multiline-outcome-var")
	p.Process("{")
	p.Process("  \"hello\":\"world\",")
	p.Process("  \"foo\":\"bar\"")
	p.Process("}")
	p.Process("::end-multiline-outcome-var")
	// Assert
	autopilot.Equals(t, 0, len(p.vars))
	autopilot.Equals(t, map[string]string{}, p.vars)
}

func TestNestedMultilineKeyOutcomeVarLogProcessor(t *testing.T) {
	// Arrange
	p := NewSetOutcomeVarLogProcessor(nil, log.Logger, "1", "1", "1")
	// Act

	p.Process("::start-multiline-outcome-var one")
	p.Process("hello")
	p.Process("world")
	p.Process("::start-multiline-outcome-var two")
	p.Process("foo")
	p.Process("foo")
	p.Process("foo")
	p.Process("::end-multiline-outcome-var")
	p.Process("foo")
	p.Process("bar")
	p.Process("::end-multiline-outcome-var")
	p.Process("::end-multiline-outcome-var")
	// Assert
	autopilot.Equals(t, 2, len(p.vars))
	autopilot.Equals(t, "hello\nworld\nfoo\nbar", p.vars["one"])
	autopilot.Equals(t, "foo\nfoo\nfoo", p.vars["two"])
}
