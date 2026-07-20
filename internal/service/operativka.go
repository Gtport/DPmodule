package service

import (
	"context"
	"sort"
	"time"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
)

// OperativkaService — карточка «Оперативка» домашней страницы: суточные счётчики
// по терминалам (реестр ports, не хардкод) — прибыло/выгружено за вчерашние и
// текущие ЖД-сутки (вехи vagon_history: date_prib_d × naznach, date_vigr_d ×
// place_vigr) плюс «не выгружено» — вагоны статуса 10 в текущем снимке.
type OperativkaService struct {
	repo      port.HistoryRepository
	actual    *ActualCache
	dir       *DirectoryCache
	unplanned port.UnplannedMoveRepository // «бесплановые в подходе» (nil — секции нет)
	journal   *Journal                     // единый журнал (может быть nil)
}

// SetJournal подключает журнал событий (nil-safe).
func (s *OperativkaService) SetJournal(j *Journal) { s.journal = j }

func NewOperativkaService(repo port.HistoryRepository, actual *ActualCache, dir *DirectoryCache, unplanned port.UnplannedMoveRepository) *OperativkaService {
	return &OperativkaService{repo: repo, actual: actual, dir: dir, unplanned: unplanned}
}

// OperativkaRowDTO — строка карточки: терминал и его суточные счётчики.
type OperativkaRowDTO struct {
	Terminal      string `json:"terminal"`
	Station       string `json:"station"`
	StationCode   string `json:"station_code"`
	PribYesterday int    `json:"prib_yesterday"`
	VigrYesterday int    `json:"vigr_yesterday"`
	PribToday     int    `json:"prib_today"`
	VigrToday     int    `json:"vigr_today"`
	NotUnloaded   int    `json:"not_unloaded"` // сейчас в статусе 10 (прибыл, не выгружен)
}

// UnplannedTrainDTO — поезд из «бесплановых в подходе»: агрегация записей
// unplanned_move по индексу (сигнал «движется без плана ближе порога»).
type UnplannedTrainDTO struct {
	Index       string   `json:"index"`
	StationOper string   `json:"station_oper"` // последняя известная станция
	Rasst       *int     `json:"rasst"`
	VagonCount  int      `json:"vagon_count"`
	Sostav      []string `json:"sostav"` // display-строки подгрупп (как в «Ближайших»)
	Vagons      []string `json:"vagons"` // номера вагонов (для «Скрыть»)
}

// OperativkaDTO — ответ ручки: даты суток, строки по терминалам и бесплановые.
type OperativkaDTO struct {
	Yesterday string              `json:"yesterday"` // yyyy-MM-dd (ЖД-сутки)
	Today     string              `json:"today"`
	Rows      []OperativkaRowDTO  `json:"rows"`
	Unplanned []UnplannedTrainDTO `json:"unplanned"` // «бесплановые в подходе» (до «Скрыть»)
}

// Snapshot — счётчики за вчера/сегодня (ЖД-сутки от МСК-«сейчас») по всем
// включённым терминалам реестра; порядок — станции по коду по убыванию (как
// колонки домашней страницы: Мыс, Находка), терминалы по имени.
func (s *OperativkaService) Snapshot(ctx context.Context) (OperativkaDTO, error) {
	today := clock.Now().Time().Truncate(24 * time.Hour)
	yesterday := today.AddDate(0, 0, -1)
	todayS, yestS := today.Format("2006-01-02"), yesterday.Format("2006-01-02")

	prib, vigr, err := s.repo.DailyTerminalCounts(ctx,
		domain.LocalTime(yesterday), domain.LocalTime(today))
	if err != nil {
		return OperativkaDTO{}, err
	}

	// «Не выгружено» — статус 10 по терминалам из RAM-снимка.
	notUnloaded := map[string]int{}
	for _, r := range s.actual.All() {
		if r.Status != nil && *r.Status == 10 && r.Naznach != "" {
			notUnloaded[r.Naznach]++
		}
	}

	targets := terminalTargets(s.dir)
	sort.SliceStable(targets, func(i, j int) bool {
		if targets[i].StationCode != targets[j].StationCode {
			return targets[i].StationCode > targets[j].StationCode // Мыс (9857) раньше Находки (9847)
		}
		return targets[i].Name < targets[j].Name
	})

	rows := make([]OperativkaRowDTO, 0, len(targets))
	for _, t := range targets {
		rows = append(rows, OperativkaRowDTO{
			Terminal: t.Name, Station: t.Station, StationCode: t.StationCode,
			PribYesterday: prib[yestS+"|"+t.Name],
			VigrYesterday: vigr[yestS+"|"+t.Name],
			PribToday:     prib[todayS+"|"+t.Name],
			VigrToday:     vigr[todayS+"|"+t.Name],
			NotUnloaded:   notUnloaded[t.Name],
		})
	}
	unplanned, err := s.unplannedTrains(ctx)
	if err != nil {
		return OperativkaDTO{}, err
	}
	return OperativkaDTO{Yesterday: yestS, Today: todayS, Rows: rows, Unplanned: unplanned}, nil
}

// unplannedTrains — агрегация «бесплановых в подходе» по индексу поезда:
// кол-во, состав (display подгрупп по index_main/naznach/gruzpol_s, как в
// «Ближайших») и номера вагонов для «Скрыть».
func (s *OperativkaService) unplannedTrains(ctx context.Context) ([]UnplannedTrainDTO, error) {
	out := []UnplannedTrainDTO{} // не nil — фронт ждёт массив
	if s.unplanned == nil {
		return out, nil
	}
	rows, err := s.unplanned.LoadAll(ctx)
	if err != nil {
		return nil, err
	}

	type subKey struct{ im, nz, gp string }
	type train struct {
		dto      *UnplannedTrainDTO
		subs     map[subKey]*ArrivalSubgroupDTO
		subOrder []subKey
	}
	var order []string
	trains := map[string]*train{}
	for i := range rows {
		r := rows[i]
		key := r.Index
		if key == "" {
			key = r.IndexMain
		}
		t, ok := trains[key]
		if !ok {
			t = &train{dto: &UnplannedTrainDTO{Index: key}, subs: map[subKey]*ArrivalSubgroupDTO{}}
			trains[key] = t
			order = append(order, key)
		}
		t.dto.VagonCount++
		t.dto.Vagons = append(t.dto.Vagons, r.Vagon)
		// Последняя известная позиция (записи одного поезда обновляются вместе —
		// берём любую свежую).
		if t.dto.StationOper == "" || !r.UpdatedAt.IsZero() {
			t.dto.StationOper = r.StationOper
			t.dto.Rasst = r.RasstStanNazn
		}
		sk := subKey{r.IndexMain, r.Naznach, r.GruzpolS}
		sg, ok := t.subs[sk]
		if !ok {
			sg = &ArrivalSubgroupDTO{
				IndexMain: r.IndexMain, StationNach: r.StationNach,
				Naznach: r.Naznach, GruzpolS: r.GruzpolS,
			}
			t.subs[sk] = sg
			t.subOrder = append(t.subOrder, sk)
		}
		sg.VagonCount++
	}

	for _, key := range order {
		t := trains[key]
		for _, sk := range t.subOrder {
			sg := t.subs[sk]
			t.dto.Sostav = append(t.dto.Sostav, arrivalDisplay(sg))
		}
		out = append(out, *t.dto)
	}
	return out, nil
}

// DismissUnplanned — «Скрыть» (указание оператора): удаление записей по вагонам.
func (s *OperativkaService) DismissUnplanned(ctx context.Context, vagons []string) (int, error) {
	if s.unplanned == nil || len(vagons) == 0 {
		return 0, nil
	}
	// Индексы скрываемых поездов — в журнал (до удаления записей).
	var idxs []string
	if s.journal != nil {
		if rows, lerr := s.unplanned.LoadAll(ctx); lerr == nil {
			want := map[string]struct{}{}
			for _, v := range vagons {
				want[v] = struct{}{}
			}
			seen := map[string]struct{}{}
			for i := range rows {
				if _, ok := want[rows[i].Vagon]; !ok {
					continue
				}
				idx := rows[i].Index
				if idx == "" {
					idx = rows[i].IndexMain
				}
				if _, dup := seen[idx]; idx != "" && !dup {
					seen[idx] = struct{}{}
					idxs = append(idxs, idx)
				}
			}
		}
	}
	n, err := s.unplanned.DeleteByVagons(ctx, vagons)
	if err != nil {
		return 0, err
	}
	if s.journal != nil {
		s.journal.RecordArrivalsEdit(ctx, "dismiss_unplanned", n,
			map[string]any{"selected": len(vagons), "trains": idxs})
	}
	return n, nil
}
