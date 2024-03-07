package cmd

import (
	"time"

	faktory "github.com/contribsys/faktory/client"
	"github.com/spf13/cobra"
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

	Custom map[string]interface{} `json:"custom"`
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
		if len(job.Custom) > 0 {
			for key, value := range job.Custom {
				j.SetCustom(key, value)
			}
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
