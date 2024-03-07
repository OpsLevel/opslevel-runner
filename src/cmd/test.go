package cmd

import (
	"fmt"
	"time"

	"github.com/opslevel/opslevel-go/v2024"
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
	job, err := readRunnerInput()
	if err != nil {
		return err
	}
	if job.Id == "" {
		job.Id = "1"
	}
	streamer := pkg.NewLogStreamer(
		log.Logger,
		pkg.NewSetOutcomeVarLogProcessor(nil, log.Logger, "1", "1", "1"),
		pkg.NewSanitizeLogProcessor(job.Variables),
		pkg.NewLoggerLogProcessor(log.Logger),
		pkg.NewOpsLevelAppendLogProcessor(nil, log.Logger, "1", "1", "1", 1024000, 30*time.Second),
	)
	jobPodConfig := newJobPodConfig()
	k8sConfig, err := pkg.GetKubernetesConfig()
	cobra.CheckErr(err)
	k8sClient, err := pkg.GetKubernetesClientset()
	cobra.CheckErr(err)
	runner := pkg.NewJobRunner("1", log.Logger, k8sConfig, k8sClient, jobPodConfig)

	go streamer.Run()
	outcome := runner.Run(*job, streamer.Stdout, streamer.Stderr)
	streamer.Flush(outcome)

	if outcome.Outcome != opslevel.RunnerJobOutcomeEnumSuccess {
		return fmt.Errorf(outcome.Message)
	}
	return nil
}
