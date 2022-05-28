package pkg

import (
	"github.com/opslevel/opslevel-go"
	"github.com/rs/zerolog"
	"regexp"
)

var setOutcomeVarExp = regexp.MustCompile(`^::set-outcome-var\s(?P<Key>[\w-]+)=(?P<Value>.*)`)

type SetOutcomeVarLogProcessor struct {
	client     *opslevel.Client
	logger     zerolog.Logger
	runnerId   string
	jobId      string
	regex      *regexp.Regexp
	keyIndex   int
	valueIndex int
	vars       []opslevel.RunnerJobOutcomeVariable
}

func NewSetOutcomeVarLogProcessor(client *opslevel.Client, logger zerolog.Logger, runnerId string, jobId string) *SetOutcomeVarLogProcessor {
	return &SetOutcomeVarLogProcessor{
		client:     client,
		logger:     logger,
		runnerId:   runnerId,
		jobId:      jobId,
		regex:      setOutcomeVarExp,
		keyIndex:   setOutcomeVarExp.SubexpIndex("Key"),
		valueIndex: setOutcomeVarExp.SubexpIndex("Value"),
		vars:       []opslevel.RunnerJobOutcomeVariable{},
	}
}

func (s *SetOutcomeVarLogProcessor) Process(line string) string {
	data := s.regex.FindStringSubmatch(line)
	if len(data) > 0 {
		s.vars = append(s.vars, opslevel.RunnerJobOutcomeVariable{
			Key:   data[s.keyIndex],
			Value: data[s.valueIndex],
		})
		return ""
	}
	return line
}

func (s *SetOutcomeVarLogProcessor) Flush(outcome JobOutcome) {
	for _, v := range s.vars {
		s.logger.Debug().Msgf("Outcome Variable | '%s'='%s'", v.Key, v.Value)
	}

	if outcome.Outcome != opslevel.RunnerJobOutcomeEnumSuccess {
		s.logger.Warn().Msgf("Job '%s' failed REASON: %s", s.jobId, outcome.Message)
	}
	if s.client == nil {
		return
	}
	err := s.client.RunnerReportJobOutcome(opslevel.RunnerReportJobOutcomeInput{
		RunnerId:         s.runnerId,
		RunnerJobId:      s.jobId,
		Outcome:          outcome.Outcome,
		OutcomeVariables: s.vars,
	})
	if err != nil {
		s.logger.Error().Err(err).Msgf("error when reporting outcome '%s' for job '%s'", outcome.Outcome, s.jobId)
	}
}
