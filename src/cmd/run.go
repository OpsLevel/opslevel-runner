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
	client := getClientGQL()
	var stdout, stderr pkg.SafeBuffer
	// TODO: If Log Level == Trace?
	// streamer := pkg.NewLogStreamer(log.Logger, &stdout, &stderr)
	// go streamer.Run(index)
	runner, err := pkg.NewJobRunner(index, &stdout, &stderr)
	cobra.CheckErr(err)
	log.Info().Msgf("[%d] Starting job worker ...", index)
	for {
		job := <-jobQueue
		log.Info().Msgf("[%d] Starting job '%s'", index, job.Id)
		outcome := runner.Run(job)
		log.Info().Msgf("[%d] Finished job '%s' with outcome '%s'", index, job.Id, outcome.Outcome)
		if outcome.Outcome != opslevel.RunnerJobOutcomeEnumSuccess {
			log.Warn().Msgf("[%d] Job '%s' failed REASON: %s", index, job.Id, outcome.Message)
		}
		_, err := client.ReportJobOutcome(opslevel.RunnerReportJobOutcomeInput{
			RunnerId:    runnerId,
			RunnerJobId: job.Id,
			Outcome:     outcome.Outcome,
		})
		if err != nil {
			log.Error().Err(err).Msgf("[%d] got error when reporting job outcome", index)
		}
	}
}

func jobPoller(runnerId string, jobQueue chan<- opslevel.RunnerJob) {
	client := getClientGQL()
	token := opslevel.NewID("")
	poll_wait_time := time.Second * time.Duration(viper.GetInt("poll-interval"))
	log.Info().Msg("[0] Starting polling for jobs")
	for {
		log.Trace().Msg("[0] Polling for jobs ...")
		continue_polling := true
		for continue_polling {
			job, nextToken, err := client.GetPendingJob(runnerId, token)
			token = nextToken
			if err != nil {
				log.Error().Err(err).Msg("[0] got error when getting pending job")
				continue_polling = false
			} else {
				if job.Id == nil {
					continue_polling = false
				} else {
					log.Debug().Msgf("[0] Enqueuing job '%s'", job.Id)
					jobQueue <- *job
				}
			}
		}
		log.Trace().Msgf("[0] Finished Polling for jobs sleeping for %s ...", poll_wait_time)
		time.Sleep(poll_wait_time)
	}
}
