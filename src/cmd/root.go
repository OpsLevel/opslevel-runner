package cmd

import (
	"os"
	"strings"

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

func Execute() {
	cobra.CheckErr(rootCmd.Execute())
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "./opslevel.yaml", "configuration options for the runner")
	rootCmd.PersistentFlags().String("log-format", "TEXT", "overrides environment variable 'OPSLEVEL_LOG_FORMAT' (options [\"JSON\", \"TEXT\"])")
	rootCmd.PersistentFlags().String("log-level", "INFO", "overrides environment variable 'OPSLEVEL_LOG_LEVEL' (options [\"ERROR\", \"WARN\", \"INFO\", \"DEBUG\"])")

	rootCmd.PersistentFlags().Int("pod-max-wait", 60, "The max amount of time to wait for the job pod to become healthy.")
	rootCmd.PersistentFlags().String("pod-shell", "/bin/sh", "The shell to use for commands inside the pod.")
	rootCmd.PersistentFlags().Int("pod-log-max-interval", 30, "The max amount of time between when pod logs are shipped to OpsLevel. Works in tandem with 'pod-log-max-size'")
	rootCmd.PersistentFlags().Int("pod-log-max-size", 1000000, "The max amount in bytes to buffer before pod logs are shipped to OpsLevel. Works in tandem with 'pod-log-max-interval'")

	viper.BindPFlags(rootCmd.PersistentFlags())
	viper.BindEnv("log-format", "OPSLEVEL_LOG_FORMAT")
	viper.BindEnv("log-level", "OPSLEVEL_LOG_LEVEL")
	viper.BindEnv("pod-max-wait", "OPSLEVEL_POD_MAX_WAIT")
	viper.BindEnv("pod-shell", "OPSLEVEL_POD_SHELL")
	viper.BindEnv("pod-log-max-interval", "OPSLEVEL_POD_LOG_MAX_INTERVAL")
	viper.BindEnv("pod-log-max-size", "OPSLEVEL_POD_LOG_MAX_SIZE")
	cobra.OnInitialize(initConfig)
}

func initConfig() {
	readConfig()
	setupLogging()
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
