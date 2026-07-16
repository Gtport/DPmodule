package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/Gtport/DPmodule/internal/domain"
)

// lt — хелпер: domain.LocalTime из компонентов (московское naive, без TZ).
func lt(h, m int) *domain.LocalTime {
	t := domain.LocalTime(time.Date(2026, 7, 13, h, m, 0, 0, time.UTC))
	return &t
}

func TestCheckSkew(t *testing.T) {
	a := &ASUIngest{log: zap.NewNop()}

	cases := []struct {
		name    string
		pulled  []pulledSource
		limit   int
		wantErr error
	}{
		{
			name:   "guard off (limit 0)",
			pulled: []pulledSource{{label: "asu/attis", ts: lt(10, 0)}, {label: "asu/nmtp", ts: lt(10, 30)}},
			limit:  0,
		},
		{
			name:   "single source — nothing to compare",
			pulled: []pulledSource{{label: "asu/attis", ts: lt(10, 0)}},
			limit:  2,
		},
		{
			name:   "within limit",
			pulled: []pulledSource{{label: "asu/attis", ts: lt(10, 0)}, {label: "asu/nmtp", ts: lt(10, 2)}},
			limit:  2,
		},
		{
			name:    "over limit",
			pulled:  []pulledSource{{label: "asu/attis", ts: lt(10, 0)}, {label: "asu/nmtp", ts: lt(10, 5)}},
			limit:   2,
			wantErr: ErrSourceSkew,
		},
		{
			name:    "over limit regardless of order",
			pulled:  []pulledSource{{label: "asu/nmtp", ts: lt(10, 5)}, {label: "asu/attis", ts: lt(10, 0)}},
			limit:   2,
			wantErr: ErrSourceSkew,
		},
		{
			name:    "missing formation ts blocks",
			pulled:  []pulledSource{{label: "asu/attis", ts: lt(10, 0)}, {label: "asu/nmtp", ts: nil}},
			limit:   2,
			wantErr: ErrNoFormationTS,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := a.checkSkew(tc.pulled, tc.limit)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("ожидали nil, получили %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ожидали %v, получили %v", tc.wantErr, err)
			}
		})
	}
}

// stubJournalRepo — JournalRepository для теста гарда «данные не обновились»:
// отдаёт одно последнее событие disl_update с заданным detail.
type stubJournalRepo struct {
	ev domain.JournalEvent
	ok bool
}

func (s stubJournalRepo) Append(context.Context, domain.JournalEvent) error { return nil }
func (s stubJournalRepo) LatestByType(context.Context, string) (domain.JournalEvent, bool, error) {
	return s.ev, s.ok, nil
}
func (s stubJournalRepo) LatestBySource(context.Context, string) (domain.JournalEvent, bool, error) {
	return domain.JournalEvent{}, false, nil
}
func (s stubJournalRepo) Recent(context.Context, int) ([]domain.JournalEvent, error) {
	return nil, nil
}
func (s stubJournalRepo) Range(context.Context, *domain.LocalTime, *domain.LocalTime, []string, int) ([]domain.JournalEvent, error) {
	return nil, nil
}

// TestCheckNotNewer: каждый уже известный поток обязан принести метку СТРОГО новее
// прошлого обновления; равная/более старая — отказ. Новый поток и отсутствие журнала
// гард пропускают.
func TestCheckNotNewer(t *testing.T) {
	prevDetail := []byte(`{"files":2,"count":10,"terminals":[
		{"organisation":"attis","terminals":["attis"],"formation_ts":"2026-07-13T10:00:00"},
		{"organisation":"nmtp","terminals":["nmtp"],"formation_ts":"2026-07-13T10:01:00"}]}`)
	withPrev := &ASUIngest{log: zap.NewNop(),
		journal: NewJournal(stubJournalRepo{ev: domain.JournalEvent{Detail: prevDetail}, ok: true}, zap.NewNop())}
	noJournal := &ASUIngest{log: zap.NewNop(), journal: nil}

	file := func(org string, ts *domain.LocalTime) LKFileInfo {
		fi := LKFileInfo{Organisation: org, Terminals: []string{org}}
		if ts != nil {
			fi.FormationTS = *ts
		}
		return fi
	}

	cases := []struct {
		name    string
		ingest  *ASUIngest
		files   []LKFileInfo
		wantErr error
	}{
		{
			name:   "оба потока новее — пропуск",
			ingest: withPrev,
			files:  []LKFileInfo{file("attis", lt(11, 0)), file("nmtp", lt(11, 0))},
		},
		{
			name:    "один поток с той же меткой — отказ",
			ingest:  withPrev,
			files:   []LKFileInfo{file("attis", lt(11, 0)), file("nmtp", lt(10, 1))},
			wantErr: ErrSourceNotNewer,
		},
		{
			name:    "оба потока с теми же метками (обрыв РЖД-АСУ) — отказ",
			ingest:  withPrev,
			files:   []LKFileInfo{file("attis", lt(10, 0)), file("nmtp", lt(10, 1))},
			wantErr: ErrSourceNotNewer,
		},
		{
			name:    "поток откатился назад — отказ",
			ingest:  withPrev,
			files:   []LKFileInfo{file("attis", lt(9, 0)), file("nmtp", lt(11, 0))},
			wantErr: ErrSourceNotNewer,
		},
		{
			name:   "новый поток (нет прежней метки) — пропуск",
			ingest: withPrev,
			files:  []LKFileInfo{file("attis", lt(11, 0)), file("newport", lt(10, 0))},
		},
		{
			name:   "журнала нет — пропуск",
			ingest: noJournal,
			files:  []LKFileInfo{file("attis", lt(10, 0))},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.ingest.checkNotNewer(context.Background(), tc.files)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("ожидали nil, получили %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ожидали %v, получили %v", tc.wantErr, err)
			}
		})
	}
}
