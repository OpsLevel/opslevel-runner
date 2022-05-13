package pkg

import (
	"testing"
	"github.com/opslevel/opslevel-go"
	"github.com/rocktavious/autopilot"
)

func TestSanitizeLogProcessor(t *testing.T) {
	// Arrange
	p := NewSanitizeLogProcessor([]opslevel.RunnerJobVariable{
		{Key: "NotSecret", Value: "Hello", Sensitive: false},
		{Key: "Secret", Value: "World", Sensitive: true},
	})
	// Act
	line1 := p.Process("lorum ipsum")
	line2 := p.Process("Hello Everyone")
	line3 := p.Process("Hello World")
	// Assert
	autopilot.Equals(t, "lorum ipsum", line1)
	autopilot.Equals(t, "Hello Everyone", line2)
	autopilot.Equals(t, "Hello **********", line3)
}