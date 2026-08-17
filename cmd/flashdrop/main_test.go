package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yyeart/flashdrop/internal/app"
)

func TestRun_CancelStopsWorkerAndWaitsForCompletion(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	workerStarted := make(chan struct{})
	cancelObserved := make(chan struct{})
	allowWorkerToFinish := make(chan struct{})
	runReturned := make(chan struct{})

	worker := func(ctx context.Context) {
		close(workerStarted)

		<-ctx.Done()
		close(cancelObserved)

		<-allowWorkerToFinish
	}

	go func() {
		if err := run(ctx, time.Second, worker); err != nil {
			return
		}
		close(runReturned)
	}()

	select {
	case <-workerStarted:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}

	cancel()

	select {
	case <-cancelObserved:
	case <-time.After(time.Second):
		t.Fatal("worker did not observe context cancellation")
	}

	select {
	case <-runReturned:
		t.Fatal("run returned before worker finished")
	default:
	}

	close(allowWorkerToFinish)

	select {
	case <-runReturned:
	case <-time.After(time.Second):
		t.Fatal("run did not return after worker finished")
	}
}

func TestRun_ReturnsWhenWorkerFinishesByItself(t *testing.T) {
	ctx := context.Background()

	workerStarted := make(chan struct{})
	allowWorkerToFinish := make(chan struct{})
	runReturned := make(chan struct{})

	worker := func(context.Context) {
		close(workerStarted)
		<-allowWorkerToFinish
	}

	go func() {
		if err := run(ctx, time.Second, worker); err != nil {
			return
		}
		close(runReturned)
	}()

	select {
	case <-workerStarted:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}

	select {
	case <-runReturned:
		t.Fatal("run returned before worker finished")
	default:
	}

	close(allowWorkerToFinish)

	select {
	case <-runReturned:
	case <-time.After(time.Second):
		t.Fatal("run did not return after worker finished")
	}
}

func TestRun_DoesNotTimeoutBeforeContextCancellation(t *testing.T) {
	ctx := context.Background()

	workerStarted := make(chan struct{})
	allowWorkerToFinish := make(chan struct{})
	runReturned := make(chan struct{})

	worker := func(context.Context) {
		close(workerStarted)
		<-allowWorkerToFinish
	}

	shutdownTimeout := 10 * time.Millisecond

	go func() {
		if err := run(ctx, shutdownTimeout, worker); err != nil {
			return
		}
		close(runReturned)
	}()

	select {
	case <-workerStarted:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}

	select {
	case <-runReturned:
		t.Fatal("run returned by shutdown timeout before context cancellation")
	case <-time.After(5 * shutdownTimeout):
	}

	close(allowWorkerToFinish)

	select {
	case <-runReturned:
	case <-time.After(time.Second):
		t.Fatal("run did not return after worker finished")
	}
}

func TestRun_AlreadyCancelledContextStillStartsAndWaitsForWorker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	workerStarted := make(chan struct{})
	cancelObserved := make(chan struct{})
	allowCleanupToFinish := make(chan struct{})
	runReturned := make(chan struct{})

	worker := func(ctx context.Context) {
		close(workerStarted)

		<-ctx.Done()
		close(cancelObserved)

		<-allowCleanupToFinish
	}

	go func() {
		if err := run(ctx, time.Second, worker); err != nil {
			return
		}
		close(runReturned)
	}()

	select {
	case <-workerStarted:
	case <-time.After(time.Second):
		t.Fatal("worker was not started for already cancelled context")
	}

	select {
	case <-cancelObserved:
	case <-time.After(time.Second):
		t.Fatal("worker did not observe already cancelled context")
	}

	select {
	case <-runReturned:
		t.Fatal("run returned before worker cleanup finished")
	default:
	}

	close(allowCleanupToFinish)

	select {
	case <-runReturned:
	case <-time.After(time.Second):
		t.Fatal("run did not return after worker cleanup finished")
	}
}

func TestRun_ReturnsShutdownTimeoutError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	workerStarted := make(chan struct{})
	allowWorkerToFinish := make(chan struct{})
	workerFinished := make(chan struct{})
	runReturned := make(chan error, 1)

	worker := func(ctx context.Context) {
		close(workerStarted)

		<-ctx.Done()
		<-allowWorkerToFinish

		close(workerFinished)
	}

	go func() {
		runReturned <- run(ctx, 10*time.Millisecond, worker)
	}()

	select {
	case <-workerStarted:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}

	cancel()

	var err error

	select {
	case err = <-runReturned:
	case <-time.After(time.Second):
		t.Fatal("run did not return after shutdown timeout")
	}

	if !errors.Is(err, app.ErrShutdownTimeout) {
		t.Fatalf(
			"run() error = %v, want error wrapping app.ErrShutdownTimeout",
			err,
		)
	}

	close(allowWorkerToFinish)

	select {
	case <-workerFinished:
	case <-time.After(time.Second):
		t.Fatal("worker did not finish")
	}
}
