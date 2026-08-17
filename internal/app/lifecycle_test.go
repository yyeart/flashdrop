package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

const testTimeout = time.Second

func TestRun_NoWorkersReturnsNil(t *testing.T) {
	err := Run(context.Background(), testTimeout)

	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
}

func TestRun_WaitsForAllWorkers(t *testing.T) {
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})

	finishFirst := make(chan struct{})
	finishSecond := make(chan struct{})

	runReturned := make(chan error, 1)

	first := func(context.Context) {
		close(firstStarted)
		<-finishFirst
	}

	second := func(context.Context) {
		close(secondStarted)
		<-finishSecond
	}

	go func() {
		runReturned <- Run(
			context.Background(),
			testTimeout,
			first,
			second,
		)
	}()

	waitForSignal(t, firstStarted, "first worker did not start")
	waitForSignal(t, secondStarted, "second worker did not start")

	close(finishFirst)

	assertNoResult(t, runReturned, "Run returned while second worker was still running")

	close(finishSecond)

	err := waitForResult(t, runReturned, "Run did not return after all workers finished")
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
}

func TestRun_CancellationWaitsForAllWorkers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	firstCancelled := make(chan struct{})
	secondCancelled := make(chan struct{})

	finishFirst := make(chan struct{})
	finishSecond := make(chan struct{})

	runReturned := make(chan error, 1)

	first := func(ctx context.Context) {
		<-ctx.Done()
		close(firstCancelled)
		<-finishFirst
	}

	second := func(ctx context.Context) {
		<-ctx.Done()
		close(secondCancelled)
		<-finishSecond
	}

	go func() {
		runReturned <- Run(ctx, testTimeout, first, second)
	}()

	cancel()

	waitForSignal(t, firstCancelled, "first worker did not observe cancellation")
	waitForSignal(t, secondCancelled, "second worker did not observe cancellation")

	close(finishFirst)

	assertNoResult(t, runReturned, "Run returned before second worker finished")

	close(finishSecond)

	err := waitForResult(t, runReturned, "Run did not return after all workers finished")
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
}

func TestRun_ReturnsShutdownTimeoutWhenWorkerDoesNotFinish(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	finishedWorkerCancelled := make(chan struct{})
	blockedWorkerCancelled := make(chan struct{})
	releaseBlockedWorker := make(chan struct{})
	blockedWorkerFinished := make(chan struct{})

	finishedWorker := func(ctx context.Context) {
		<-ctx.Done()
		close(finishedWorkerCancelled)
	}

	blockedWorker := func(ctx context.Context) {
		<-ctx.Done()
		close(blockedWorkerCancelled)

		<-releaseBlockedWorker
		close(blockedWorkerFinished)
	}

	runReturned := make(chan error, 1)

	go func() {
		runReturned <- Run(
			ctx,
			10*time.Millisecond,
			finishedWorker,
			blockedWorker,
		)
	}()

	cancel()

	waitForSignal(t, finishedWorkerCancelled, "first worker did not observe cancellation")
	waitForSignal(t, blockedWorkerCancelled, "blocked worker did not observe cancellation")

	err := waitForResult(t, runReturned, "Run did not return after shutdown timeout")

	if !errors.Is(err, ErrShutdownTimeout) {
		t.Fatalf(
			"Run() error = %v, want error wrapping ErrShutdownTimeout",
			err,
		)
	}

	// Run вернулся по timeout, но goroutine принудительно не завершается.
	// Освобождаем worker, чтобы тест не оставлял собственные goroutine.
	close(releaseBlockedWorker)

	waitForSignal(t, blockedWorkerFinished, "blocked worker did not finish")
}

func TestRun_DoesNotApplyShutdownTimeoutBeforeCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	workerStarted := make(chan struct{})
	finishWorker := make(chan struct{})
	runReturned := make(chan error, 1)

	worker := func(context.Context) {
		close(workerStarted)
		<-finishWorker
	}

	shutdownTimeout := 10 * time.Millisecond

	go func() {
		runReturned <- Run(ctx, shutdownTimeout, worker)
	}()

	waitForSignal(t, workerStarted, "worker did not start")

	// Проходит больше shutdownTimeout, но ctx ещё не отменён.
	// Run всё ещё должен находиться в первой фазе.
	select {
	case err := <-runReturned:
		t.Fatalf(
			"Run returned before context cancellation: %v",
			err,
		)
	case <-time.After(5 * shutdownTimeout):
	}

	close(finishWorker)

	err := waitForResult(t, runReturned, "Run did not return after worker finished")
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
}

func TestRun_AlreadyCancelledContextStillStartsAllWorkers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})

	firstCancelled := make(chan struct{})
	secondCancelled := make(chan struct{})

	finishFirst := make(chan struct{})
	finishSecond := make(chan struct{})

	runReturned := make(chan error, 1)

	first := func(ctx context.Context) {
		close(firstStarted)

		<-ctx.Done()
		close(firstCancelled)

		<-finishFirst
	}

	second := func(ctx context.Context) {
		close(secondStarted)

		<-ctx.Done()
		close(secondCancelled)

		<-finishSecond
	}

	go func() {
		runReturned <- Run(
			ctx,
			testTimeout,
			first,
			second,
		)
	}()

	waitForSignal(t, firstStarted, "first worker was not started")
	waitForSignal(t, secondStarted, "second worker was not started")

	waitForSignal(t, firstCancelled, "first worker did not observe cancellation")
	waitForSignal(t, secondCancelled, "second worker did not observe cancellation")

	assertNoResult(t, runReturned, "Run returned before workers completed cleanup")

	close(finishFirst)

	assertNoResult(t, runReturned, "Run returned before second worker completed cleanup")

	close(finishSecond)

	err := waitForResult(t, runReturned, "Run did not wait for worker cleanup")
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
}

func waitForSignal(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(testTimeout):
		t.Fatal(message)
	}
}

func waitForResult(t *testing.T, ch <-chan error, message string) error {
	t.Helper()

	select {
	case err := <-ch:
		return err
	case <-time.After(testTimeout):
		t.Fatal(message)
		return nil
	}
}

func assertNoResult(t *testing.T, ch <-chan error, message string) {
	t.Helper()

	select {
	case err := <-ch:
		t.Fatalf("%s: %v", message, err)
	default:
	}
}
