package cmd

import (
	"bytes"
	"fmt"
	"os"

	"github.com/opslevel/opslevel-go/v2024"
	"gopkg.in/yaml.v3"
)

func readJobInput[T any]() (*T, error) {
	var err error
	var input []byte
	switch jobFile {
	case "":
		return nil, fmt.Errorf("please specify a job file")
	case "-":
		if isStdInFromTerminal() {
			fmt.Println("Reading input directly from command line... Press CTRL+D to stop typing")
		}
		buf := bytes.Buffer{}
		_, err = buf.ReadFrom(os.Stdin)
		input = buf.Bytes()
	default:
		if jobFile == "." {
			jobFile = "./job.yaml"
		}
		input, err = os.ReadFile(jobFile)
	}
	if err != nil {
		return nil, err
	}

	var job T
	err = yaml.Unmarshal(input, &job)
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// TODO: test this
func readFaktoryJobInput() (*FaktoryJobDefinition, error) {
	return readJobInput[FaktoryJobDefinition]()
}

// TODO: test this
func readRunnerInput() (*opslevel.RunnerJob, error) {
	return readJobInput[opslevel.RunnerJob]()
}
