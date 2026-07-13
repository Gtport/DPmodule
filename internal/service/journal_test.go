package service_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/auth"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/service"
)

// fakeJournalRepo — in-memory port.JournalRepository для юнит-тестов.
type fakeJournalRepo struct {
	events []domain.JournalEvent
	err    error
}

func (f *fakeJournalRepo) Append(_ context.Context, ev domain.JournalEvent) error {
	if f.err != nil {
		return f.err
	}
	f.events = append(f.events, ev)
	return nil
}
func (f *fakeJournalRepo) LatestByType(_ context.Context, t string) (domain.JournalEvent, bool, error) {
	for i := len(f.events) - 1; i >= 0; i-- {
		if f.events[i].EventType == t {
			return f.events[i], true, nil
		}
	}
	return domain.JournalEvent{}, false, nil
}
func (f *fakeJournalRepo) LatestBySource(_ context.Context, s string) (domain.JournalEvent, bool, error) {
	for i := len(f.events) - 1; i >= 0; i-- {
		if f.events[i].Source == s {
			return f.events[i], true, nil
		}
	}
	return domain.JournalEvent{}, false, nil
}
func (f *fakeJournalRepo) Recent(_ context.Context, limit int) ([]domain.JournalEvent, error) {
	return f.events, nil
}
func (f *fakeJournalRepo) Range(_ context.Context, _, _ *domain.LocalTime, types []string, _ int) ([]domain.JournalEvent, error) {
	var out []domain.JournalEvent
	for _, e := range f.events {
		for _, t := range types {
			if e.EventType == t {
				out = append(out, e)
				break
			}
		}
	}
	return out, nil
}

func jlt(s string) domain.LocalTime {
	t, err := time.Parse("2006-01-02T15:04:05", s)
	if err != nil {
		panic(err)
	}
	return domain.LocalTime(t)
}

func TestRecordDislUpdate_OldestFormationTS(t *testing.T) {
	repo := &fakeJournalRepo{}
	j := service.NewJournal(repo, nil)
	ctx := auth.WithClaims(context.Background(), &auth.Claims{Username: "User2"})

	files := []service.LKFileInfo{
		{Okpo: "111", Organisation: "НМТП", Terminals: []string{"АЭ"}, FormationTS: jlt("2026-07-12T08:30:00"), AgeMinutes: 40},
		{Okpo: "222", Organisation: "АТТИС", Terminals: []string{"УТ-1"}, FormationTS: jlt("2026-07-12T08:10:00"), AgeMinutes: 60},
	}
	j.RecordDislUpdate(ctx, "lk", domain.TriggerManual, files, 1234)

	require.Len(t, repo.events, 1)
	ev := repo.events[0]
	assert.Equal(t, domain.EventDislUpdate, ev.EventType)
	assert.Equal(t, "lk", ev.Source)
	assert.Equal(t, domain.TriggerManual, ev.Trigger) // триггер записан
	assert.Equal(t, "User2", ev.Actor)                 // «кто» — из JWT в контексте
	require.NotNil(t, ev.DocTS)                         // doc_ts — самая старая метка
	assert.Equal(t, "2026-07-12T08:10:00", ev.DocTS.String())
	assert.False(t, ev.CreatedAt.IsZero())             // «когда записано» проставлено

	var det struct {
		Files, Count int
		Terminals    []struct {
			Organisation string `json:"organisation"`
			FormationTS  string `json:"formation_ts"`
		}
	}
	require.NoError(t, json.Unmarshal(ev.Detail, &det))
	assert.Equal(t, 2, det.Files)
	assert.Equal(t, 1234, det.Count)
	require.Len(t, det.Terminals, 2)
}

func TestRecordPlanUpload_SourceAndDate(t *testing.T) {
	repo := &fakeJournalRepo{}
	j := service.NewJournal(repo, nil)
	ctx := auth.WithClaims(context.Background(), &auth.Claims{Email: "u@x"}) // нет username → email

	planDate := jlt("2026-07-12T00:00:00")
	j.RecordPlanUpload(ctx, "ma", "ma.xlsx", &planDate, 30, 25, 410)

	require.Len(t, repo.events, 1)
	ev := repo.events[0]
	assert.Equal(t, domain.EventPlanUpload, ev.EventType)
	assert.Equal(t, "plan_ma", ev.Source)             // источник = plan_<код>
	assert.Equal(t, domain.TriggerPlan, ev.Trigger)   // загрузка плана = триггер plan
	assert.Equal(t, "u@x", ev.Actor)
	require.NotNil(t, ev.DocTS)
	assert.Equal(t, "2026-07-12T00:00:00", ev.DocTS.String())
}

func TestDislocationJournal_Mapping(t *testing.T) {
	repo := &fakeJournalRepo{}
	j := service.NewJournal(repo, nil)
	st := service.NewStatusService(j, nil)

	ctxUser := auth.WithClaims(context.Background(), &auth.Claims{Username: "boss"})
	j.RecordDislUpdate(ctxUser, "lk", domain.TriggerManual,
		[]service.LKFileInfo{{FormationTS: jlt("2026-07-13T02:49:00")}}, 4631)
	j.RecordPlanUpload(context.Background(), "ma", "ma.xlsx", nil, 30, 25, 410) // без claims → система

	items, err := st.DislocationJournal(context.Background(), nil, nil, 0)
	require.NoError(t, err)
	require.Len(t, items, 2)

	byType := map[string]service.DislJournalEntry{}
	for _, it := range items {
		byType[it.EventType] = it
	}
	disl := byType[domain.EventDislUpdate]
	assert.Equal(t, "lk", disl.Source)
	assert.Equal(t, domain.TriggerManual, disl.Trigger)
	assert.Equal(t, "user", disl.ActorType) // есть имя → пользователь
	assert.Equal(t, "boss", disl.Actor)
	assert.Equal(t, 4631, disl.Wagons) // ЛК → размер снимка

	plan := byType[domain.EventPlanUpload]
	assert.Equal(t, domain.TriggerPlan, plan.Trigger)
	assert.Equal(t, "system", plan.ActorType) // нет имени → система
	assert.Equal(t, 410, plan.Wagons)         // план → застолблено
}

func TestJournal_NilSafe(t *testing.T) {
	var j *service.Journal // без репозитория/приёмника — no-op, без паники
	assert.NotPanics(t, func() {
		j.RecordDislUpdate(context.Background(), "lk", domain.TriggerManual, nil, 0)
		j.RecordPlanUpload(context.Background(), "ma", "f", nil, 0, 0, 0)
	})
}
