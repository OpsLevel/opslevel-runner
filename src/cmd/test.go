package cmd

import (
	"fmt"
	"os"

	"github.com/opslevel/opslevel-go"
	"github.com/opslevel/opslevel-runner/pkg"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var jobFile string

// previewCmd represents the preview command
var testCmd = &cobra.Command{
	Use:  "test",
	RunE: doTest,
}

func init() {
	rootCmd.AddCommand(testCmd)

	testCmd.PersistentFlags().StringVarP(&jobFile, "file", "f", ".", "File to read data from. If '-' then reads from stdin. Defaults to read from './job.yaml'")
	viper.BindPFlags(testCmd.Flags())
}

func doTest(cmd *cobra.Command, args []string) error {
	job, err := readJobInput()
	if err != nil {
		return err
	}
	if job.Id == nil {
		job.Id = "1"
	}
	streamer := pkg.NewLogStreamer(
		pkg.NewSetOutcomeVarLogProcessor(),
		pkg.NewSanitizeLogProcessor(job.Variables),
		pkg.NewPrefixLogProcessor(fmt.Sprintf("[%d] ", 0)),
		pkg.NewLoggerLogProcessor(log.Logger))
	runner, err := pkg.NewJobRunner(0)
	cobra.CheckErr(err)

	go streamer.Run()

	outcome := runner.Run(*job, streamer.Stdout, streamer.Stderr)

	streamer.Flush()

	if outcome.Outcome != opslevel.RunnerJobOutcomeEnumSuccess {
		return fmt.Errorf(outcome.Message)
	}
	return nil
}

func readJobInput() (*opslevel.RunnerJob, error) {
	if jobFile == "" {
		return nil, fmt.Errorf("please specify a job file")
	}
	if jobFile == "-" {
		viper.SetConfigType("yaml")
		viper.ReadConfig(os.Stdin)
	} else if jobFile == "." {
		viper.SetConfigFile("./job.yaml")
	} else {
		viper.SetConfigFile(jobFile)
	}
	err := viper.ReadInConfig()
	if err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
	}
	job := &opslevel.RunnerJob{}
	viper.Unmarshal(&job)
	return job, nil
}
