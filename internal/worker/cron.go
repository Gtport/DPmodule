package worker

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// CronWorker runs a job on a fixed interval. Два режима:
//   - обычный тикер (NewCronWorker): первый тик через interval после старта процесса;
//   - выровненный (NewAlignedCronWorker): тики привязаны к стеночным часам — моменты
//     offset, offset+interval, ... от границы интервала (interval=10m, offset=5m →
//     :05, :15, :25, :35, :45, :55 каждого часа, независимо от времени старта).
type CronWorker struct {
	name     string
	interval time.Duration
	offset   time.Duration
	aligned  bool
	log      *zap.Logger
	job      func(ctx context.Context) error
}

func NewCronWorker(name string, interval time.Duration, log *zap.Logger, job func(ctx context.Context) error) *CronWorker {
	return &CronWorker{name: name, interval: interval, log: log, job: job}
}

// NewAlignedCronWorker — тики по стеночным часам: offset от границы interval.
// Для привязки к минутам часа interval должен делить час нацело (10m, 15m, 30m...).
func NewAlignedCronWorker(name string, interval, offset time.Duration, log *zap.Logger, job func(ctx context.Context) error) *CronWorker {
	return &CronWorker{name: name, interval: interval, offset: offset, aligned: true, log: log, job: job}
}

func (w *CronWorker) Name() string { return w.name }

func (w *CronWorker) Run(ctx context.Context) error {
	if w.aligned {
		return w.runAligned(ctx)
	}
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			w.runJob(ctx)
		}
	}
}

// runAligned ждёт до ближайшего выровненного момента, выполняет job и повторяет.
// Если job работал дольше interval, пропущенные моменты не навёрстываются —
// следующий тик снова выровнен.
func (w *CronWorker) runAligned(ctx context.Context) error {
	for {
		timer := time.NewTimer(time.Until(nextAligned(time.Now(), w.interval, w.offset)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
			w.runJob(ctx)
		}
	}
}

func (w *CronWorker) runJob(ctx context.Context) {
	if err := w.job(ctx); err != nil {
		w.log.Error("cron job failed",
			zap.String("worker", w.name),
			zap.Error(err),
		)
		// Log and continue — don't stop the cron on a single failure.
	}
}

// nextAligned — ближайший момент строго после now, отстоящий на offset от границы
// interval по стеночным часам. Не TZ-операция: границы Truncate абсолютные, минуты
// часа совпадают в любом поясе с целочасовым смещением.
func nextAligned(now time.Time, interval, offset time.Duration) time.Time {
	next := now.Truncate(interval).Add(offset % interval)
	for !next.After(now) {
		next = next.Add(interval)
	}
	return next
}
