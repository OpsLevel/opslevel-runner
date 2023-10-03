package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	faktory "github.com/contribsys/faktory/client"
	worker "github.com/contribsys/faktory_worker_go"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	opslevel_common "github.com/opslevel/opslevel-common/v2022"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	clientset "k8s.io/client-go/kubernetes"

	"github.com/opslevel/opslevel-go/v2023"
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
	runCmd.Flags().String("mode", "api", "Whether to use the 'api` or 'faktory` for jobs.")
	runCmd.Flags().StringArray("queues", []string{"default"}, "The faktory queues to target.")
	runCmd.Flags().Int("job-concurrency", 3, "The number jobs this runner will handle in parallel.")
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
		runner, err := client.RunnerRegister()
		pkg.CheckErr(err)

		pkg.StartMetricsServer(string(runner.Id), viper.GetInt("metrics-port"))

		if viper.GetBool("scaling-enabled") {
			config, err := pkg.GetKubernetesConfig()
			pkg.CheckErr(err)

			k8sClient := clientset.NewForConfigOrDie(config)

			log.Info().Msgf("electing leader...")
			go electLeader(k8sClient, runner.Id)
		}

		stop := opslevel_common.InitSignalHandler()
		wg := startWorkers(runner.Id, stop)
		<-stop // Enter Forever Loop
		log.Info().Msgf("interupt - waiting for jobs to complete ...")
		wg.Wait()
		log.Info().Msgf("Unregister runner for id '%s'...", runner.Id)
		client.RunnerUnregister(runner.Id)
	}
}

func electLeader(k8sClient *clientset.Clientset, runnerId opslevel.ID) {
	leaseLockName := viper.GetString("runner-deployment")
	leaseLockNamespace := viper.GetString("runner-pod-namespace")
	lockIdentity := viper.GetString("runner-pod-name")

	pkg.RunLeaderElection(k8sClient, runnerId, leaseLockName, lockIdentity, leaseLockNamespace)
}

func startWorkers(runnerId opslevel.ID, stop <-chan struct{}) *sync.WaitGroup {
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

func jobWorker(wg *sync.WaitGroup, index int, runnerId opslevel.ID, jobQueue <-chan opslevel.RunnerJob) {
	logMaxBytes := viper.GetInt("job-pod-log-max-size")
	logMaxDuration := time.Duration(viper.GetInt("job-pod-log-max-interval")) * time.Second
	logPrefix := func() string { return fmt.Sprintf("%s [%d] ", time.Now().UTC().Format(time.RFC3339), index) }
	logLevel := strings.ToLower(viper.GetString("log-level"))
	logger := log.With().Int("worker", index).Logger()
	client := pkg.NewGraphClient()
	k8sConfig, err := pkg.GetKubernetesConfig()
	cobra.CheckErr(err)
	k8sClient, err := pkg.GetKubernetesClientset()
	cobra.CheckErr(err)
	tracer := pkg.GetTracer()
	podConfig := newJobPodConfig()
	runner := pkg.NewJobRunner(string(runnerId), logger, k8sConfig, k8sClient, podConfig)

	logger.Info().Msgf("Starting job processor %d ...", index)
	defer wg.Done()
	for job := range jobQueue {
		ctx := context.Background()
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

		go streamer.Run()
		ctx, spanStart := tracer.Start(ctx, "start-job",
			trace.WithSpanKind(trace.SpanKindConsumer),
			trace.WithAttributes(attribute.String("job", jobNumber)),
		)
		outcome := runner.Run(job, streamer.Stdout, streamer.Stderr)
		_, spanFinish := tracer.Start(ctx,
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

func jobPoller(runnerId opslevel.ID, stop <-chan struct{}, jobQueue chan<- opslevel.RunnerJob) {
	logger := log.With().Int("worker", 0).Logger()
	client := pkg.NewGraphClient()
	token := opslevel.ID("")
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
				logger.Debug().Msgf("Get pending jobs with lastUpdateToken '%v' ...", token)
				job, nextToken, err := client.RunnerGetPendingJob(runnerId, token)
				if err != nil {
					logger.Error().Err(err).Msg("got error when getting pending job")
					continue_polling = false
				} else {
					token = nextToken
					if job.Id == "" {
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

func runFaktory() {
	client, err := faktory.Open()
	cobra.CheckErr(err)
	logMaxBytes := viper.GetInt("job-pod-log-max-size")
	logMaxDuration := time.Duration(viper.GetInt("job-pod-log-max-interval")) * time.Second
	logPrefix := func() string { return fmt.Sprintf("%s [%d] ", time.Now().UTC().Format(time.RFC3339), 0) }
	logger := log.With().Int("faktory", 0).Logger()
	podConfig := newJobPodConfig()

	mgr := worker.NewManager()
	mgr.Register("legacy", func(ctx context.Context, args ...interface{}) error {
		pkg.MetricJobsStarted.Inc()

		config, err := pkg.GetKubernetesConfig()
		if err != nil {
			return err
		}
		clientset, err := pkg.GetKubernetesClientset()
		if err != nil {
			return err
		}

		var job opslevel.RunnerJob
		data, err := json.Marshal(args[0])
		if err != nil {
			return err
		}
		err = json.Unmarshal(data, &job)
		if err != nil {
			return err
		}

		helper := worker.HelperFor(ctx)

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

		// TODO: add log processors that ships log chunks as jobs
		streamer := pkg.NewLogStreamer(
			logger,
			pkg.NewFaktorySetOutcomeProcessor(client, helper, logger, job.Id),
			pkg.NewSanitizeLogProcessor(job.Variables),
			pkg.NewPrefixLogProcessor(logPrefix),
			pkg.NewLoggerLogProcessor(logger),
			pkg.NewFaktoryAppendJobLogProcessor(client, helper, logger, job.Id, logMaxBytes, logMaxDuration),
		)
		pkg.MetricJobsProcessing.Inc()
		jobStart := time.Now()
		go streamer.Run()

		runner := pkg.NewJobRunner("faktory", logger, config, clientset, podConfig)
		outcome := runner.Run(job, streamer.Stdout, streamer.Stderr)
		streamer.Flush(outcome)

		jobDuration := time.Since(jobStart)
		pkg.MetricJobsDuration.Observe(jobDuration.Seconds())
		logger.Info().Msgf("Finished job took '%s' and had outcome '%s'", jobDuration, outcome.Outcome)
		pkg.MetricJobsFinished.WithLabelValues(string(outcome.Outcome)).Inc()
		pkg.MetricJobsProcessing.Dec()
		return nil
	})
	mgr.Concurrency = getConcurrency()
	mgr.ProcessStrictPriorityQueues(viper.GetStringSlice("queues")...)
	logger.Info().Msgf("Starting faktory worker")
	mgr.Run() // blocking
	logger.Info().Msgf("Stopping faktory worker")
}
