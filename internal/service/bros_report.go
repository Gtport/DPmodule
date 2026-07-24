package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

// Report — отчёт по броскам за период с разбивкой суток простоя по типам причин.
//
// Для каждого броска идём по дням от даты бросания до фактического подъёма (или
// сегодня) и на каждый день берём «эффективную» запись журнала: точное совпадение
// по дате → ближайшая предыдущая → код из снимка bros (перенос логики gtport
// BrosReport «последней известной записи»). День классифицируется по коду:
//
//	05 — платное размещение; 01 согл./несогл. — по is_agreed; иначе прочие.
func (s *BrosService) Report(ctx context.Context, f domain.BrosFilter) ([]domain.BrosReportRow, error) {
	records, _, err := s.repo.Filter(ctx, f)
	if err != nil {
		return nil, fmt.Errorf("отбор бросков: %w", err)
	}

	ids := make([]string, 0, len(records))
	for i := range records {
		ids = append(ids, records[i].ID)
	}
	entries, err := s.journal.ByBrosIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("журнал бросков: %w", err)
	}
	byBros := make(map[string][]domain.BrosJournalEntry, len(records))
	for _, e := range entries {
		byBros[e.BrosID] = append(byBros[e.BrosID], e) // уже отсортированы bros_id, date ASC
	}

	now := time.Time(clock.Now())
	today := dayFloor(now)

	out := make([]domain.BrosReportRow, 0, len(records))
	for i := range records {
		r := records[i]
		row := domain.BrosReportRow{Bros: r}
		if from, ok := brosDayOf(r.DateBr); ok {
			to := today
			if d, ok2 := brosDayOf(r.DatePodFact); ok2 {
				to = d
			}
			brosCountDays(&row, from, to, byBros[r.ID], r.Reason)
		}
		out = append(out, row)
	}
	return out, nil
}

// brosCountDays заполняет разбивку суток строки отчёта.
func brosCountDays(row *domain.BrosReportRow, from, to time.Time, entries []domain.BrosJournalEntry, fallback string) {
	if !to.After(from) {
		return
	}
	for cursor := from; cursor.Before(to); cursor = cursor.AddDate(0, 0, 1) {
		row.DaysTotal++
		reason, isAgreed, found := brosEffectiveEntry(cursor, entries, fallback)
		switch brosClassify(reason, isAgreed, found) {
		case brosCatCode05:
			row.DaysCode05++
		case brosCatCode01Agreed:
			row.DaysCode01Agreed++
		case brosCatCode01NotAgreed:
			row.DaysCode01NotAgreed++
		default:
			row.DaysOther++
		}
	}
}

// brosEffectiveEntry — актуальная на день классификация: точное совпадение по дате
// → ближайшая предыдущая запись → код из снимка bros (синтетически, is_agreed nil).
func brosEffectiveEntry(day time.Time, entries []domain.BrosJournalEntry, fallback string) (reason string, isAgreed *bool, found bool) {
	dayStr := day.Format("2006-01-02")
	var last *domain.BrosJournalEntry
	for i := range entries {
		ed, ok := brosDayOf(entries[i].Date)
		if !ok {
			continue
		}
		eStr := ed.Format("2006-01-02")
		if eStr == dayStr {
			return entries[i].Reason, entries[i].IsAgreed, true
		}
		if eStr > dayStr {
			break // записи отсортированы по возрастанию — дальше только позже
		}
		last = &entries[i]
	}
	if last != nil {
		return last.Reason, last.IsAgreed, true
	}
	if fallback != "" {
		return fallback, nil, true
	}
	return "", nil, false
}

const (
	brosCatOther = iota
	brosCatCode05
	brosCatCode01Agreed
	brosCatCode01NotAgreed
)

// brosClassify относит код+согласованность к категории суток.
func brosClassify(reason string, isAgreed *bool, found bool) int {
	if !found {
		return brosCatOther
	}
	switch reason {
	case "05", "5":
		return brosCatCode05
	case "01", "1":
		if isAgreed != nil && *isAgreed {
			return brosCatCode01Agreed
		}
		if isAgreed != nil && !*isAgreed {
			return brosCatCode01NotAgreed
		}
		return brosCatOther
	}
	return brosCatOther
}

// dayFloor — полночь той же даты (UTC-якорь, время naive).
func dayFloor(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// brosDayOf — полночь даты LocalTime; false для nil/нулевой.
func brosDayOf(t *domain.LocalTime) (time.Time, bool) {
	if t == nil {
		return time.Time{}, false
	}
	tt := time.Time(*t)
	if tt.IsZero() {
		return time.Time{}, false
	}
	return dayFloor(tt), true
}
