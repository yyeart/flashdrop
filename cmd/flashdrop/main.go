package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yyeart/flashdrop/internal/app"
	"github.com/yyeart/flashdrop/internal/config"
)

func main() {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	config, err := config.Load()
	if err != nil {
		fmt.Printf("failed to load config: %v", err)

		os.Exit(1)
	}

	if err := run(
		ctx,
		func(ctx context.Context) {
			<-ctx.Done()
		},
		config.ShutdownTimeout,
	); err != nil {
		fmt.Printf("worker error: %v", err)

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
