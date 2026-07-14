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
