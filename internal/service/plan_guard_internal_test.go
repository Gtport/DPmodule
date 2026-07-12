package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

// guardJournalRepo — фейк journal-репозитория для гарда: отдаёт заданный doc_ts
// последнего обновления дислокации (или «нет события»).
type guardJournalRepo struct {
	ts  *domain.LocalTime
	has bool
}

func (r guardJournalRepo) Append(context.Context, domain.JournalEvent) error { return nil }
func (r guardJournalRepo) LatestByType(_ context.Context, t string) (domain.JournalEvent, bool, error) {
	if !r.has || t != domain.EventDislUpdate {
		return domain.JournalEvent{}, false, nil
	}
	return domain.JournalEvent{EventType: t, DocTS: r.ts}, true, nil
}
func (r guardJournalRepo) LatestBySource(context.Context, string) (domain.JournalEvent, bool, error) {
	return domain.JournalEvent{}, false, nil
}
func (r guardJournalRepo) Recent(context.Context, int) ([]domain.JournalEvent, error) {
	return nil, nil
}

func cfgWithAgeGuard(minutes int) *ConfigCache {
	return &ConfigCache{settings: domain.ClientSettings{
		IngestPolicy: domain.IngestPolicy{Plan: domain.CategoryPolicy{PlanMaxDislAgeMinutes: minutes}},
	}}
}

func planProcWithGuard(cfg *ConfigCache, docTS *domain.LocalTime, hasEvent bool) *PlanProcessor {
	return &PlanProcessor{
		cfg:     cfg,
		journal: NewJournal(guardJournalRepo{ts: docTS, has: hasEvent}, nil),
	}
}

func TestEnsureDislFresh(t *testing.T) {
	ctx := context.Background()
	now := clock.Now().Time()
	ago := func(m int) *domain.LocalTime { return domain.NewLocalTime(now.Add(-time.Duration(m) * time.Minute)) }

	t.Run("stale blocks", func(t *testing.T) {
		p := planProcWithGuard(cfgWithAgeGuard(60), ago(90), true)
		err := p.ensureDislFresh(ctx)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrDislStale))
	})
	t.Run("fresh passes", func(t *testing.T) {
		p := planProcWithGuard(cfgWithAgeGuard(60), ago(10), true)
		assert.NoError(t, p.ensureDislFresh(ctx))
	})
	t.Run("no event passes", func(t *testing.T) {
		p := planProcWithGuard(cfgWithAgeGuard(60), nil, false)
		assert.NoError(t, p.ensureDislFresh(ctx))
	})
	t.Run("threshold 0 disables guard", func(t *testing.T) {
		p := planProcWithGuard(cfgWithAgeGuard(0), ago(999), true)
		assert.NoError(t, p.ensureDislFresh(ctx))
	})
	t.Run("nil config passes", func(t *testing.T) {
		p := planProcWithGuard(nil, ago(999), true)
		assert.NoError(t, p.ensureDislFresh(ctx))
	})
}
