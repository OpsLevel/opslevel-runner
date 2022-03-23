package pkg

import "encoding/json"

type JobEnvSchema struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	Sensitive bool   `json:"sensitive"`
}

type JobSchema struct {
	JobId    string         `json:"job_id" mapstructure:"job_id"`
	Image    string         `json:"image"`
	Commands []string       `json:"commands"`
	Config   []JobEnvSchema `json:"config"`
}

func (s *JobSchema) ToJson() (string, error) {
	data, err := json.MarshalIndent(s, "", "  ")
	return string(data), err
}
