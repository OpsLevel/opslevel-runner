package pkg

import (
	"testing"
	"time"

	"github.com/rocktavious/autopilot/v2023"
	"github.com/rs/zerolog"
)

func TestOpsLevelAppendLogProcessorTickShipsIdleLogs(t *testing.T) {
	// Arrange
	p := NewOpsLevelAppendLogProcessor(nil, zerolog.Nop(), "1", "1", "1", 1024, 50*time.Millisecond)
	p.Process("first")  // first line ships immediately
	p.Process("second") // buffered
	autopilot.Equals(t, 1, len(p.logLines))
	// Act & Assert: maxTime has not elapsed yet, so nothing ships
	p.Tick()
	autopilot.Equals(t, 1, len(p.logLines))
	// Act & Assert: once maxTime elapses, Tick ships without any new lines
	time.Sleep(100 * time.Millisecond)
	p.Tick()
	autopilot.Equals(t, 0, len(p.logLines))
}
