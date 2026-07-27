package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
)

// DelayService — чтение памяти о задержках (vagon_delay): отчёт «Простои за
// период». Только чтение; эпизоды пишет reconcile-слой конвейера (applyDelays).
type DelayService struct {
	repo port.VagonDelayRepository
}

func NewDelayService(repo port.VagonDelayRepository) *DelayService {
	return &DelayService{repo: repo}
}

// Report — отчёт за период дат [from; to] (обе включительно, МСК): эпизоды,
// пересекающиеся с периодом, + агрегат по станциям + итоги. Главная метрика —
// вагоно-часы простоя ЗА ПЕРИОД (hours_in_period): длительность эпизода,
// обрезанная границами периода; открытый эпизод считается до «сейчас».
// Полная длительность эпизода (hours) остаётся в строке как есть.
func (s *DelayService) Report(ctx context.Context, from, to domain.LocalTime) (domain.DelayReport, error) {
	start := dayStartTime(from)
	end := dayStartTime(to).Add(24 * time.Hour) // конец периода — следующая полночь
	rows, err := s.repo.ByPeriod(ctx, domain.LocalTime(start), domain.LocalTime(end))
	if err != nil {
		return domain.DelayReport{}, fmt.Errorf("эпизоды за период: %w", err)
	}

	now := time.Time(clock.Now())
	rep := domain.DelayReport{Records: rows, TotalEpisodes: len(rows)}

	type aggKey struct{ code, name string }
	aggs := map[aggKey]*domain.DelayStationAgg{}
	aggVagons := map[aggKey]map[string]bool{}
	order := []aggKey{}
	vagons := map[string]bool{}

	for i := range rep.Records {
		r := &rep.Records[i]
		if r.DateFrom == nil {
			continue
		}
		// Вагоно-часы за период: пересечение эпизода с [start; end),
		// открытый эпизод — до «сейчас».
		effEnd := now
		if r.DateTo != nil {
			effEnd = time.Time(*r.DateTo)
		}
		s0, e0 := time.Time(*r.DateFrom), effEnd
		if s0.Before(start) {
			s0 = start
		}
		if e0.After(end) {
			e0 = end
		}
		if h := e0.Sub(s0).Hours(); h > 0 {
			r.HoursInPeriod = round1(h)
		}

		rep.TotalHours += r.HoursInPeriod
		vagons[r.Vagon] = true
		if r.DateTo == nil {
			rep.OpenNow++
		}

		k := aggKey{r.StationCode, r.StationName}
		a := aggs[k]
		if a == nil {
			a = &domain.DelayStationAgg{StationCode: r.StationCode, StationName: r.StationName, Doroga: r.Doroga}
			aggs[k] = a
			aggVagons[k] = map[string]bool{}
			order = append(order, k)
		}
		a.Episodes++
		aggVagons[k][r.Vagon] = true
		if r.DateTo == nil {
			a.OpenNow++
		}
		if r.Kind == domain.DelayKindBros {
			a.Hours5 = round1(a.Hours5 + r.HoursInPeriod)
		} else {
			a.Hours4 = round1(a.Hours4 + r.HoursInPeriod)
		}
		a.Hours = round1(a.Hours + r.HoursInPeriod)
	}

	rep.TotalHours = round1(rep.TotalHours)
	rep.TotalVagons = len(vagons)
	for _, k := range order {
		aggs[k].Vagons = len(aggVagons[k])
		rep.Stations = append(rep.Stations, *aggs[k])
	}
	sort.SliceStable(rep.Stations, func(i, j int) bool { // тяжёлые станции сверху
		return rep.Stations[i].Hours > rep.Stations[j].Hours
	})
	sort.SliceStable(rep.Records, func(i, j int) bool { // свежие эпизоды сверху
		ti, tj := rep.Records[i].DateFrom, rep.Records[j].DateFrom
		if ti == nil || tj == nil {
			return tj == nil && ti != nil
		}
		return time.Time(*ti).After(time.Time(*tj))
	})
	return rep, nil
}

// dayStartTime — полночь дня указанного времени.
func dayStartTime(t domain.LocalTime) time.Time {
	tt := time.Time(t)
	return time.Date(tt.Year(), tt.Month(), tt.Day(), 0, 0, 0, 0, time.UTC)
}
