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

type Worker func(context.Context) error

func Run(
	ctx context.Context,
	shutdownTimeout time.Duration,
	workers ...Worker,
) error {
	if len(workers) == 0 {
		return nil
	}

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup

	results := startWorkers(workerCtx, workers, &wg)

	remaining, firstErr := waitForShutdown(
		ctx, cancel,
		results, len(workers),
	)

	if remaining == 0 {
		wg.Wait()
		return firstErr
	}

	cleanupErr := waitForWorkers(
		results, remaining,
		shutdownTimeout, workerCtx.Err(),
	)

	if errors.Is(cleanupErr, ErrShutdownTimeout) {
		return errors.Join(firstErr, cleanupErr)
	}

	wg.Wait()

	return errors.Join(firstErr, cleanupErr)
}

func startWorkers(
	ctx context.Context,
	workers []Worker,
	wg *sync.WaitGroup,
) <-chan error {
	results := make(chan error, len(workers))

	for _, worker := range workers {
		wg.Go(func() {
			results <- worker(ctx)
		})
	}

	return results
}

func waitForShutdown(
	ctx context.Context,
	cancel context.CancelFunc,
	results <-chan error,
	workersCount int,
) (remaining int, firstErr error) {
	remaining = workersCount

	if ctx.Err() != nil {
		cancel()

		return remaining, nil
	}

	for remaining > 0 {
		select {
		case err := <-results:
			remaining--

			if err == nil {
				continue
			}

			if ctxErr := ctx.Err(); ctxErr != nil {
				cancel()

				if errors.Is(err, ctxErr) {
					return remaining, nil
				}

				return remaining, err
			}

			cancel()

			return remaining, err

		case <-ctx.Done():
			cancel()

			return remaining, nil
		}
	}

	return 0, nil
}

func waitForWorkers(
	results <-chan error,
	remaining int,
	shutdownTimeout time.Duration,
	cancellationErr error,
) error {
	timer := time.NewTimer(shutdownTimeout)
	defer timer.Stop()

	var firstErr error

	for remaining > 0 {
		select {
		case err := <-results:
			remaining--
			firstErr = keepFirstWorkerResult(firstErr, err, cancellationErr)

		case <-timer.C:
			return drainReadyResults(
				results, remaining, firstErr,
				cancellationErr, shutdownTimeout,
			)
		}
	}

	return firstErr
}

func drainReadyResults(
	results <-chan error,
	remaining int,
	firstErr error,
	cancellationError error,
	shutdownTimeout time.Duration,
) error {
	for remaining > 0 {
		select {
		case err := <-results:
			remaining--
			firstErr = keepFirstWorkerResult(firstErr, err, cancellationError)

		default:
			timeoutErr := fmt.Errorf(
				"workers did not finish within %s: %w",
				shutdownTimeout, ErrShutdownTimeout,
			)

			return errors.Join(firstErr, timeoutErr)
		}
	}

	return firstErr
}

func keepFirstWorkerResult(
	firstErr, err, cancellationError error,
) error {
	if firstErr != nil || err == nil || errors.Is(err, cancellationError) {
		return firstErr
	}

	return err
}
