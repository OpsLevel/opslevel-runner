package pkg

import (
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

type LogStreamer struct {
	logger zerolog.Logger
	stdout *SafeBuffer
	stderr *SafeBuffer
}

func NewLogStreamer(logger zerolog.Logger, stdout, stderr *SafeBuffer) LogStreamer {
	return LogStreamer{logger: logger, stdout: stdout, stderr: stderr}
}

func (s LogStreamer) Run(job_id string) {
	for {
		for len(s.stdout.String()) > 0 {
			line, err := s.stdout.ReadString('\n')
			if err == nil {
				logLine := fmt.Sprintf("[%s] %s", job_id, strings.TrimSuffix(line, "\n"))
				// TODO: Sanitize Log Line
				s.logger.Info().Msgf(logLine)
			}
		}
		for len(s.stderr.String()) > 0 {
			line, err := s.stderr.ReadString('\n')
			if err == nil {
				logLine := fmt.Sprintf("[%s] %s", job_id, strings.TrimSuffix(line, "\n"))
				// TODO: Sanitize Log Line
				s.logger.Error().Msgf(logLine)
			}
		}
	}
}

type OpsLevelLogWriter struct {
	id                string
	maxTime           time.Duration
	maxSize           int
	cache             []byte
	timeSinceLastEmit time.Time
}

func NewOpsLevelLogWriter(jobID string, maxTime time.Duration, maxSize int) OpsLevelLogWriter {
	return OpsLevelLogWriter{
		id:                jobID,
		maxTime:           maxTime,
		maxSize:           maxSize,
		cache:             []byte{},
		timeSinceLastEmit: time.Now(),
	}
}

func (s *OpsLevelLogWriter) Write(p []byte) (n int, err error) {
	s.cache = append(s.cache, p...)
	if time.Since(s.timeSinceLastEmit) > s.maxTime {
		s.Emit()
	}
	if len(s.cache) > s.maxSize {
		s.Emit()
	}
	return len(p), nil
}

func (s *OpsLevelLogWriter) Emit() {
	// TODO: Send API request back to OpsLevel with all s.cache log lines
	// fmt.Printf("Emitting '%d' bytes to OpsLevel\n", len(s.cache))
	// fmt.Printf("##########\n")
	// fmt.Printf("%s", string(s.cache))
	// fmt.Printf("##########\n")
	s.cache = []byte{}
	s.timeSinceLastEmit = time.Now()
}
