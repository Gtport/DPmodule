package worker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

// job вызывается по тику, а отмена context останавливает Run.
func TestCronWorker_TicksAndStops(t *testing.T) {
	var calls atomic.Int32
	fired := make(chan struct{}, 1)
	job := func(context.Context) error {
		calls.Add(1)
		select {
		case fired <- struct{}{}:
		default:
		}
		return nil
	}
	w := NewCronWorker("test", 10*time.Millisecond, zap.NewNop(), job)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = w.Run(ctx); close(done) }()

	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("job не вызвался по тику")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run не остановился по отмене context")
	}
	if calls.Load() == 0 {
		t.Fatal("ожидали хотя бы один вызов job")
	}
}

// nextAligned: тики в offset, offset+interval, ... от границы часа, строго после now.
func TestNextAligned(t *testing.T) {
	interval, offset := 10*time.Minute, 5*time.Minute
	base := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	cases := []struct{ now, want time.Time }{
		{base, base.Add(5 * time.Minute)},                                       // 12:00 → 12:05
		{base.Add(5 * time.Minute), base.Add(15 * time.Minute)},                 // ровно 12:05 → 12:15 (строго после)
		{base.Add(7 * time.Minute), base.Add(15 * time.Minute)},                 // 12:07 → 12:15
		{base.Add(59*time.Minute + 30*time.Second), base.Add(65 * time.Minute)}, // 12:59:30 → 13:05
		{base.Add(14*time.Minute + 59*time.Second), base.Add(15 * time.Minute)}, // 12:14:59 → 12:15
	}
	for _, c := range cases {
		if got := nextAligned(c.now, interval, offset); !got.Equal(c.want) {
			t.Errorf("nextAligned(%v) = %v, ожидали %v", c.now, got, c.want)
		}
	}

	// offset 0 → границы интервала: 12:07 → 12:10.
	if got := nextAligned(base.Add(7*time.Minute), interval, 0); !got.Equal(base.Add(10 * time.Minute)) {
		t.Errorf("offset 0: получили %v", got)
	}
	// offset >= interval нормализуется по модулю: 15m%10m=5m.
	if got := nextAligned(base, interval, 15*time.Minute); !got.Equal(base.Add(5 * time.Minute)) {
		t.Errorf("offset mod interval: получили %v", got)
	}
}

// Выровненный воркер срабатывает и останавливается по отмене context.
func TestAlignedCronWorker_TicksAndStops(t *testing.T) {
	fired := make(chan struct{}, 1)
	job := func(context.Context) error {
		select {
		case fired <- struct{}{}:
		default:
		}
		return nil
	}
	w := NewAlignedCronWorker("test-aligned", 20*time.Millisecond, 5*time.Millisecond, zap.NewNop(), job)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = w.Run(ctx); close(done) }()

	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("выровненный job не вызвался")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run не остановился по отмене context")
	}
}

// Ошибка job не должна останавливать тикер (забор АСУ мог временно отвалиться).
func TestCronWorker_ContinuesOnJobError(t *testing.T) {
	var calls atomic.Int32
	got2 := make(chan struct{}, 1)
	job := func(context.Context) error {
		if calls.Add(1) >= 2 {
			select {
			case got2 <- struct{}{}:
			default:
			}
		}
		return errors.New("boom")
	}
	w := NewCronWorker("test-err", 10*time.Millisecond, zap.NewNop(), job)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Run(ctx) }()

	select {
	case <-got2:
	case <-time.After(2 * time.Second):
		t.Fatal("после ошибки job тикер должен продолжать вызывать job")
	}
}
