package cmd

import (
	"github.com/getsentry/sentry-go"
	"github.com/opslevel/opslevel-go/v2022"
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
	cobra.CheckErr(rootCmd.Execute())
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "./opslevel.yaml", "configuration options for the runner")
	rootCmd.PersistentFlags().String("api-url", "https://api.opslevel.com", "The OpsLevel API Url. Overrides environment variable 'OPSLEVEL_API_URL'")
	rootCmd.PersistentFlags().String("api-token", "", "The OpsLevel API Token. Overrides environment variable 'OPSLEVEL_API_TOKEN'")
	rootCmd.PersistentFlags().String("log-format", "TEXT", "overrides environment variable 'OPSLEVEL_LOG_FORMAT' (options [\"JSON\", \"TEXT\"])")
	rootCmd.PersistentFlags().String("log-level", "INFO", "overrides environment variable 'OPSLEVEL_LOG_LEVEL' (options [\"ERROR\", \"WARN\", \"INFO\", \"DEBUG\"])")

	rootCmd.PersistentFlags().Int("pod-max-wait", 60, "The max amount of time to wait for the job pod to become healthy.")
	rootCmd.PersistentFlags().String("pod-namespace", "default", "The kubernetes namespace to create pods in.")
	rootCmd.PersistentFlags().String("pod-shell", "/bin/sh", "The shell to use for commands inside the pod.")
	rootCmd.PersistentFlags().Int("pod-log-max-interval", 30, "The max amount of time between when pod logs are shipped to OpsLevel. Works in tandem with 'pod-log-max-size'")
	rootCmd.PersistentFlags().Int("pod-log-max-size", 1000000, "The max amount in bytes to buffer before pod logs are shipped to OpsLevel. Works in tandem with 'pod-log-max-interval'")

	viper.BindPFlags(rootCmd.PersistentFlags())
	viper.BindEnv("log-format", "OPSLEVEL_LOG_FORMAT")
	viper.BindEnv("log-level", "OPSLEVEL_LOG_LEVEL")
	viper.BindEnv("api-url", "OPSLEVEL_API_URL", "OPSLEVEL_APP_URL")
	viper.BindEnv("api-token", "OPSLEVEL_API_TOKEN")
	viper.BindEnv("pod-max-wait", "OPSLEVEL_POD_MAX_WAIT")
	viper.BindEnv("pod-namespace", "OPSLEVEL_POD_NAMESPACE")
	viper.BindEnv("pod-shell", "OPSLEVEL_POD_SHELL")
	viper.BindEnv("pod-log-max-interval", "OPSLEVEL_POD_LOG_MAX_INTERVAL")
	viper.BindEnv("pod-log-max-size", "OPSLEVEL_POD_LOG_MAX_SIZE")
	cobra.OnInitialize(initConfig)
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

var _clientGQL *opslevel.Client

func getClientGQL() *opslevel.Client {
	if _clientGQL == nil {
		_clientGQL = pkg.NewGraphClient(version)
	}
	return _clientGQL
}
