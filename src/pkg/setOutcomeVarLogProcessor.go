package pkg

import (
	"regexp"
	"sync"

	"github.com/rs/zerolog/log"
)

type OutcomeVariable struct {
	Key   string
	Value string
}

type SetOutcomeVarLogProcessor struct {
	mutex      sync.Mutex
	regex      *regexp.Regexp
	keyIndex   int
	valueIndex int
	vars       []OutcomeVariable
}

func NewSetOutcomeVarLogProcessor() *SetOutcomeVarLogProcessor {
	exp := regexp.MustCompile(`^::set-outcome-var\s(?P<Key>[\w-]+)=(?P<Value>.*)`)
	return &SetOutcomeVarLogProcessor{
		mutex:      sync.Mutex{},
		regex:      exp,
		keyIndex:   exp.SubexpIndex("Key"),
		valueIndex: exp.SubexpIndex("Value"),
		vars:       []OutcomeVariable{},
	}
}

func (s *SetOutcomeVarLogProcessor) Process(line string) string {
	data := s.regex.FindStringSubmatch(line)
	if len(data) > 0 {
		s.mutex.Lock()
		defer s.mutex.Unlock()
		s.vars = append(s.vars, OutcomeVariable{
			Key:   data[s.keyIndex],
			Value: data[s.valueIndex],
		})
		return ""
	}
	return line
}

func (s *SetOutcomeVarLogProcessor) Flush() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	for _, v := range s.vars {
		log.Info().Msgf("Outcome Variable | '%s'='%s'", v.Key, v.Value)
	}
}
