package pkg

import (
	"github.com/opslevel/opslevel-go"
	"strings"
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

func (m *SanitizeLogProcessor) Process(line string) string {
	scrubbed := line
	for _, variable := range m.variables {
		scrubbed = strings.ReplaceAll(scrubbed, variable.Value, "**********")
	}
	return scrubbed
}