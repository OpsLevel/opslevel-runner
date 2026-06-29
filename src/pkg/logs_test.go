package pkg

import (
	"context"
	"strings"
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
	s := NewLogStreamer(zerolog.Nop(), 0, cap)

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

// Data with no newline that exceeds the buffer cap must be bounded to the cap
// (excess dropped) and the capped portion flushed at job end.
func TestLogStreamerCapsOversizedLine(t *testing.T) {
	cap := &captureProcessor{}
	s := NewLogStreamer(zerolog.Nop(), 64, cap)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	// 200 bytes, no newline: 64 are buffered, 136 dropped.
	_, _ = s.Stdout.Write([]byte(strings.Repeat("x", 200)))
	time.Sleep(150 * time.Millisecond)
	s.Flush(JobOutcome{})

	autopilot.Equals(t, []string{strings.Repeat("x", 64)}, cap.lines)
}

// SafeBuffer must cap resident memory and report what it dropped.
func TestSafeBufferCapsAndReportsDrops(t *testing.T) {
	b := NewSafeBuffer(10)
	n, _ := b.Write([]byte("0123456789ABCDEF")) // 16 bytes into a 10-byte cap
	autopilot.Equals(t, 16, n)                  // caller always sees a full write
	autopilot.Equals(t, 10, b.Len())
	autopilot.Equals(t, 6, b.DroppedBytes())
	autopilot.Equals(t, 0, b.DroppedBytes()) // counter resets after read
}

func TestLogStreamerPartialLineStderr(t *testing.T) {
	cap := &captureProcessor{}
	s := NewLogStreamer(zerolog.Nop(), 0, cap)

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
