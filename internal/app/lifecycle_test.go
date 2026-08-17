package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRun_ReturnsWhenWorkerFinishesBeforeCancellation(t *testing.T) {
	ctx := context.Background()

	worker := func(context.Context) {}

	err := Run(ctx, worker, time.Second)
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
}

func TestRun_CancellationWaitsForWorker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cancelObserved := make(chan struct{})
	allowWorkerToFinish := make(chan struct{})
	runReturned := make(chan error, 1)

	worker := func(ctx context.Context) {
		<-ctx.Done()
		close(cancelObserved)

		<-allowWorkerToFinish
	}

	go func() {
		runReturned <- Run(ctx, worker, time.Second)
	}()

	cancel()

	select {
	case <-cancelObserved:
	case <-time.After(time.Second):
		t.Fatal("worker did not observe context cancellation")
	}

	select {
	case err := <-runReturned:
		t.Fatalf("Run() returned before worker finished: %v", err)
	default:
	}

	close(allowWorkerToFinish)

	select {
	case err := <-runReturned:
		if err != nil {
			t.Fatalf("Run() error = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not return after worker finished")
	}
}

func TestRun_ReturnsShutdownTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	workerStarted := make(chan struct{})
	allowWorkerToFinish := make(chan struct{})
	workerFinished := make(chan struct{})

	worker := func(ctx context.Context) {
		close(workerStarted)

		<-ctx.Done()
		<-allowWorkerToFinish

		close(workerFinished)
	}

	runReturned := make(chan error, 1)

	go func() {
		runReturned <- Run(ctx, worker, 50*time.Millisecond)
	}()

	<-workerStarted
	cancel()

	var err error

	select {
	case err = <-runReturned:
	case <-time.After(time.Second):
		t.Fatal("Run() did not return after shutdown timeout")
	}

	if !errors.Is(err, ErrShutdownTimeout) {
		t.Fatalf(
			"Run() error = %v, want error wrapping ErrShutdownTimeout",
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
