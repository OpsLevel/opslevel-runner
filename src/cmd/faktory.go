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

type MapStructureRunnerJobFile struct {
	Name     string `mapstructure:"name"`
	Contents string `mapstructure:"contents"`
}

func startFaktory(mgr *worker.Manager) {
	log.Info().Msgf("Starting faktory worker")
	err := mgr.Run() // blocking
	if err != nil {
		log.Error().Err(err).Msgf("faktory worker returned error")
	}
	log.Info().Msgf("Stopping faktory worker")
}

func parseJob(args []any, job *opslevel.RunnerJob) error {
	data, err := json.Marshal(args[0])
	if err != nil {
		log.Error().Err(err).Msgf("failed to marshal job data: %v", args[0])
		return err
	}
	if err = json.Unmarshal(data, &job); err != nil {
		log.Error().Err(err).Msgf("failed to unmarshal job data: %v", string(data))
		return err
	}
	return nil
}

func extractJobId(helper worker.Helper, job *opslevel.RunnerJob) {
	// args[0]["id"] is a factory reserved job id so we need to get the opslevel job id a different way
	jobID, ok := helper.Custom("opslevel-runner-job-id")
	if ok {
		switch casted := jobID.(type) {
		case string:
			job.Id = opslevel.ID(casted)
		case float64:
			job.Id = opslevel.ID(fmt.Sprintf("%d", int(casted)))
		default:
			job.Id = "0"
			log.Warn().Msgf("opslevel-runner-job-id is unexpected type '%T' value was '%v'", jobID, jobID)
		}
	}
}

func extractCustomImage(helper worker.Helper, job *opslevel.RunnerJob) error {
	overrideImage, ok := helper.Custom("opslevel-runner-image")
	if ok {
		var image string
		err := mapstructure.Decode(overrideImage, &image)
		if err != nil {
			return err
		}
		job.Image = image
	}
	return nil
}

func extractCustomExtraCommands(helper worker.Helper, job *opslevel.RunnerJob) error {
	extraCommands, ok := helper.Custom("opslevel-runner-commands")
	if ok {
		var commands []string
		err := mapstructure.Decode(extraCommands, &commands)
		if err != nil {
			return err
		}
		job.Commands = append(job.Commands, commands...)
	}
	return nil
}

func extractCustomExtraVars(helper worker.Helper, job *opslevel.RunnerJob) error {
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

func extractCustomExtraFiles(helper worker.Helper, job *opslevel.RunnerJob) error {
	extraFiles, ok := helper.Custom("opslevel-runner-files")
	if ok {
		var files []MapStructureRunnerJobFile
		err := mapstructure.Decode(extraFiles, &files)
		if err != nil {
			return err
		}
		for _, file := range files {
			job.Files = append(job.Files, opslevel.RunnerJobFile{
				Name:     file.Name,
				Contents: file.Contents,
			})
		}
	}
	return nil
}

func runJob(ctx context.Context, helper worker.Helper, job opslevel.RunnerJob) pkg.JobOutcome {
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
	go streamer.Run(ctx)

	pkg.MetricJobsProcessing.Inc()
	logger.Info().Msgf("Starting job '%s'", job.Id)
	runner := pkg.NewJobRunner("faktory", cfgFile)
	outcome := runner.Run(ctx, job, streamer.Stdout, streamer.Stderr)
	streamer.Flush(outcome)
	return outcome
}

func emitJobStartedMetrics() time.Time {
	pkg.MetricJobsStarted.Inc()
	return time.Now()
}

func emitJobCompleteMetrics(jobStart time.Time, job opslevel.RunnerJob, outcome pkg.JobOutcome) {
	jobDuration := time.Since(jobStart)
	log.Info().Str("outcome", outcome.Message).Msgf("Finished job '%s' took '%s' and had outcome '%s'", job.Id, jobDuration, outcome.Outcome)
	pkg.MetricJobsDuration.Observe(jobDuration.Seconds())
	pkg.MetricJobsFinished.WithLabelValues(string(outcome.Outcome)).Inc()
	pkg.MetricJobsProcessing.Dec()
}

func legacyJobHandler(ctx context.Context, args ...interface{}) error {
	jobStart := emitJobStartedMetrics()

	helper := worker.HelperFor(ctx)

	var job opslevel.RunnerJob

	if err := parseJob(args, &job); err != nil {
		return err
	}

	extractJobId(helper, &job)

	if err := extractCustomImage(helper, &job); err != nil {
		return err
	}

	if err := extractCustomExtraCommands(helper, &job); err != nil {
		return err
	}

	if err := extractCustomExtraVars(helper, &job); err != nil {
		return err
	}

	if err := extractCustomExtraFiles(helper, &job); err != nil {
		return err
	}

	outcome := runJob(ctx, helper, job)

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
