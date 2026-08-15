package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/yyeart/flashdrop/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	run(ctx, func(ctx context.Context) {
		<-ctx.Done()
	})
}

func run(ctx context.Context, worker func(context.Context)) {
	app.Run(ctx, worker)
}
