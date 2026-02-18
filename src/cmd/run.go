package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/opslevel/opslevel-runner/signal"

	"github.com/getsentry/sentry-go"
	"github.com/opslevel/opslevel-go/v2026"
	"github.com/opslevel/opslevel-runner/pkg"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// previewCmd represents the preview command
var runCmd = &cobra.Command{
	Use: "run",
	Run: doRun,
}

func init() {
	runCmd.Flags().String("mode", "api", "Whether to use the 'api' or 'faktory' for jobs.")
	runCmd.Flags().StringArray("queues", []string{"default"}, "The faktory queues to target.")
	runCmd.Flags().Int("job-concurrency", 3, "The number jobs this runner will handle in parallel.")
	runCmd.Flags().Int("job-concurrency-factor", 100, "The scale factor of jobs concurrency used to calculate the runner scale based on queue depth.")
	runCmd.Flags().Int("poll-interval", 10, "The amount of time in seconds between API calls to find pending jobs.")
	runCmd.Flags().Int("metrics-port", 10354, "The port on which to bind the metrics endpoint to.")
	runCmd.Flags().Int("log-max-bytes", 512000, "The max amount in bytes before job logs will be sent to OpsLevel.")
	runCmd.Flags().Int("log-max-time", 30, "The max amount of time in second before job logs will be sent to OpsLevel.")
	viper.BindPFlags(runCmd.Flags())

	rootCmd.AddCommand(runCmd)
}

func doRun(cmd *cobra.Command, args []string) {
	defer sentry.Flush(2 * time.Second)
	logVersion()

	log.Info().Msg("Starting runner ...")

	switch viper.GetString("mode") {
	case "faktory":
		pkg.StartMetricsServer("faktory", viper.GetInt("metrics-port"))
		runFaktory()
	case "api":
		client := pkg.NewGraphClient()
		var registerArgs []string
		if queue := viper.GetString("queue"); queue != "" {
			log.Info().Str("queue", queue).Msg("Registering with queue")
			registerArgs = append(registerArgs, queue)
		}
		runner, err := client.RunnerRegister(registerArgs...)
		pkg.CheckErr(err)

		pkg.StartMetricsServer(string(runner.Id), viper.GetInt("metrics-port"))

		ctx := signal.Init(context.Background())

		if viper.GetBool("scaling-enabled") {
			leaseLockName := viper.GetString("runner-deployment")
			leaseLockNamespace := viper.GetString("runner-pod-namespace")
			lockIdentity := viper.GetString("runner-pod-name")
			cobra.CheckErr(pkg.RunLeaderElection(ctx, runner.Id, leaseLockName, lockIdentity, leaseLockNamespace))
		}

		wg := startWorkers(ctx, runner.Id)
		time.Sleep(1 * time.Second)
		wg.Wait()
		log.Info().Msgf("Unregister runner for id '%s'...", runner.Id)
		err = client.RunnerUnregister(runner.Id)
		if err != nil {
			log.Error().Err(err).Msgf("received error while unregistering runner")
		}
	}
}

func startWorkers(ctx context.Context, runnerId opslevel.ID) *sync.WaitGroup {
	wg := sync.WaitGroup{}
	concurrency := getConcurrency()
	wg.Add(concurrency)
	jobQueue := make(chan opslevel.RunnerJob)
	for w := 1; w <= concurrency; w++ {
		go jobWorker(ctx, &wg, w, runnerId, jobQueue)
	}
	go jobPoller(ctx, runnerId, jobQueue)
	return &wg
}

func getConcurrency() int {
	concurrency := viper.GetInt("job-concurrency")
	if concurrency < 1 {
		concurrency = 1
	}
	return concurrency
}

func jobWorker(ctx context.Context, wg *sync.WaitGroup, index int, runnerId opslevel.ID, jobQueue <-chan opslevel.RunnerJob) {
	logMaxBytes := viper.GetInt("job-pod-log-max-size")
	logMaxDuration := time.Duration(viper.GetInt("job-pod-log-max-interval")) * time.Second
	logPrefix := func() string { return fmt.Sprintf("%s [%d] ", time.Now().UTC().Format(time.RFC3339), index) }
	logLevel := strings.ToLower(viper.GetString("log-level"))
	logger := log.With().Int("worker", index).Logger()
	client := pkg.NewGraphClient()
	tracer := pkg.GetTracer()
	runner := pkg.NewJobRunner(string(runnerId), cfgFile)

	logger.Info().Msgf("Starting job processor %d ...", index)
	defer wg.Done()
	for job := range jobQueue {
		jobId := job.Id
		jobNumber := job.Number()

		streamer := pkg.NewLogStreamer(
			logger,
			pkg.NewSetOutcomeVarLogProcessor(client, logger, runnerId, jobId, jobNumber),
			pkg.NewSanitizeLogProcessor(job.Variables),
			pkg.NewPrefixLogProcessor(logPrefix),
			pkg.NewOpsLevelAppendLogProcessor(client, logger, runnerId, jobId, jobNumber, logMaxBytes, logMaxDuration),
		)
		if logLevel == "trace" {
			streamer.AddProcessor(pkg.NewLoggerLogProcessor(logger))
		}

		jobStart := time.Now()
		pkg.MetricJobsStarted.Inc()
		pkg.MetricJobsProcessing.Inc()
		logger.Info().Msgf("Starting job '%s'", jobNumber)

		go streamer.Run(ctx)
		traceCtx, spanStart := tracer.Start(ctx, "start-job",
			trace.WithSpanKind(trace.SpanKindConsumer),
			trace.WithAttributes(attribute.String("job", jobNumber)),
		)
		outcome := runner.Run(ctx, job, streamer.Stdout, streamer.Stderr)
		_, spanFinish := tracer.Start(traceCtx,
			"finish-job",
			trace.WithSpanKind(trace.SpanKindConsumer),
			trace.WithAttributes(attribute.String("job", jobNumber)))
		streamer.Flush(outcome)
		spanFinish.SetAttributes(
			attribute.String("outcome", string(outcome.Outcome)),
		)
		if outcome.Outcome != opslevel.RunnerJobOutcomeEnumSuccess {
			err := errors.New(outcome.Message)
			spanFinish.RecordError(err)
			spanFinish.SetStatus(codes.Error, err.Error())

			logs := streamer.GetLogBuffer()

			localHub := sentry.CurrentHub().Clone()
			localHub.ConfigureScope(func(scope *sentry.Scope) {
				scope.SetTag("outcome", string(outcome.Outcome))
				scope.SetTag("job", jobNumber)
				scope.SetContext("logs", map[string]interface{}{
					"runner_logs": logs,
				})
			})
			localHub.CaptureMessage(fmt.Sprintf("[job: %s] Ended with Outcome: %s", jobNumber, string(outcome.Outcome)))
		}
		spanFinish.End()
		spanStart.End()

		jobDuration := time.Since(jobStart)
		pkg.MetricJobsDuration.Observe(jobDuration.Seconds())
		logger.Info().Msgf("Finished Job '%s' took '%s' and had outcome '%s'", jobNumber, jobDuration, outcome.Outcome)
		pkg.MetricJobsFinished.WithLabelValues(string(outcome.Outcome)).Inc()
		pkg.MetricJobsProcessing.Dec()
	}
	logger.Info().Msgf("Shutting down job processor %d ...", index)
}

func jobPoller(ctx context.Context, runnerId opslevel.ID, jobQueue chan<- opslevel.RunnerJob) {
	logger := log.With().Int("worker", 0).Logger()
	client := pkg.NewGraphClient()
	token := opslevel.ID("")
	pollWaitTime := time.Second * time.Duration(viper.GetInt("poll-interval"))
	logger.Info().Msg("Starting polling for jobs")
	for {
		select {
		case <-ctx.Done():
			logger.Info().Msg("Stopped Polling for jobs ...")
			close(jobQueue)
			return
		default:
			logger.Trace().Msg("Polling for jobs ...")
			continuePolling := true
			for continuePolling {
				logger.Debug().Msgf("Get pending jobs with lastUpdateToken '%v' ...", token)
				job, nextToken, err := client.RunnerGetPendingJob(runnerId, token)
				if err != nil {
					logger.Error().Err(err).Msg("got error when getting pending job")
					continuePolling = false
				} else {
					token = nextToken
					if job.Id == "" {
						continuePolling = false
					} else {
						logger.Debug().Msgf("Enqueuing job '%s'", job.Number())
						jobQueue <- *job
					}
				}
			}
			logger.Trace().Msgf("Finished Polling for jobs sleeping for %s ...", pollWaitTime)
			time.Sleep(pollWaitTime)
		}
	}
}
