package cmd

import (
	"github.com/getsentry/sentry-go"
	"os"
	"strings"

	"github.com/opslevel/opslevel-runner/pkg"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile string
)

var rootCmd = &cobra.Command{
	Use:   "opslevel-runner",
	Short: "Opslevel Runner",
	Long:  `Opslevel Runner`,
}

func Execute(v string, c string) {
	version = v
	commit = c
	pkg.SetClientVersion(version)
	cobra.CheckErr(rootCmd.Execute())
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "./opslevel.yaml", "configuration options for the runner")
	rootCmd.PersistentFlags().String("api-url", "https://api.opslevel.com", "The OpsLevel API Url. Overrides environment variable 'OPSLEVEL_API_URL'")
	rootCmd.PersistentFlags().String("api-token", "", "The OpsLevel API Token. Overrides environment variable 'OPSLEVEL_API_TOKEN'")
	rootCmd.PersistentFlags().String("log-format", "TEXT", "overrides environment variable 'OPSLEVEL_LOG_FORMAT' (options [\"JSON\", \"TEXT\"])")
	rootCmd.PersistentFlags().String("log-level", "INFO", "overrides environment variable 'OPSLEVEL_LOG_LEVEL' (options [\"ERROR\", \"WARN\", \"INFO\", \"DEBUG\"])")
	rootCmd.PersistentFlags().Bool("scaling-enabled", false, "Enables built-in pod scaling for kubernetes environment, defaults to false for local development")

	rootCmd.PersistentFlags().Int("job-pod-max-wait", 60, "The max amount of time to wait for the job pod to become healthy.")
	rootCmd.PersistentFlags().Int("job-pod-exec-max-wait", 60, "The max amount of time to wait for a job pod exec command with no output before timing out.")
	rootCmd.PersistentFlags().Int("job-pod-max-lifetime", 3600, "The max amount of time a job pod can run for.")
	rootCmd.PersistentFlags().String("job-pod-namespace", "default", "The kubernetes namespace to create job pods in.")
	rootCmd.PersistentFlags().Int64("job-pod-requests-cpu", 1000, "The job pod resource requests cpu millicores.")
	rootCmd.PersistentFlags().Int64("job-pod-requests-memory", 1024, "The job pod resource requests in MB.")
	rootCmd.PersistentFlags().Int64("job-pod-limits-cpu", 1000, "The job pod resource limits cpu millicores.")
	rootCmd.PersistentFlags().Int64("job-pod-limits-memory", 1024, "The job pod resource limits in MB.")
	rootCmd.PersistentFlags().String("job-pod-shell", "/bin/sh", "The job pod shell to use for commands run inside the pod.")
	rootCmd.PersistentFlags().Int("job-pod-log-max-interval", 30, "The max amount of time between when pod logs are shipped to OpsLevel. Works in tandem with 'job-pod-log-max-size'")
	rootCmd.PersistentFlags().Int("job-pod-log-max-size", 1000000, "The max amount in bytes to buffer before pod logs are shipped to OpsLevel. Works in tandem with 'job-pod-log-max-interval'")

	rootCmd.PersistentFlags().String("runner-pod-name", "", "overrides environment variable 'RUNNER_POD_NAME'")
	rootCmd.PersistentFlags().String("runner-pod-namespace", "default", "The kubernetes namespace the runner pod is deployed in. Overrides environment variable 'RUNNER_POD_NAMESPACE'")
	rootCmd.PersistentFlags().String("runner-deployment", "runner", "The runner's kubernetes deployment name")
	rootCmd.PersistentFlags().Int("runner-min-replicas", 1, "The min replicas the runner leader should not scale below")
	rootCmd.PersistentFlags().Int("runner-max-replicas", 10, "The max replicas the runner leader should not scale above")

	viper.BindPFlags(rootCmd.PersistentFlags())
	viper.BindEnv("log-format", "OPSLEVEL_LOG_FORMAT")
	viper.BindEnv("log-level", "OPSLEVEL_LOG_LEVEL")
	viper.BindEnv("api-url", "OPSLEVEL_API_URL", "OPSLEVEL_APP_URL")
	viper.BindEnv("api-token", "OPSLEVEL_API_TOKEN")
	viper.BindEnv("scaling-enabled", "SCALING_ENABLED")

	viper.BindEnv("job-pod-max-wait", "OPSLEVEL_JOB_POD_MAX_WAIT")
	viper.BindEnv("job-pod-max-lifetime", "OPSLEVEL_JOB_POD_MAX_LIFETIME")
	viper.BindEnv("job-pod-namespace", "OPSLEVEL_JOB_POD_NAMESPACE")
	viper.BindEnv("job-pod-shell", "OPSLEVEL_JOB_POD_SHELL")
	viper.BindEnv("job-pod-log-max-interval", "OPSLEVEL_JOB_POD_LOG_MAX_INTERVAL")
	viper.BindEnv("job-pod-log-max-size", "OPSLEVEL_JOB_POD_LOG_MAX_SIZE")

	viper.BindEnv("runner-pod-name", "RUNNER_POD_NAME")
	viper.BindEnv("runner-pod-namespace", "RUNNER_POD_NAMESPACE")
	viper.BindEnv("runner-deployment", "RUNNER_DEPLOYMENT")
	viper.BindEnv("runner-min-replicas", "RUNNER_MIN_REPLICAS")
	viper.BindEnv("runner-max-replicas", "RUNNER_MAX_REPLICAS")

	cobra.OnInitialize(initConfig)
}

func newJobPodConfig() pkg.JobPodConfig {
	return pkg.JobPodConfig{
		Namespace:   viper.GetString("job-pod-namespace"),
		Lifetime:    viper.GetInt("job-pod-max-lifetime"),
		CpuRequests: viper.GetInt64("job-pod-requests-cpu"),
		MemRequests: viper.GetInt64("job-pod-requests-memory"),
		CpuLimit:    viper.GetInt64("job-pod-limits-cpu"),
		MemLimit:    viper.GetInt64("job-pod-limits-memory"),
	}
}

func initConfig() {
	readConfig()
	setupLogging()
	if value, present := os.LookupEnv("SENTRY_DSN"); present {
		setupSentry(value)
	}
}

func readConfig() {
	if cfgFile != "" {
		if cfgFile == "." {
			viper.SetConfigType("yaml")
			viper.ReadConfig(os.Stdin)
			return
		} else {
			viper.SetConfigFile(cfgFile)
		}
	} else {
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		viper.SetConfigName("opslevel")
		viper.SetConfigType("yaml")
		viper.AddConfigPath(".")
		viper.AddConfigPath(home)
	}
	viper.SetEnvPrefix("OPSLEVEL")
	viper.AutomaticEnv()
	viper.ReadInConfig()
}

func setupLogging() {
	logFormat := strings.ToLower(viper.GetString("log-format"))
	logLevel := strings.ToLower(viper.GetString("log-level"))

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	if logFormat == "text" {
		output := zerolog.ConsoleWriter{Out: os.Stderr}
		log.Logger = log.Output(output)
	}

	switch {
	case logLevel == "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	case logLevel == "warn":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case logLevel == "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case logLevel == "trace":
		zerolog.SetGlobalLevel(zerolog.TraceLevel)
	default:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
}

func setupSentry(dsn string) {
	err := sentry.Init(sentry.ClientOptions{
		Dsn: dsn,

		// Set TracesSampleRate to 1.0 to capture 100%
		// of transactions for performance monitoring.
		// We recommend adjusting this value in production,
		TracesSampleRate: 1.0,
	})
	if err != nil {
		log.Error().Msgf("sentry.Init: %s", err)
	}
}
