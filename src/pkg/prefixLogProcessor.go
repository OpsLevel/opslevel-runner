package pkg

import (
	"fmt"
)

type PrefixLogProcessor struct {
	prefix func() string
}

func NewPrefixLogProcessor(prefix func() string) *PrefixLogProcessor {
	return &PrefixLogProcessor{
		prefix: prefix,
	}
}

func (s *PrefixLogProcessor) Process(line string) string {
	return fmt.Sprintf("%s%s", s.prefix(), line)
}

func (s *PrefixLogProcessor) ProcessStdout(line string) string {
	return s.Process(line)
}

func (s *PrefixLogProcessor) ProcessStderr(line string) string {
	return s.Process(line)
}

func (s *PrefixLogProcessor) Flush(outcome JobOutcome) {}
