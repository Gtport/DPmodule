package worker

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// CronWorker runs a job on a fixed interval — reference implementation.
// Copy and adapt: replace the job func with real business logic.
type CronWorker struct {
	name     string
	interval time.Duration
	log      *zap.Logger
	job      func(ctx context.Context) error
}

func NewCronWorker(name string, interval time.Duration, log *zap.Logger, job func(ctx context.Context) error) *CronWorker {
	return &CronWorker{name: name, interval: interval, log: log, job: job}
}

func (w *CronWorker) Name() string { return w.name }

func (w *CronWorker) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := w.job(ctx); err != nil {
				w.log.Error("cron job failed",
					zap.String("worker", w.name),
					zap.Error(err),
				)
				// Log and continue — don't stop the cron on a single failure.
			}
		}
	}
}
