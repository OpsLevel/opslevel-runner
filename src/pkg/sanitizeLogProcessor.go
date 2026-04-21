package pkg

import (
	"strings"

	"github.com/opslevel/opslevel-go/v2026"
)

type SanitizeLogProcessor struct {
	variables []opslevel.RunnerJobVariable
}

func NewSanitizeLogProcessor(variables []opslevel.RunnerJobVariable) *SanitizeLogProcessor {
	var secrets []opslevel.RunnerJobVariable
	for _, variable := range variables {
		if variable.Sensitive && variable.Value != "" {
			secrets = append(secrets, variable)
		}
	}
	return &SanitizeLogProcessor{
		variables: secrets,
	}
}

func (s *SanitizeLogProcessor) Process(line string) string {
	scrubbed := line
	for _, variable := range s.variables {
		scrubbed = strings.ReplaceAll(scrubbed, variable.Value, "**********")
	}
	return scrubbed
}

func (s *SanitizeLogProcessor) ProcessStdout(line string) string {
	return s.Process(line)
}

func (s *SanitizeLogProcessor) ProcessStderr(line string) string {
	return s.Process(line)
}

// SanitizeBoundary lets LogStreamer redact secrets across a force-flush
// chunk boundary before the prefix is emitted, so a secret straddling the
// cut is masked in both halves.
func (s *SanitizeLogProcessor) SanitizeBoundary(line string) string {
	return s.Process(line)
}

func (s *SanitizeLogProcessor) Flush(outcome JobOutcome) {}
