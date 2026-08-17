package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrShutdownTimeout = errors.New("shutdown timeout")
)

type Worker func(context.Context)

func Run(
	ctx context.Context,
	shutdownTimeout time.Duration,
	workers ...Worker,
) error {
	if len(workers) == 0 {
		return nil
	}

	allDone := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(len(workers))

	for _, worker := range workers {
		go func(w Worker) {
			defer wg.Done()
			w(ctx)
		}(worker)
	}

	go func() {
		wg.Wait()
		close(allDone)
	}()

	select {
	case <-allDone:
		return nil
	case <-ctx.Done():
	}

	timer := time.NewTimer(shutdownTimeout)
	defer timer.Stop()

	select {
	case <-allDone:
		return nil
	case <-timer.C:
		select {
		case <-allDone:
			return nil
		default:
			return fmt.Errorf(
				"workers did not finish within %s: %w",
				shutdownTimeout,
				ErrShutdownTimeout,
			)
		}
	}
}
