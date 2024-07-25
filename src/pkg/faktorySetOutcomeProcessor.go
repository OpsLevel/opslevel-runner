package pkg

import (
	"fmt"
	"strings"

	faktory "github.com/contribsys/faktory/client"
	faktoryWorker "github.com/contribsys/faktory_worker_go"
	"github.com/opslevel/opslevel-go/v2024"
	"github.com/rs/zerolog"
)

type FaktorySetOutcomeProcessor struct {
	client                 *faktory.Client
	helper                 faktoryWorker.Helper
	logger                 zerolog.Logger
	jobId                  opslevel.ID
	multilineOutcomeVarKey Stack[string]
	vars                   map[string]string
}

func NewFaktorySetOutcomeProcessor(helper faktoryWorker.Helper, logger zerolog.Logger, jobId opslevel.ID) *FaktorySetOutcomeProcessor {
	client, _ := faktory.Open()
	return &FaktorySetOutcomeProcessor{
		client:                 client,
		helper:                 helper,
		logger:                 logger,
		jobId:                  jobId,
		multilineOutcomeVarKey: NewStack[string](""),
		vars:                   map[string]string{},
	}
}

func (s *FaktorySetOutcomeProcessor) Process(line string) string {
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

func (s *FaktorySetOutcomeProcessor) ProcessStdout(line string) string {
	return s.Process(line)
}

func (s *FaktorySetOutcomeProcessor) ProcessStderr(line string) string {
	// We don't want to process stderr lines as they will never contain outcome var data
	return line
}

func (s *FaktorySetOutcomeProcessor) Flush(outcome JobOutcome) {
	vars := make([]opslevel.RunnerJobOutcomeVariable, 0)
	for k, v := range s.vars {
		vars = append(vars, opslevel.RunnerJobOutcomeVariable{
			Key:   k,
			Value: v,
		})
	}
	payload := opslevel.RunnerReportJobOutcomeInput{
		RunnerId:         "faktory",
		RunnerJobId:      s.jobId,
		Outcome:          outcome.Outcome,
		OutcomeVariables: vars,
	}
	job := faktory.NewJob("Runners::Faktory::ReportJobOutcome", payload)
	job.Queue = "app"
	batch := s.helper.Bid()
	if batch != "" {
		err := s.helper.Batch(func(b *faktory.Batch) error {
			return b.Push(job)
		})
		if err != nil {
			MetricEnqueueBatchFailed.Inc()
			s.logger.Error().Err(err).Msgf("error when reporting outcome '%s' for job '%s'", outcome.Outcome, s.jobId)
		}
	} else {
		err := s.client.Push(job)
		if err != nil {
			MetricEnqueueFailed.Inc()
			s.logger.Error().Err(err).Msgf("error when reporting outcome '%s' for job '%s'", outcome.Outcome, s.jobId)
		}
	}
}
