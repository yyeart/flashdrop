package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yyeart/flashdrop/internal/app"
	"github.com/yyeart/flashdrop/internal/appLogger"
	"github.com/yyeart/flashdrop/internal/config"
)

func main() {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	btLogger := appLogger.NewLogger(slog.LevelInfo, os.Stderr)

	config, err := config.Load()
	if err != nil {
		btLogger.Error("failed to load config", "err", err)

		os.Exit(1)
	}

	logger := appLogger.NewLogger(config.LogLevel, os.Stderr)
	slog.SetDefault(logger)

	if err := run(
		ctx,
		config.ShutdownTimeout,
		func(ctx context.Context) error {
			<-ctx.Done()
			return nil
		},
	); err != nil {
		logger.Error("worker error", "err", err)

		os.Exit(1)
	}
}

func run(
	ctx context.Context,
	shutdownTimeout time.Duration,
	workers ...app.Worker,
) error {

	if err := app.Run(ctx, shutdownTimeout, workers...); err != nil {
		return err
	}

	return nil
}
