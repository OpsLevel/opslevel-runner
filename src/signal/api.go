package signal

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog/log"
)

func Init(parent context.Context) context.Context {
	ctx, cancel := context.WithCancel(parent)
	closeChannel := make(chan os.Signal, 1)
	signal.Notify(closeChannel, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		log.Info().Msg("waiting for interrupt signal")
		sig := <-closeChannel
		log.Info().Str("signal", sig.String()).Msg("Handling interruption")
		cancel()
	}()
	return ctx
}
