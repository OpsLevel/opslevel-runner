package pkg

import (
	"github.com/rs/zerolog"
)

type LoggerLogProcessor struct {
	logger zerolog.Logger
}

func NewLoggerLogProcessor(logger zerolog.Logger) *LoggerLogProcessor {
	return &LoggerLogProcessor{
		logger: logger,
	}
}

func (s *LoggerLogProcessor) Process(line string) string {
	if len(line) > 0 {
		s.logger.Info().Msgf(line)
	}
	return line
}

func (s *LoggerLogProcessor) ProcessStdout(line string) string {
	return s.Process(line)
}

func (s *LoggerLogProcessor) ProcessStderr(line string) string {
	return s.Process(line)
}

func (s *LoggerLogProcessor) Flush(outcome JobOutcome) {}
