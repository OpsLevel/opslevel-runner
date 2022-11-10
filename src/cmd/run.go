package cmd

import (
	"context"
	"errors"
	"fmt"
	"github.com/getsentry/sentry-go"
	opslevel_common "github.com/opslevel/opslevel-common/v2022"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	clientset "k8s.io/client-go/kubernetes"
	"strings"
	"sync"
	"time"

	"github.com/opslevel/opslevel-go/v2022"
	"github.com/opslevel/opslevel-runner/pkg"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// previewCmd represents the preview command
var runCmd = &cobra.Command{
	Use: "run",
	Run: doRun,
}

func init() {
	runCmd.Flags().Int("job-concurrency", 3, "The number jobs this runner will handle in parallel.")
	runCmd.Flags().Int("poll-interval", 10, "The amount of time in seconds between API calls to find pending jobs.")
	runCmd.Flags().Int("metrics-port", 10354, "The port on which to bind the metrics endpoint to.")
	runCmd.Flags().Int("log-max-bytes", 1024000, "The max amount in bytes before job logs will be sent to OpsLevel.")
	runCmd.Flags().Int("log-max-time", 30, "The max amount of time in second before job logs will be sent to OpsLevel.")
	viper.BindPFlags(runCmd.Flags())

	rootCmd.AddCommand(runCmd)
}

func doRun(cmd *cobra.Command, args []string) {
	defer sentry.Flush(2 * time.Second)
	logVersion()

	client := getClientGQL()

	runner, err := client.RunnerRegister()
	pkg.CheckErr(err)

	if viper.GetBool("scaling-enabled") {
		config, err := pkg.GetKubernetesConfig()
		pkg.CheckErr(err)

		client := clientset.NewForConfigOrDie(config)

		log.Info().Msgf("electing leader...")
		go electLeader(client)
	}

	log.Info().Msgf("Starting runner for id '%s'", runner.Id)
	pkg.StartMetricsServer(runner.Id.(string), viper.GetInt("metrics-port"))
	stop := opslevel_common.InitSignalHandler()
	wg := startWorkers(runner.Id.(string), stop)
	<-stop // Enter Forever Loop
	log.Info().Msgf("interupt - waiting for jobs to complete ...")
	wg.Wait()
	log.Info().Msgf("Unregister runner for id '%s'...", runner.Id)
	client.RunnerUnregister(&runner.Id)
}

func electLeader(client *clientset.Clientset) {
	leaseLockName := viper.GetString("runner-deployment")
	leaseLockNamespace := viper.GetString("runner-pod-namespace")
	lockIdentity := viper.GetString("runner-pod-name")

	pkg.RunLeaderElection(client, leaseLockName, lockIdentity, leaseLockNamespace)
}

func startWorkers(runnerId string, stop <-chan struct{}) *sync.WaitGroup {
	wg := sync.WaitGroup{}
	concurrency := getConcurrency()
	wg.Add(concurrency)
	jobQueue := make(chan opslevel.RunnerJob)
	for w := 1; w <= concurrency; w++ {
		go jobWorker(&wg, w, runnerId, jobQueue)
	}
	go jobPoller(runnerId, stop, jobQueue)
	return &wg
}

func getConcurrency() int {
	concurrency := viper.GetInt("job-concurrency")
	if concurrency < 1 {
		concurrency = 1
	}
	return concurrency
}

func jobWorker(wg *sync.WaitGroup, index int, runnerId string, jobQueue <-chan opslevel.RunnerJob) {
	logMaxBytes := viper.GetInt("pod-log-max-size")
	logMaxDuration := time.Duration(viper.GetInt("pod-log-max-interval")) * time.Second
	logPrefix := func() string { return fmt.Sprintf("%s [%d] ", time.Now().UTC().Format(time.RFC3339), index) }
	logLevel := strings.ToLower(viper.GetString("log-level"))
	logger := log.With().Int("worker", index).Logger()
	client := getClientGQL()
	tracer := pkg.GetTracer()
	podConfig := newJobPodConfig()
	runner, err := pkg.NewJobRunner(runnerId, logger, podConfig)
	pkg.CheckErr(err)

	logger.Info().Msgf("Starting job processor %d ...", index)
	defer wg.Done()
	for job := range jobQueue {
		ctx := context.Background()
		jobId := job.Id.(string)
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

		go streamer.Run()
		ctx, spanStart := tracer.Start(ctx, "start-job",
			trace.WithSpanKind(trace.SpanKindConsumer),
			trace.WithAttributes(attribute.String("job", jobNumber)),
		)
		outcome := runner.Run(job, streamer.Stdout, streamer.Stderr)
		ctx, spanFinish := tracer.Start(ctx,
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

func jobPoller(runnerId string, stop <-chan struct{}, jobQueue chan<- opslevel.RunnerJob) {
	logger := log.With().Int("worker", 0).Logger()
	client := getClientGQL()
	token := opslevel.NewID("")
	poll_wait_time := time.Second * time.Duration(viper.GetInt("poll-interval"))
	logger.Info().Msg("Starting polling for jobs")
	for {
		select {
		case <-stop:
			logger.Info().Msg("Stopped Polling for jobs ...")
			close(jobQueue)
			return
		default:
			logger.Trace().Msg("Polling for jobs ...")
			continue_polling := true
			for continue_polling {
				logger.Debug().Msgf("Get pending jobs with lastUpdateToken '%v' ...", *token)
				job, nextToken, err := client.RunnerGetPendingJob(runnerId, token)
				if err != nil {
					logger.Error().Err(err).Msg("got error when getting pending job")
					continue_polling = false
				} else {
					token = nextToken
					if job.Id == nil {
						continue_polling = false
					} else {
						logger.Debug().Msgf("Enqueuing job '%s'", job.Number())
						jobQueue <- *job
					}
				}
			}
			logger.Trace().Msgf("Finished Polling for jobs sleeping for %s ...", poll_wait_time)
			time.Sleep(poll_wait_time)
		}
	}
}
