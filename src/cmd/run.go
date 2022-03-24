package cmd

import (
	"time"

	"github.com/opslevel/opslevel-runner/pkg"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// previewCmd represents the preview command
var runCmd = &cobra.Command{
	Use:  "run [COMMAND]",
	Args: cobra.MaximumNArgs(1),
	Run:  doRun,
}

func init() {
	runCmd.Flags().Int("poll-interval", 10, "The amount of time in seconds between API calls to find pending jobs.")
	viper.BindPFlags(runCmd.Flags())

	rootCmd.AddCommand(runCmd)
}

func doRun(cmd *cobra.Command, args []string) {
	client := getClientGQL()
	runner, err := pkg.NewJobRunner()
	cobra.CheckErr(err)

	poll_wait_time := time.Second * time.Duration(viper.GetInt("poll-interval"))

	log.Info().Msg("Starting runner ...")
	// Enter Forever loop
	for {
		// Poll for Jobs
		log.Info().Msg("Polling for jobs ...")
		jobs, err := client.ListPendingJobs()
		if err != nil {
			log.Error().Err(err).Msg("got error when listing pending jobs")
		} else {
			for _, job := range jobs {
				cobra.CheckErr(runner.Run(job))
			}
		}

		log.Info().Msgf("Finished Polling for jobs sleeping for %s ...", poll_wait_time)
		time.Sleep(poll_wait_time)
	}
}
