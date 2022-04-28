package cmd

import (
	"time"

	"github.com/opslevel/opslevel-go"
	"github.com/opslevel/opslevel-runner/pkg"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// previewCmd represents the preview command
var runCmd = &cobra.Command{
	Use:  "run [RUNNER ID]",
	Args: cobra.ExactArgs(1),
	Run:  doRun,
}

func init() {
	runCmd.Flags().Int("job-concurrency", 3, "The number jobs this runner will handle in parallel.")
	runCmd.Flags().Int("poll-interval", 10, "The amount of time in seconds between API calls to find pending jobs.")
	viper.BindPFlags(runCmd.Flags())

	rootCmd.AddCommand(runCmd)
}

func doRun(cmd *cobra.Command, args []string) {
	logVersion()

	runnerId := args[0]
	jobQueue := make(chan opslevel.RunnerJob)
	// Validate we can create a graphql client
	getClientGQL()

	concurrency := viper.GetInt("job-concurrency")
	if concurrency < 1 {
		concurrency = 1
	}
	for w := 1; w <= concurrency; w++ {
		go jobWorker(w, runnerId, jobQueue)
	}
	// Enter Forever loop
	jobPoller(runnerId, jobQueue)
}

func jobWorker(index int, runnerId string, jobQueue <-chan opslevel.RunnerJob) {
	logger := log.With().Int("worker", index).Logger()
	client := getClientGQL()
	runner, err := pkg.NewJobRunner(logger)
	cobra.CheckErr(err)
	outcomeProcessor := pkg.NewSetOutcomeVarLogProcessor()
	// TODO: If Log Level == Trace - add logging processor similar to `test` command?
	streamer := pkg.NewLogStreamer(outcomeProcessor)
	go streamer.Run()
	logger.Info().Msg("Starting job processor ...")
	for {
		job := <-jobQueue
		logger.Info().Msgf("Starting job '%s'", job.Id)
		outcome := runner.Run(job, streamer.Stdout, streamer.Stderr)
		streamer.Flush()
		logger.Info().Msgf("Finished job '%s' with outcome '%s'", job.Id, outcome.Outcome)
		if outcome.Outcome != opslevel.RunnerJobOutcomeEnumSuccess {
			logger.Warn().Msgf("Job '%s' failed REASON: %s", job.Id, outcome.Message)
		}
		err = client.ReportJobOutcome(opslevel.RunnerReportJobOutcomeInput{
			RunnerId:         runnerId,
			RunnerJobId:      job.Id,
			Outcome:          outcome.Outcome,
			OutcomeVariables: outcomeProcessor.Variables(),
		})
		outcomeProcessor.Clear()
		if err != nil {
			logger.Error().Err(err).Msg("got error when reporting job outcome")
		}
	}
}

func jobPoller(runnerId string, jobQueue chan<- opslevel.RunnerJob) {
	logger := log.With().Int("worker", 0).Logger()
	client := getClientGQL()
	token := opslevel.NewID("")
	poll_wait_time := time.Second * time.Duration(viper.GetInt("poll-interval"))
	logger.Info().Msg("Starting polling for jobs")
	for {
		logger.Trace().Msg("Polling for jobs ...")
		continue_polling := true
		for continue_polling {
			logger.Debug().Msgf("Get pending jobs with lastUpdateToken '%v' ...", *token)
			job, nextToken, err := client.GetPendingJob(runnerId, token)
			if err != nil {
				logger.Error().Err(err).Msg("got error when getting pending job")
				continue_polling = false
			} else {
				token = nextToken
				if job.Id == nil {
					continue_polling = false
				} else {
					logger.Debug().Msgf("Enqueuing job '%s'", job.Id)
					jobQueue <- *job
				}
			}
		}
		logger.Trace().Msgf("Finished Polling for jobs sleeping for %s ...", poll_wait_time)
		time.Sleep(poll_wait_time)
	}
}
