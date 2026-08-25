package pkg

import (
	"container/ring"
	"context"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

type LogProcessor interface {
	ProcessStdout(line string) string
	ProcessStderr(line string) string
	Flush(outcome JobOutcome)
}

type LogStreamer struct {
	Stdout     *SafeBuffer
	Stderr     *SafeBuffer
	processors []LogProcessor
	logger     zerolog.Logger
	quit       chan bool
	done       chan struct{}
	logBuffer  *ring.Ring
}

func NewLogStreamer(logger zerolog.Logger, processors ...LogProcessor) LogStreamer {
	return LogStreamer{
		Stdout:     &SafeBuffer{},
		Stderr:     &SafeBuffer{},
		processors: processors,
		logger:     logger,
		quit:       make(chan bool),
		done:       make(chan struct{}),
		logBuffer:  ring.New(20),
	}
}

func (s *LogStreamer) AddProcessor(processor LogProcessor) {
	s.processors = append(s.processors, processor)
}

type logStream struct {
	buf *SafeBuffer
	fn  func(LogProcessor, string) string
}

func (s *LogStreamer) streams() []logStream {
	return []logStream{
		{s.Stderr, LogProcessor.ProcessStderr},
		{s.Stdout, LogProcessor.ProcessStdout},
	}
}

func (s *LogStreamer) processLine(stream logStream) bool {
	line, _ := stream.buf.ReadString('\n')
	if line == "" {
		return false
	}
	line = strings.TrimSuffix(line, "\n")
	for _, processor := range s.processors {
		line = stream.fn(processor, line)
	}
	s.logBuffer.Value = line
	s.logBuffer = s.logBuffer.Next()
	return true
}

func (s *LogStreamer) GetLogBuffer() []string {
	output := make([]string, 0)
	s.logBuffer.Do(func(line any) {
		if line != nil {
			output = append(output, line.(string))
		}
	})
	return output
}

func (s *LogStreamer) Run(ctx context.Context) {
	defer close(s.done)
	s.logger.Trace().Msg("Starting log streamer ...")
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.logger.Trace().Msg("Shutting down log streamer ...")
			return
		case <-s.quit:
			s.logger.Trace().Msg("Shutting down log streamer ...")
			return
		case <-ticker.C:
			for _, stream := range s.streams() {
				for strings.Contains(stream.buf.String(), "\n") {
					s.processLine(stream)
				}
			}
		}
	}
}

func (s *LogStreamer) Flush(outcome JobOutcome) {
	s.logger.Trace().Msg("Starting log streamer flush ...")
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.After(30 * time.Second)
wait:
	for strings.Contains(s.Stderr.String(), "\n") || strings.Contains(s.Stdout.String(), "\n") {
		select {
		case <-ticker.C:
			// Continue waiting
		case <-s.done:
			// 'Run' already exited (context cancelled); drain the rest below.
			break wait
		case <-timeout:
			s.logger.Warn().Msg("Flush timeout reached, proceeding with remaining data")
			break wait
		}
	}
	// Stop 'Run' if it is still going — it may have already exited via ctx.Done,
	// in which case there is no receiver for quit and sending would block forever.
	select {
	case s.quit <- true:
	case <-s.done:
	}
	<-s.done
	s.logger.Trace().Msg("Finished log streamer flush ...")
	// Drain anything 'Run' did not process, including a partial line that never
	// received a terminating newline.
	for _, stream := range s.streams() {
		for s.processLine(stream) {
		}
	}
	s.logger.Trace().Msg("Flushing log processors ...")
	for i := len(s.processors) - 1; i >= 0; i-- {
		s.processors[i].Flush(outcome)
	}
}
