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

	first := func(context.Context) error {
		close(firstStarted)
		<-finishFirst
		return nil
	}

	second := func(context.Context) error {
		close(secondStarted)
		<-finishSecond
		return nil
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

	first := func(ctx context.Context) error {
		<-ctx.Done()
		close(firstCancelled)
		<-finishFirst
		return nil
	}

	second := func(ctx context.Context) error {
		<-ctx.Done()
		close(secondCancelled)
		<-finishSecond
		return nil
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

	finishedWorker := func(ctx context.Context) error {
		<-ctx.Done()
		close(finishedWorkerCancelled)
		return nil
	}

	blockedWorker := func(ctx context.Context) error {
		<-ctx.Done()
		close(blockedWorkerCancelled)

		<-releaseBlockedWorker
		close(blockedWorkerFinished)
		return nil
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

	worker := func(context.Context) error {
		close(workerStarted)
		<-finishWorker
		return nil
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

func TestRun_NilResultDoesNotStartShutdownTimeout(t *testing.T) {
	firstFinished := make(chan struct{})
	releaseSecond := make(chan struct{})
	runReturned := make(chan error, 1)

	first := func(context.Context) error {
		close(firstFinished)
		return nil
	}

	second := func(context.Context) error {
		<-releaseSecond
		return nil
	}

	shutdownTimeout := 10 * time.Millisecond

	go func() {
		runReturned <- Run(
			context.Background(),
			shutdownTimeout,
			first,
			second,
		)
	}()

	waitForSignal(t, firstFinished, "first worker did not finish")

	select {
	case err := <-runReturned:
		t.Fatalf(
			"Run returned after a successful worker without a shutdown trigger: %v",
			err,
		)
	case <-time.After(5 * shutdownTimeout):
	}

	close(releaseSecond)

	err := waitForResult(t, runReturned, "Run did not return after all workers finished")
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
}

func TestRun_ReturnsErrorAfterAnotherWorkerReturnedNil(t *testing.T) {
	wantErr := errors.New("worker failed")

	firstFinished := make(chan struct{})
	releaseFailure := make(chan struct{})
	runReturned := make(chan error, 1)

	first := func(context.Context) error {
		close(firstFinished)
		return nil
	}

	second := func(context.Context) error {
		<-releaseFailure
		return wantErr
	}

	go func() {
		runReturned <- Run(context.Background(), testTimeout, first, second)
	}()

	waitForSignal(t, firstFinished, "first worker did not finish")
	close(releaseFailure)

	err := waitForResult(t, runReturned, "Run did not return the worker error")
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want error wrapping %v", err, wantErr)
	}
}

func TestRun_WorkerErrorCancelsOthersAndWaitsForCleanup(t *testing.T) {
	wantErr := errors.New("worker failed")

	secondStarted := make(chan struct{})
	secondCancelled := make(chan struct{})
	releaseSecond := make(chan struct{})
	runReturned := make(chan error, 1)

	first := func(context.Context) error {
		<-secondStarted
		return wantErr
	}

	second := func(ctx context.Context) error {
		close(secondStarted)
		<-ctx.Done()
		close(secondCancelled)
		<-releaseSecond
		return nil
	}

	go func() {
		runReturned <- Run(context.Background(), testTimeout, first, second)
	}()

	waitForSignal(t, secondCancelled, "second worker did not observe cancellation")
	assertNoResult(t, runReturned, "Run returned before worker cleanup finished")

	close(releaseSecond)

	err := waitForResult(t, runReturned, "Run did not return after worker cleanup")
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want error wrapping %v", err, wantErr)
	}
}

func TestRun_WorkerErrorAndShutdownTimeoutAreBothReturned(t *testing.T) {
	wantErr := errors.New("worker failed")

	blockedStarted := make(chan struct{})
	blockedCancelled := make(chan struct{})
	releaseBlocked := make(chan struct{})
	blockedFinished := make(chan struct{})

	failingWorker := func(context.Context) error {
		<-blockedStarted
		return wantErr
	}

	blockedWorker := func(ctx context.Context) error {
		close(blockedStarted)
		<-ctx.Done()
		close(blockedCancelled)
		<-releaseBlocked
		close(blockedFinished)
		return nil
	}

	err := Run(
		context.Background(),
		10*time.Millisecond,
		failingWorker,
		blockedWorker,
	)

	if !errors.Is(err, wantErr) {
		t.Errorf("Run() error = %v, want error wrapping %v", err, wantErr)
	}

	if !errors.Is(err, ErrShutdownTimeout) {
		t.Errorf(
			"Run() error = %v, want error wrapping ErrShutdownTimeout",
			err,
		)
	}

	waitForSignal(t, blockedCancelled, "blocked worker did not observe cancellation")
	close(releaseBlocked)
	waitForSignal(t, blockedFinished, "blocked worker did not finish")
}

func TestWaitForWorkers_DoesNotTimeoutWhenLastResultIsReady(t *testing.T) {
	const attempts = 1000

	for range attempts {
		results := make(chan error, 1)
		results <- nil

		err := waitForWorkers(results, 1, 0, nil)
		if err != nil {
			t.Fatalf("waitForWorkers() error = %v, want nil", err)
		}
	}
}

func TestRun_ExternalCancellationIgnoresContextCancellationError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	workerStarted := make(chan struct{})
	runReturned := make(chan error, 1)

	worker := func(ctx context.Context) error {
		close(workerStarted)
		<-ctx.Done()
		return ctx.Err()
	}

	go func() {
		runReturned <- Run(ctx, testTimeout, worker)
	}()

	waitForSignal(t, workerStarted, "worker did not start")
	cancel()

	err := waitForResult(t, runReturned, "Run did not return after cancellation")
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

	first := func(ctx context.Context) error {
		close(firstStarted)

		<-ctx.Done()
		close(firstCancelled)

		<-finishFirst
		return nil
	}

	second := func(ctx context.Context) error {
		close(secondStarted)

		<-ctx.Done()
		close(secondCancelled)

		<-finishSecond
		return nil
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

func TestRun_AlreadyCancelledContextPreservesRealWorkerError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	wantErr := errors.New("listener failed")

	err := Run(
		ctx,
		testTimeout,
		func(context.Context) error {
			return wantErr
		},
	)

	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want error wrapping %v", err, wantErr)
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
