package pkg

import (
	"github.com/getsentry/sentry-go"
	"github.com/spf13/cobra"
)

func CheckErr(err error) {
	if err != nil {
		sentry.CaptureException(err)
	}
	cobra.CheckErr(err)
}
