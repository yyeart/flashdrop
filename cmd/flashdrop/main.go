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
		func(ctx context.Context) {
			<-ctx.Done()
		},
		config.ShutdownTimeout,
	); err != nil {
		logger.Error("worker error", "err", err)

		os.Exit(1)
	}
}

func run(
	ctx context.Context,
	worker func(context.Context),
	shutdownTimeout time.Duration,
) error {
	if err := app.Run(ctx, worker, shutdownTimeout); err != nil {
		return err
	}

	return nil
}
