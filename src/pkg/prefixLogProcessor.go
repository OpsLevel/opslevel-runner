package pkg

import (
	"fmt"
)

type PrefixLogProcessor struct {
	prefix string
}

func NewPrefixLogProcessor(prefix string) *PrefixLogProcessor {
	return &PrefixLogProcessor{
		prefix: prefix,
	}
}

func (s *PrefixLogProcessor) Process(line string) string {
	if len(line) > 0 {
		return fmt.Sprintf("%s%s", s.prefix, line)
	}
	return line
}
