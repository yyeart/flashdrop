package app

import "context"

type Worker func(context.Context)

func Run(ctx context.Context, worker Worker) {
	done := make(chan struct{})

	go func() {
		defer close(done)
		worker(ctx)
	}()

	<-done
}
