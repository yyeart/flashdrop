package app

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	ErrShutdownTimeout = errors.New("shutdown timeout")
)

type Worker func(context.Context)

func Run(
	ctx context.Context,
	worker Worker,
	shutdownTimeout time.Duration,
) error {
	done := make(chan struct{})

	go func() {
		defer close(done)
		worker(ctx)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
	}

	timer := time.NewTimer(shutdownTimeout)
	defer timer.Stop()

	select {
	case <-done:
		return nil
	case <-timer.C:
		select {
		case <-done:
			return nil
		default:
			return fmt.Errorf(
				"worker did not finish within %s: %w",
				shutdownTimeout,
				ErrShutdownTimeout,
			)
		}
	}
}
