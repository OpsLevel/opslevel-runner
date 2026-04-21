package pkg

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/opslevel/opslevel-go/v2026"
	"github.com/rocktavious/autopilot/v2023"
	"github.com/rs/zerolog"
)

type captureProcessor struct {
	mu    sync.Mutex
	lines []string
}

func (c *captureProcessor) ProcessStdout(line string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, line)
	return line
}

func (c *captureProcessor) ProcessStderr(line string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, line)
	return line
}

func (c *captureProcessor) Flush(_ JobOutcome) {}

func (c *captureProcessor) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.lines))
	copy(out, c.lines)
	return out
}

// A partial write without a newline is buffered until either more data arrives
// with a newline or the pipe is closed (via Flush) — at which point the
// trailing partial line is delivered as a final line.
func TestLogStreamerPartialLineStdout(t *testing.T) {
	proc := &captureProcessor{}
	s := NewLogStreamer(zerolog.Nop(), proc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	_, _ = s.Stdout.Write([]byte("par"))
	_, _ = s.Stdout.Write([]byte("tial\ntrailing-no-newline"))

	s.Flush(JobOutcome{})

	autopilot.Equals(t, []string{"partial", "trailing-no-newline"}, proc.snapshot())
}

func TestLogStreamerPartialLineStderr(t *testing.T) {
	proc := &captureProcessor{}
	s := NewLogStreamer(zerolog.Nop(), proc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	_, _ = s.Stderr.Write([]byte("par"))
	_, _ = s.Stderr.Write([]byte("tial\ntrailing-no-newline"))

	s.Flush(JobOutcome{})

	autopilot.Equals(t, []string{"partial", "trailing-no-newline"}, proc.snapshot())
}

// Flush must not deadlock when Run has already exited via ctx cancellation
// before Flush is called.
func TestLogStreamerFlushAfterContextCancel(t *testing.T) {
	proc := &captureProcessor{}
	s := NewLogStreamer(zerolog.Nop(), proc)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	_, _ = s.Stdout.Write([]byte("before-cancel\n"))
	cancel()
	<-done

	finished := make(chan struct{})
	go func() {
		s.Flush(JobOutcome{})
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("Flush deadlocked after ctx cancellation")
	}
}

// A line longer than maxLineBytes must be force-emitted in bounded chunks
// rather than growing the buffer without bound. Assert total bytes preserved,
// at least one force-flush emission plus trailing, and that each segment
// stays within the memory bound.
func TestLogStreamerOversizedPartial(t *testing.T) {
	proc := &captureProcessor{}
	s := NewLogStreamer(zerolog.Nop(), proc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	total := maxLineBytes + 100
	big := strings.Repeat("x", total)
	_, _ = s.Stdout.Write([]byte(big))

	s.Flush(JobOutcome{})

	lines := proc.snapshot()
	if len(lines) < 2 {
		t.Fatalf("expected force-flush + trailing (>=2 segments), got %d", len(lines))
	}
	sum := 0
	for _, l := range lines {
		if len(l) > maxLineBytes+readChunk {
			t.Fatalf("emitted segment of %d bytes exceeds bound %d", len(l), maxLineBytes+readChunk)
		}
		sum += len(l)
	}
	autopilot.Equals(t, total, sum)
}

// A secret that straddles the force-flush cut must still be redacted in both
// the emitted prefix and the retained tail — a BoundarySafeProcessor is
// applied to the full buffer before cutting.
func TestLogStreamerOversizedSanitizerBoundary(t *testing.T) {
	const secret = "s3cretpassword-XYZ" // 18 chars
	sanitizer := NewSanitizeLogProcessor([]opslevel.RunnerJobVariable{
		{Sensitive: true, Value: secret},
	})
	proc := &captureProcessor{}
	s := NewLogStreamer(zerolog.Nop(), sanitizer, proc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	// Centre the secret on the cut point: force-flush cuts at
	// len(buf) - boundaryOverlap, so placing the secret's midpoint at
	// maxLineBytes - boundaryOverlap makes it straddle both halves.
	prefixLen := maxLineBytes - boundaryOverlap - len(secret)/2
	suffixLen := maxLineBytes - prefixLen - len(secret)
	payload := strings.Repeat("x", prefixLen) + secret + strings.Repeat("y", suffixLen)
	_, _ = s.Stdout.Write([]byte(payload))

	s.Flush(JobOutcome{})

	for i, line := range proc.snapshot() {
		if strings.Contains(line, secret) {
			t.Fatalf("secret leaked unredacted in emitted segment %d", i)
		}
		// Also reject any partial-secret fragment of length >= 6 as a
		// conservative leak indicator.
		for n := 6; n < len(secret); n++ {
			if strings.Contains(line, secret[:n]) {
				t.Fatalf("secret prefix of len %d leaked in segment %d", n, i)
			}
			if strings.Contains(line, secret[len(secret)-n:]) {
				t.Fatalf("secret suffix of len %d leaked in segment %d", n, i)
			}
		}
	}
}

// GetLogBuffer must be safe to call concurrently with ongoing line emission.
// Meaningful under `go test -race`.
func TestLogStreamerGetLogBufferConcurrentAccess(t *testing.T) {
	proc := &captureProcessor{}
	s := NewLogStreamer(zerolog.Nop(), proc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_, _ = s.Stdout.Write([]byte("line\n"))
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = s.GetLogBuffer()
			time.Sleep(time.Millisecond)
		}
	}()
	wg.Wait()
	s.Flush(JobOutcome{})
}

// Flush is idempotent — calling it multiple times must not panic on
// double-close of the underlying pipes.
func TestLogStreamerFlushIdempotent(t *testing.T) {
	proc := &captureProcessor{}
	s := NewLogStreamer(zerolog.Nop(), proc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	_, _ = s.Stdout.Write([]byte("hello\n"))
	s.Flush(JobOutcome{})
	s.Flush(JobOutcome{})

	autopilot.Equals(t, []string{"hello"}, proc.snapshot())
}

// A writer blocked on io.Pipe must be unblocked when Run observes ctx
// cancellation — Run closes the reader side with the ctx error, which
// propagates to pending writes. Without this, a hung exec would leak the
// streamer.
func TestLogStreamerWriteUnblocksOnCancel(t *testing.T) {
	proc := &captureProcessor{}
	s := NewLogStreamer(zerolog.Nop(), proc)

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(runDone)
	}()

	writeDone := make(chan error, 1)
	go func() {
		_, err := s.Stdout.Write([]byte(strings.Repeat("x", 4*maxLineBytes)))
		writeDone <- err
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case <-writeDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Write did not unblock after ctx cancellation")
	}
	select {
	case <-runDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after ctx cancellation")
	}

	s.Flush(JobOutcome{})
}
