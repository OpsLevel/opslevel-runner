package cmd

import (
	"fmt"
	faktory "github.com/contribsys/faktory/client"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	"os"
	"time"
)

type FaktoryJobDefinition struct {
	Batch      string        `json:"batch"`
	Queue      string        `json:"queue"`
	Type       string        `json:"type"`
	Args       []interface{} `json:"args"`
	At         string        `json:"at"`
	Retries    int           `json:"retries"`
	ReserveFor int           `json:"reserve_for"`
	Backtrace  int           `json:"backtrace"`

	UniqueFor   uint                `json:"unique_for"`
	UniqueUntil faktory.UniqueUntil `json:"unique_until"`
	ExpiresIn   int                 `json:"expires_in"`
}

var enqueueCmd = &cobra.Command{
	Use: "enqueue",
	Run: func(cmd *cobra.Command, args []string) {
		job, err := readFaktoryJobInput()
		cobra.CheckErr(err)
		client, err := faktory.Open()
		cobra.CheckErr(err)

		j := faktory.NewJob(job.Type, job.Args...)
		j.Queue = job.Queue
		j.At = job.At
		j.ReserveFor = job.ReserveFor
		if job.Retries > 0 {
			j.Retry = &job.Retries
		}
		j.Backtrace = job.Backtrace
		if job.UniqueFor > 0 {
			j.SetUniqueFor(job.UniqueFor)
		}
		if job.UniqueUntil != "" {
			j.SetUniqueness(job.UniqueUntil)
		}
		if job.ExpiresIn > 0 {
			j.SetExpiresIn(time.Duration(job.ExpiresIn) * time.Second)
		}

		if job.Batch != "" {
			batch, err := client.BatchOpen(job.Batch)
			cobra.CheckErr(err)
			err = batch.Push(j)
			cobra.CheckErr(err)
		} else {
			err := client.Push(j)
			cobra.CheckErr(err)
		}
	},
}

func init() {
	rootCmd.AddCommand(enqueueCmd)
	enqueueCmd.Flags().StringVarP(&jobFile, "file", "f", ".", "File to read data from. If '-' then reads from stdin. Defaults to read from './job.yaml'")
}

func readFaktoryJobInput() (*FaktoryJobDefinition, error) {
	if jobFile == "" {
		return nil, fmt.Errorf("please specify a job file")
	}

	var job FaktoryJobDefinition

	if jobFile == "-" {
		if err := yaml.NewDecoder(os.Stdin).Decode(&job); err != nil {
			return nil, err
		}
	} else {
		if jobFile == "." {
			jobFile = "./job.yaml"
		}
		data, err := os.ReadFile("./job.yaml")
		if err != nil {
			return nil, err
		}
		if err := yaml.Unmarshal(data, &job); err != nil {
			return nil, err
		}
	}

	return &job, nil
}
