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

func TestLogStreamerFlushAfterContextCancel(t *testing.T) {
	cap := &captureProcessor{}
	s := NewLogStreamer(zerolog.Nop(), cap)

	ctx, cancel := context.WithCancel(context.Background())
	go s.Run(ctx)

	_, _ = s.Stdout.Write([]byte("line-one\nline-two\n"))
	time.Sleep(100 * time.Millisecond)

	// Cancel the context so 'Run' exits on its own, then write more lines that
	// only Flush can drain.
	cancel()
	<-s.done
	_, _ = s.Stdout.Write([]byte("late-one\nlate-two"))

	flushed := make(chan struct{})
	go func() {
		s.Flush(JobOutcome{})
		close(flushed)
	}()
	select {
	case <-flushed:
	case <-time.After(5 * time.Second):
		t.Fatal("Flush deadlocked after context cancellation")
	}

	autopilot.Equals(t, []string{"line-one", "line-two", "late-one", "late-two"}, cap.lines)
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
