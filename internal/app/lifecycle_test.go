package app

import (
	"context"
	"testing"
	"time"
)

func TestRun_CancelStopsWorkerAndWaitsForCompletion(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{})
	cancelObserved := make(chan struct{})
	allowWorkerToFinish := make(chan struct{})
	runReturned := make(chan struct{})

	worker := func(ctx context.Context) {
		close(started)

		<-ctx.Done()
		close(cancelObserved)

		<-allowWorkerToFinish
	}

	go func() {
		Run(ctx, worker)
		close(runReturned)
	}()

	<-started

	cancel()

	<-cancelObserved

	select {
	case <-runReturned:
		t.Fatal("Run returned before worker finished")
	default:
	}

	close(allowWorkerToFinish)

	select {
	case <-runReturned:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after worker finished")
	}
}

func TestRun_ReturnsWhenWorkerFinishesByItself(t *testing.T) {
	ctx := context.Background()

	workerFinished := make(chan struct{})
	runReturned := make(chan struct{})

	worker := func(context.Context) {
		close(workerFinished)
	}

	go func() {
		Run(ctx, worker)
		close(runReturned)
	}()

	select {
	case <-workerFinished:
	case <-time.After(time.Second):
		t.Fatal("worker did not finish")
	}

	select {
	case <-runReturned:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after worker finished")
	}
}

func TestRun_PassesContextToWorker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	cancelObserved := make(chan struct{})
	runReturned := make(chan struct{})

	worker := func(ctx context.Context) {
		<-ctx.Done()
		close(cancelObserved)
	}

	go func() {
		Run(ctx, worker)
		close(runReturned)
	}()

	cancel()

	select {
	case <-cancelObserved:
	case <-time.After(time.Second):
		t.Fatal("worker did not observe context cancellation")
	}

	select {
	case <-runReturned:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after worker finished")
	}
}
