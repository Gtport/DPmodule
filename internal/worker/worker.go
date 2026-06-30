package worker

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// Worker is the interface every background job must implement.
type Worker interface {
	Name() string
	Run(ctx context.Context) error
}

// Run starts all workers on their own goroutines and blocks until ctx is cancelled.
// Each worker is restarted automatically if it returns an error.
func Run(ctx context.Context, log *zap.Logger, workers ...Worker) {
	for _, w := range workers {
		w := w
		go runWorker(ctx, log, w)
	}
	<-ctx.Done()
	log.Info("worker manager: context cancelled, stopping all workers")
}

func runWorker(ctx context.Context, log *zap.Logger, w Worker) {
	log.Info("worker started", zap.String("worker", w.Name()))
	for {
		if err := w.Run(ctx); err != nil {
			if ctx.Err() != nil {
				// Context cancelled — normal shutdown.
				log.Info("worker stopped", zap.String("worker", w.Name()))
				return
			}
			log.Error("worker error, restarting in 5s",
				zap.String("worker", w.Name()),
				zap.Error(err),
			)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}
		// Worker returned nil — means it finished cleanly, no restart.
		log.Info("worker finished", zap.String("worker", w.Name()))
		return
	}
}
