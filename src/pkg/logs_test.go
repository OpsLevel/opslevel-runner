package pkg

import (
	"context"
	"sync/atomic"
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

type tickCountProcessor struct {
	captureProcessor
	ticks atomic.Int32
}

func (c *tickCountProcessor) Tick() {
	c.ticks.Add(1)
}

func TestLogStreamerCallsTickWhileIdle(t *testing.T) {
	proc := &tickCountProcessor{}
	s := NewLogStreamer(zerolog.Nop(), proc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	// No lines are ever written; the streamer should still tick processors.
	time.Sleep(200 * time.Millisecond)

	autopilot.Assert(t, proc.ticks.Load() > 0, "expected Tick to be called while idle")
}

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
