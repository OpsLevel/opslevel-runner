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
	var stdout, stderr pkg.SafeBuffer
	outcome_processor := pkg.NewSetOutcomeVarLogProcessor()
	streamer := pkg.NewLogStreamer(log.Logger, &stdout, &stderr,
		[]pkg.LogProcessor{
			pkg.NewSanitizeLogProcessor(job.Variables),
			outcome_processor,
	})
	runner, err := pkg.NewJobRunner(0, &stdout, &stderr)
	cobra.CheckErr(err)

	go streamer.Run(0)

	outcome := runner.Run(*job)

	streamer.Flush()
	for _, variable := range outcome_processor.Variables() {
		log.Info().Msgf("Outcome Variable | '%s'='%s'", variable.Key, variable.Value)
	}
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
