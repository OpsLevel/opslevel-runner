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

func (s *SanitizeLogProcessor) Flush(outcome JobOutcome) {}
