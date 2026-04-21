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
	logBuffer  *ring.Ring
}

func NewLogStreamer(logger zerolog.Logger, processors ...LogProcessor) LogStreamer {
	quit := make(chan bool)
	return LogStreamer{
		Stdout:     &SafeBuffer{},
		Stderr:     &SafeBuffer{},
		processors: processors,
		logger:     logger,
		quit:       quit,
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

func (s *LogStreamer) processLine(stream logStream) {
	line, _ := stream.buf.ReadString('\n')
	if line == "" {
		return
	}
	line = strings.TrimSuffix(line, "\n")
	for _, processor := range s.processors {
		line = stream.fn(processor, line)
	}
	s.logBuffer.Value = line
	s.logBuffer = s.logBuffer.Next()
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
	for strings.Contains(s.Stderr.String(), "\n") || strings.Contains(s.Stdout.String(), "\n") {
		select {
		case <-ticker.C:
			// Continue waiting
		case <-timeout:
			s.logger.Warn().Msg("Flush timeout reached, proceeding with remaining data")
			goto done
		}
	}
done:
	s.logger.Trace().Msg("Finished log streamer flush ...")
	s.quit <- true
	time.Sleep(200 * time.Millisecond) // Allow 'Run' goroutine to quit
	// Drain any partial line that never received a terminating newline.
	for _, stream := range s.streams() {
		s.processLine(stream)
	}
	s.logger.Trace().Msg("Flushing log processors ...")
	for i := len(s.processors) - 1; i >= 0; i-- {
		s.processors[i].Flush(outcome)
	}
}
