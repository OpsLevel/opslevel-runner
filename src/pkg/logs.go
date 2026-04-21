package pkg

import (
	"bytes"
	"container/ring"
	"context"
	"io"
	"sync"

	"github.com/rs/zerolog"
)

const (
	// maxLineBytes caps a single emitted log line. Longer runs without a
	// newline are force-flushed so a misbehaving process cannot grow the
	// buffer without bound.
	maxLineBytes = 64 * 1024

	// boundaryOverlap is the tail of the buffer retained across a
	// force-flush cut so a pattern (redacted secret, ::set-outcome-var
	// directive, etc.) straddling the cut still has a chance to match on
	// the next emission. Must be at least as large as the longest pattern
	// any processor cares about.
	boundaryOverlap = 1024

	// readChunk is the per-read size pulled from the pipe reader.
	readChunk = 4 * 1024
)

type LogProcessor interface {
	ProcessStdout(line string) string
	ProcessStderr(line string) string
	Flush(outcome JobOutcome)
}

// BoundarySafeProcessor is implemented by processors whose redaction must
// apply across force-flush chunk boundaries for oversized no-newline lines.
// When present, LogStreamer runs SanitizeBoundary over the full accumulated
// buffer before cutting, so a secret that would otherwise straddle the cut
// is masked in both the emitted prefix and the retained tail.
type BoundarySafeProcessor interface {
	SanitizeBoundary(line string) string
}

type LogStreamer struct {
	Stdout *io.PipeWriter
	Stderr *io.PipeWriter

	stdoutR *io.PipeReader
	stderrR *io.PipeReader

	processors []LogProcessor
	logger     zerolog.Logger

	logBuffer   *ring.Ring
	logBufferMu sync.Mutex

	wg        sync.WaitGroup
	closeOnce sync.Once
}

// NewLogStreamer constructs a LogStreamer and starts the two reader
// goroutines that drain Stdout/Stderr. Callers MUST call Flush exactly once
// per streamer or the reader goroutines leak. Callers SHOULD also
// `go Run(ctx)` to propagate ctx cancellation — without it, a stuck exec
// write cannot be unblocked by cancellation.
func NewLogStreamer(logger zerolog.Logger, processors ...LogProcessor) *LogStreamer {
	sor, sow := io.Pipe()
	ser, sew := io.Pipe()
	s := &LogStreamer{
		Stdout:     sow,
		Stderr:     sew,
		stdoutR:    sor,
		stderrR:    ser,
		processors: processors,
		logger:     logger,
		logBuffer:  ring.New(20),
	}
	s.wg.Add(2)
	go s.readLoop(s.stdoutR, LogProcessor.ProcessStdout)
	go s.readLoop(s.stderrR, LogProcessor.ProcessStderr)
	return s
}

// AddProcessor appends a processor to the chain. Must be called before the
// first Write to Stdout/Stderr: the reader goroutines observe s.processors
// without a lock, relying on the io.Pipe Write→Read happens-before to
// publish the slice. Mutating after writes begin is a data race.
func (s *LogStreamer) AddProcessor(processor LogProcessor) {
	s.processors = append(s.processors, processor)
}

// Run watches ctx and, on cancellation, closes the pipe readers so any
// in-flight write from the k8s exec stream fails fast rather than blocking.
// Returns only after the internal reader goroutines have exited, so callers
// can rely on the log buffer being fully populated on return. Safe to call
// without a corresponding Flush; safe to omit entirely (Flush alone suffices
// for a successful run — Run only matters for ctx-driven cancellation).
func (s *LogStreamer) Run(ctx context.Context) {
	s.logger.Trace().Msg("Starting log streamer ...")
	defer s.logger.Trace().Msg("Log streamer stopped.")
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-ctx.Done():
		_ = s.stdoutR.CloseWithError(ctx.Err())
		_ = s.stderrR.CloseWithError(ctx.Err())
		<-done
	case <-done:
	}
}

// readLoop reads from r, extracts newline-terminated lines, and emits each
// through processFn. Exits on io.EOF (writer closed) or any read error,
// emitting any trailing no-newline partial line first.
func (s *LogStreamer) readLoop(r io.Reader, processFn func(LogProcessor, string) string) {
	defer s.wg.Done()
	buf := make([]byte, 0, maxLineBytes+readChunk)
	readBuf := make([]byte, readChunk)
	for {
		n, err := r.Read(readBuf)
		if n > 0 {
			buf = append(buf, readBuf[:n]...)
			buf = s.drain(buf, processFn)
		}
		if err != nil {
			if len(buf) > 0 {
				s.emit(string(buf), processFn)
			}
			return
		}
	}
}

// drain emits every complete line in buf and, if the remaining tail has
// reached maxLineBytes without a newline, force-flushes a prefix while
// retaining a boundaryOverlap tail. Before cutting, any BoundarySafeProcessor
// masks the entire buffer so a secret straddling the cut is redacted in both
// halves. Returns the residual buffer to carry into the next read.
func (s *LogStreamer) drain(buf []byte, processFn func(LogProcessor, string) string) []byte {
	for {
		if i := bytes.IndexByte(buf, '\n'); i >= 0 {
			s.emit(string(buf[:i]), processFn)
			buf = buf[i+1:]
			continue
		}
		if len(buf) < maxLineBytes {
			return buf
		}
		full := string(buf)
		for _, p := range s.processors {
			if bp, ok := p.(BoundarySafeProcessor); ok {
				full = bp.SanitizeBoundary(full)
			}
		}
		if len(full) <= boundaryOverlap {
			return append(buf[:0], full...)
		}
		cut := len(full) - boundaryOverlap
		s.emit(full[:cut], processFn)
		return append(buf[:0], full[cut:]...)
	}
}

func (s *LogStreamer) emit(line string, processFn func(LogProcessor, string) string) {
	for _, processor := range s.processors {
		line = processFn(processor, line)
	}
	s.logBufferMu.Lock()
	s.logBuffer.Value = line
	s.logBuffer = s.logBuffer.Next()
	s.logBufferMu.Unlock()
}

func (s *LogStreamer) GetLogBuffer() []string {
	s.logBufferMu.Lock()
	defer s.logBufferMu.Unlock()
	output := make([]string, 0)
	s.logBuffer.Do(func(line any) {
		if line != nil {
			output = append(output, line.(string))
		}
	})
	return output
}

// Flush closes the pipe writers (signaling EOF to the reader goroutines),
// waits for the readers to drain, then flushes the processor chain in
// reverse order. Safe to call multiple times.
func (s *LogStreamer) Flush(outcome JobOutcome) {
	s.logger.Trace().Msg("Starting log streamer flush ...")
	s.closeOnce.Do(func() {
		_ = s.Stdout.Close()
		_ = s.Stderr.Close()
	})
	s.wg.Wait()
	s.logger.Trace().Msg("Flushing log processors ...")
	for i := len(s.processors) - 1; i >= 0; i-- {
		s.processors[i].Flush(outcome)
	}
}
