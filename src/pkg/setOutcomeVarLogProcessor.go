package pkg

import (
	"regexp"
	"sync"
)

type OutcomeVariable struct {
	Key string
	Value string
}

type SetOutcomeVarLogProcessor struct {
	mutex sync.Mutex
	regex *regexp.Regexp
	keyIndex int
	valueIndex int
	vars []OutcomeVariable
}

func NewSetOutcomeVarLogProcessor() *SetOutcomeVarLogProcessor {
	exp := regexp.MustCompile(`^::set-outcome-var\s(?P<Key>[\w-]+)=(?P<Value>.*)`)
	return &SetOutcomeVarLogProcessor{
		mutex: sync.Mutex{},
		regex: exp,
		keyIndex: exp.SubexpIndex("Key"),
		valueIndex: exp.SubexpIndex("Value"),
		vars: []OutcomeVariable{},
	}
}

func (c *SetOutcomeVarLogProcessor) Process(line string) string {
	data := c.regex.FindStringSubmatch(line)
	if len(data) > 0 {
		c.mutex.Lock()
		defer c.mutex.Unlock()
		c.vars = append(c.vars, OutcomeVariable{
			Key: data[c.keyIndex],
			Value: data[c.valueIndex],
		})
		return ""
	}
	return line
}

func (c *SetOutcomeVarLogProcessor) Variables() []OutcomeVariable {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.vars
}
