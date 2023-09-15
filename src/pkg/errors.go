package pkg

import (
	"os"

	"github.com/getsentry/sentry-go"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

func CheckErr(err error) {
	if err != nil {
		if _, present := os.LookupEnv("SENTRY_DSN"); present {
			sentry.CaptureException(err)
		} else {
			log.Error().Err(err).Msg("")
		}
	}
	cobra.CheckErr(err)
}
