package pkg

import (
	"github.com/opslevel/opslevel-go"
	"regexp"
	"sync"

	"github.com/rs/zerolog/log"
)

var setOutcomeVarExp = regexp.MustCompile(`^::set-outcome-var\s(?P<Key>[\w-]+)=(?P<Value>.*)`)

type SetOutcomeVarLogProcessor struct {
	mutex      sync.Mutex
	regex      *regexp.Regexp
	keyIndex   int
	valueIndex int
	vars       []opslevel.RunnerJobOutcomeVariable
}

func NewSetOutcomeVarLogProcessor() *SetOutcomeVarLogProcessor {
	return &SetOutcomeVarLogProcessor{
		mutex:      sync.Mutex{},
		regex:      setOutcomeVarExp,
		keyIndex:   setOutcomeVarExp.SubexpIndex("Key"),
		valueIndex: setOutcomeVarExp.SubexpIndex("Value"),
		vars:       []opslevel.RunnerJobOutcomeVariable{},
	}
}

func (s *SetOutcomeVarLogProcessor) Process(line string) string {
	data := s.regex.FindStringSubmatch(line)
	if len(data) > 0 {
		s.mutex.Lock()
		defer s.mutex.Unlock()
		s.vars = append(s.vars, opslevel.RunnerJobOutcomeVariable{
			Key:   data[s.keyIndex],
			Value: data[s.valueIndex],
		})
		return ""
	}
	return line
}

func (s *SetOutcomeVarLogProcessor) PrintVariables() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	for _, v := range s.vars {
		log.Info().Msgf("Outcome Variable | '%s'='%s'", v.Key, v.Value)
	}
}

func (s *SetOutcomeVarLogProcessor) Variables() []opslevel.RunnerJobOutcomeVariable {
	return s.vars
}
