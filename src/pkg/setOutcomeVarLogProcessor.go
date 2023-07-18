package pkg

import (
	"encoding/json"
	"fmt"
	"github.com/opslevel/opslevel-go/v2023"
	"github.com/rs/zerolog"
	"regexp"
	"strings"
)

var skipCapture = regexp.MustCompile(`^\+\s.*$`)
var setOutcomeVarExp = regexp.MustCompile(`^::set-outcome-var\s(?P<Key>[\w-]+)=(?P<Value>.*)`)
var startOutcomeVarExp = regexp.MustCompile(`^::start-multiline-outcome-var\s(?P<Key>[\w-]+)`)
var endOutcomeVarExp = regexp.MustCompile(`^::end-multiline-outcome-var`)

type SetOutcomeVarLogProcessor struct {
	client                 *opslevel.Client
	logger                 zerolog.Logger
	runnerId               opslevel.ID
	jobId                  opslevel.ID
	jobNumber              string
	multilineOutcomeVarKey Stack[string]
	vars                   map[string]string
}

func NewSetOutcomeVarLogProcessor(client *opslevel.Client, logger zerolog.Logger, runnerId opslevel.ID, jobId opslevel.ID, jobNumber string) *SetOutcomeVarLogProcessor {
	return &SetOutcomeVarLogProcessor{
		client:                 client,
		logger:                 logger,
		runnerId:               runnerId,
		jobId:                  jobId,
		jobNumber:              jobNumber,
		multilineOutcomeVarKey: NewStack[string](""),
		vars:                   map[string]string{},
	}
}

func (s *SetOutcomeVarLogProcessor) Process(line string) string {
	if skipCapture.MatchString(line) {
		return line
	}
	// This is like a poor man's state machine
	startOutcomeData := startOutcomeVarExp.FindStringSubmatch(line)
	if len(startOutcomeData) > 0 {
		s.multilineOutcomeVarKey.Push(startOutcomeData[1])
		return ""
	}
	endOutcomeData := endOutcomeVarExp.FindStringSubmatch(line)
	currentKey := s.multilineOutcomeVarKey.Peek()
	if currentKey != "" {
		if len(endOutcomeData) > 0 {
			key := s.multilineOutcomeVarKey.Pop()
			if key != "" {
				s.vars[key] = strings.TrimSuffix(s.vars[key], "\n")
				return ""
			}
			return ""
		}
		s.vars[currentKey] += fmt.Sprintf("%s\n", line)
		return ""
	}

	varData := setOutcomeVarExp.FindStringSubmatch(line)
	if len(varData) > 0 {
		s.vars[varData[1]] = varData[2]
		return ""
	}
	return line
}

func (s *SetOutcomeVarLogProcessor) ProcessStdout(line string) string {
	return s.Process(line)
}

func (s *SetOutcomeVarLogProcessor) ProcessStderr(line string) string {
	// We don't want to process stderr lines as they will never contain outcome var data
	return line
}

func (s *SetOutcomeVarLogProcessor) Flush(outcome JobOutcome) {
	vars := []opslevel.RunnerJobOutcomeVariable{}
	for k, v := range s.vars {
		vars = append(vars, opslevel.RunnerJobOutcomeVariable{
			Key:   k,
			Value: v,
		})
	}
	s.logger.Debug().Msgf("Outcome Variables:")
	bytes, _ := json.MarshalIndent(vars, "    ", "  ")
	s.logger.Debug().Msg(string(bytes))

	if outcome.Outcome != opslevel.RunnerJobOutcomeEnumSuccess {
		s.logger.Warn().Msgf("Job '%s' failed REASON: %s", s.jobNumber, outcome.Message)
	}
	if s.client == nil {
		return
	}

	err := s.client.RunnerReportJobOutcome(opslevel.RunnerReportJobOutcomeInput{
		RunnerId:         opslevel.ID(s.runnerId),
		RunnerJobId:      opslevel.ID(s.jobId),
		Outcome:          outcome.Outcome,
		OutcomeVariables: vars,
	})
	if err != nil {
		s.logger.Error().Err(err).Msgf("error when reporting outcome '%s' for job '%s'", outcome.Outcome, s.jobNumber)
	}
}
