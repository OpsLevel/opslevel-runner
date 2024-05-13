package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	worker "github.com/contribsys/faktory_worker_go"
	"github.com/mitchellh/mapstructure"
	"github.com/opslevel/opslevel-go/v2024"
	"github.com/opslevel/opslevel-runner/pkg"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
)

type MapStructureRunnerJobVariable struct {
	Key       string `mapstructure:"key"`
	Value     string `mapstructure:"value"`
	Sensitive bool   `mapstructure:"sensitive"`
}

func startFaktory(mgr *worker.Manager) {
	log.Info().Msgf("Starting faktory worker")
	err := mgr.Run() // blocking
	if err != nil {
		log.Error().Err(err).Msgf("faktory worker returned error")
	}
	log.Info().Msgf("Stopping faktory worker")
}

func prepareJob(helper worker.Helper, job opslevel.RunnerJob) error {
	// args[0]["id"] is a factory reserved job id so we need to get the opslevel job id a different way
	jobID, ok := helper.Custom("opslevel-runner-job-id")
	if ok {
		job.Id = opslevel.ID(jobID.(string))
	}

	extraVars, ok := helper.Custom("opslevel-runner-extra-vars")
	if ok {
		var castedVars []MapStructureRunnerJobVariable
		err := mapstructure.Decode(extraVars, &castedVars)
		if err != nil {
			return err
		}
		for _, extraVar := range castedVars {
			job.Variables = append(job.Variables, opslevel.RunnerJobVariable{
				Key:       extraVar.Key,
				Value:     extraVar.Value,
				Sensitive: extraVar.Sensitive,
			})
		}
	}

	// TODO: We should also parse opslevel-runner-extra-files so they can be supplied via custom data

	batch := helper.Bid()
	if batch != "" {
		job.Variables = append(job.Variables, opslevel.RunnerJobVariable{
			Key:       "FAKTORY_BATCH_ID",
			Value:     batch,
			Sensitive: false,
		})
	}
	faktoryProvider, faktoryProviderPresent := os.LookupEnv("FAKTORY_PROVIDER")
	if faktoryProviderPresent {
		job.Variables = append(job.Variables, opslevel.RunnerJobVariable{
			Key:       "FAKTORY_PROVIDER",
			Value:     faktoryProvider,
			Sensitive: false,
		})
	}
	faktoryUrl, faktoryUrlPresent := os.LookupEnv("FAKTORY_URL")
	if faktoryUrlPresent {
		job.Variables = append(job.Variables, opslevel.RunnerJobVariable{
			Key:       "FAKTORY_URL",
			Value:     faktoryUrl,
			Sensitive: false,
		})
	}
	job.Variables = append(job.Variables, opslevel.RunnerJobVariable{
		Key:       "RUNNER_JOB_ID",
		Value:     string(job.Id),
		Sensitive: false,
	})
	return nil
}

func parseJob(args []any) (opslevel.RunnerJob, error) {
	data, err := json.Marshal(args[0])
	if err != nil {
		log.Error().Err(err).Msgf("failed to marshal job data: %v", args[0])
		return opslevel.RunnerJob{}, err
	}
	var job opslevel.RunnerJob
	if err = json.Unmarshal(data, &job); err != nil {
		log.Error().Err(err).Msgf("failed to unmarshal job data: %v", string(data))
		return opslevel.RunnerJob{}, err
	}
	return job, nil
}

func runJob(helper worker.Helper, job opslevel.RunnerJob) pkg.JobOutcome {
	logger := log.With().Str("runner", "faktory").Logger()
	logMaxBytes := viper.GetInt("job-pod-log-max-size")
	logMaxDuration := time.Duration(viper.GetInt("job-pod-log-max-interval")) * time.Second
	logPrefix := func() string { return fmt.Sprintf("%s [%d] ", time.Now().UTC().Format(time.RFC3339), 0) }
	streamer := pkg.NewLogStreamer(
		logger,
		pkg.NewFaktorySetOutcomeProcessor(helper, logger, job.Id),
		pkg.NewSanitizeLogProcessor(job.Variables),
		pkg.NewPrefixLogProcessor(logPrefix),
		pkg.NewFaktoryAppendJobLogProcessor(helper, logger, job.Id, logMaxBytes, logMaxDuration),
	)
	go streamer.Run()

	pkg.MetricJobsProcessing.Inc()
	runner := pkg.NewJobRunner("faktory")
	outcome := runner.Run(job, streamer.Stdout, streamer.Stderr)
	streamer.Flush(outcome)
	return outcome
}

func emitJobStartedMetrics() time.Time {
	pkg.MetricJobsStarted.Inc()
	return time.Now()
}

func emitJobCompleteMetrics(jobStart time.Time, job opslevel.RunnerJob, outcome pkg.JobOutcome) {
	jobDuration := time.Since(jobStart)
	log.Info().Msgf("Finished job '%s' took '%s' and had outcome '%s'", job.Id, jobDuration, outcome.Outcome)
	pkg.MetricJobsDuration.Observe(jobDuration.Seconds())
	pkg.MetricJobsFinished.WithLabelValues(string(outcome.Outcome)).Inc()
	pkg.MetricJobsProcessing.Dec()
}

func legacyJobHandler(ctx context.Context, args ...interface{}) error {
	jobStart := emitJobStartedMetrics()

	job, err := parseJob(args)
	if err != nil {
		return err
	}

	helper := worker.HelperFor(ctx)

	if err := prepareJob(helper, job); err != nil {
		return err
	}

	outcome := runJob(helper, job)

	emitJobCompleteMetrics(jobStart, job, outcome)
	return nil
}

func runFaktory() {
	mgr := worker.NewManager()
	mgr.Concurrency = getConcurrency()
	mgr.ProcessStrictPriorityQueues(viper.GetStringSlice("queues")...)
	mgr.Register("legacy", legacyJobHandler)
	startFaktory(mgr)
}
