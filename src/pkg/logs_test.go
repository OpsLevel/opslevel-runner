package pkg

import (
	"context"
	"testing"
	"time"

	"github.com/rocktavious/autopilot/v2023"
	"github.com/rs/zerolog"
)

type captureProcessor struct {
	lines []string
}

func (c *captureProcessor) ProcessStdout(line string) string {
	c.lines = append(c.lines, line)
	return line
}

func (c *captureProcessor) ProcessStderr(line string) string {
	c.lines = append(c.lines, line)
	return line
}

func (c *captureProcessor) Flush(_ JobOutcome) {}

func TestLogStreamerPartialLineStdout(t *testing.T) {
	cap := &captureProcessor{}
	s := NewLogStreamer(zerolog.Nop(), cap)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	// Write first half — no newline yet.
	_, _ = s.Stdout.Write([]byte("par"))
	time.Sleep(100 * time.Millisecond)

	// Complete the line, then write a trailing partial with no newline.
	_, _ = s.Stdout.Write([]byte("tial\ntrailing-no-newline"))
	time.Sleep(100 * time.Millisecond)

	s.Flush(JobOutcome{})

	autopilot.Equals(t, []string{"partial", "trailing-no-newline"}, cap.lines)
}

func TestLogStreamerPartialLineStderr(t *testing.T) {
	cap := &captureProcessor{}
	s := NewLogStreamer(zerolog.Nop(), cap)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	_, _ = s.Stderr.Write([]byte("par"))
	time.Sleep(100 * time.Millisecond)

	_, _ = s.Stderr.Write([]byte("tial\ntrailing-no-newline"))
	time.Sleep(100 * time.Millisecond)

	s.Flush(JobOutcome{})

	autopilot.Equals(t, []string{"partial", "trailing-no-newline"}, cap.lines)
}
