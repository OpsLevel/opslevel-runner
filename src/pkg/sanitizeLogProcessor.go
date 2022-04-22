package pkg

import (
	"strings"

	"github.com/opslevel/opslevel-go"
)

type SanitizeLogProcessor struct {
	variables []opslevel.RunnerJobVariable
}

func NewSanitizeLogProcessor(variables []opslevel.RunnerJobVariable) *SanitizeLogProcessor {
	var secrets []opslevel.RunnerJobVariable
	for _, variable := range variables {
		if variable.Sensitive {
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
