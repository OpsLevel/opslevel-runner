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
	Use:  "run [ID]",
	Args: cobra.MaximumNArgs(1),
	Run:  doRun,
}

func init() {
	runCmd.Flags().Int("job-concurrency", 3, "The number jobs this runner will handle in parallel.")
	runCmd.Flags().Int("poll-interval", 10, "The amount of time in seconds between API calls to find pending jobs.")
	viper.BindPFlags(runCmd.Flags())

	rootCmd.AddCommand(runCmd)
}

func doRun(cmd *cobra.Command, args []string) {
	runnerId := args[0]
	jobQueue := make(chan opslevel.RunnerJob)

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
	runner, err := pkg.NewJobRunner()
	cobra.CheckErr(err)
	log.Info().Msgf("[%d] Starting worker ...", index)
	for {
		job := <-jobQueue
		log.Info().Msgf("[%d] Found Pending Job\n%+v", index, job)
		// TODO: need to handle specific exit codes so we can set the job outcome
		runner.Run(job)
		jobOutcome, err := client.ReportJobOutcome(opslevel.RunnerReportJobOutcomeInput{
			RunnerId:    runnerId,
			RunnerJobId: job.Id,
			Outcome:     opslevel.RunnerJobOutcomeEnumSuccess,
		})
		if err != nil {
			log.Error().Err(err).Msgf("[%d] got error when reporting job outcome", index)
		}
		log.Info().Msgf("[%d] Report Job Outcome\n%+v", index, jobOutcome)
	}
}

func jobPoller(runnerId string, jobQueue chan<- opslevel.RunnerJob) {
	client := getClientGQL()
	poll_wait_time := time.Second * time.Duration(viper.GetInt("poll-interval"))
	for {
		log.Info().Msg("Polling for jobs ...")
		continue_polling := true
		for continue_polling {
			job, err := client.GetPendingJob(runnerId)
			if err != nil {
				log.Error().Err(err).Msg("got error when getting pending job")
				continue_polling = false
			} else {
				if job.Id == nil {
					continue_polling = false
				} else {
					jobQueue <- *job
				}
			}
		}
		log.Info().Msgf("Finished Polling for jobs sleeping for %s ...", poll_wait_time)
		time.Sleep(poll_wait_time)
	}
}
