package main

import (
	"context"
	"testing"
	"time"
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
		run(ctx, worker)
		close(runReturned)
	}()

	<-workerStarted

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
