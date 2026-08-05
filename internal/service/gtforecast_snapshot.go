package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/service/unloadsim"
)

// Снапшоты прогноза ГТ (перенос gtport gt_saved_plans) и CSV-аналитика
// «прогноз vs факт». Сохранение НЕ доверяет расчёту фронта: сервер сам
// прогоняет Simulate по присланному входу сеанса и фиксирует свой результат —
// снапшот всегда консистентен движку. Фронт добавляет только журнал правок.

// GtSnapshotSaveRequest — сохранить план: сутки плана + вход сеанса + журнал.
type GtSnapshotSaveRequest struct {
	PlanDate string            `json:"plan_date"` // YYYY-MM-DD
	Request  GtSimulateRequest `json:"request"`
	Journal  json.RawMessage   `json:"journal"`
}

// GtSnapshotMetaDTO — строка списка сохранённых планов.
type GtSnapshotMetaDTO struct {
	PlanDate  string            `json:"plan_date"`
	Station   string            `json:"station"`
	StartDate string            `json:"start_date"`
	DaysCount int               `json:"days_count"`
	SavedBy   string            `json:"saved_by"`
	UpdatedAt *domain.LocalTime `json:"updated_at"`
}

// GtSnapshotDTO — полный сохранённый план (архивный просмотр read-only).
type GtSnapshotDTO struct {
	GtSnapshotMetaDTO
	Request   json.RawMessage `json:"request"`
	Trains    json.RawMessage `json:"trains"`
	Flows     json.RawMessage `json:"flows"`
	FreeSlots json.RawMessage `json:"free_slots"`
	Journal   json.RawMessage `json:"journal"`
}

// SaveSnapshot пересчитывает план по входу сеанса и сохраняет слепок (upsert
// по суткам × станции).
func (s *GtForecastService) SaveSnapshot(ctx context.Context, req GtSnapshotSaveRequest, savedBy string) error {
	planDate, err := time.Parse("2006-01-02", req.PlanDate)
	if err != nil {
		return fmt.Errorf("plan_date: %w", err)
	}
	res, err := s.Simulate(ctx, req.Request)
	if err != nil {
		return fmt.Errorf("пересчёт перед сохранением: %w", err)
	}
	startDate, err := time.Parse("2006-01-02", req.Request.StartDate)
	if err != nil {
		return fmt.Errorf("start_date: %w", err)
	}

	marshal := func(v any) string {
		b, _ := json.Marshal(v)
		return string(b)
	}
	journal := "[]"
	if len(req.Journal) > 0 {
		journal = string(req.Journal)
	}
	now := domain.LocalTime(clock.Now().Time())
	return s.snapshots.Upsert(ctx, domain.GtSnapshot{
		PlanDate:  domain.LocalTime(planDate),
		Station:   req.Request.Station,
		StartDate: domain.LocalTime(startDate),
		DaysCount: req.Request.Days,
		Request:   marshal(req.Request),
		Trains:    marshal(res.Trains),
		Flows:     marshal(res.Flows),
		FreeSlots: marshal(res.FreeSlots),
		Journal:   journal,
		SavedBy:   savedBy,
		CreatedAt: &now, UpdatedAt: &now,
	})
}

// ListSnapshots — сохранённые планы периода (station пустой = все режимы).
func (s *GtForecastService) ListSnapshots(ctx context.Context, from, to time.Time, station string) ([]GtSnapshotMetaDTO, error) {
	snaps, err := s.snapshots.List(ctx, domain.LocalTime(from), domain.LocalTime(to), station)
	if err != nil {
		return nil, err
	}
	out := make([]GtSnapshotMetaDTO, 0, len(snaps))
	for _, sn := range snaps {
		out = append(out, snapshotMeta(sn))
	}
	return out, nil
}

// GetSnapshot — полный план на сутки × станцию; nil = не найден.
func (s *GtForecastService) GetSnapshot(ctx context.Context, planDate time.Time, station string) (*GtSnapshotDTO, error) {
	sn, err := s.snapshots.Get(ctx, domain.LocalTime(planDate), station)
	if err != nil || sn == nil {
		return nil, err
	}
	dto := &GtSnapshotDTO{
		GtSnapshotMetaDTO: snapshotMeta(*sn),
		Request:           rawJSON(sn.Request),
		Trains:            rawJSON(sn.Trains),
		Flows:             rawJSON(sn.Flows),
		FreeSlots:         rawJSON(sn.FreeSlots),
		Journal:           rawJSON(sn.Journal),
	}
	return dto, nil
}

// DeleteSnapshot удаляет сохранённый план.
func (s *GtForecastService) DeleteSnapshot(ctx context.Context, planDate time.Time, station string) error {
	return s.snapshots.Delete(ctx, domain.LocalTime(planDate), station)
}

func snapshotMeta(sn domain.GtSnapshot) GtSnapshotMetaDTO {
	return GtSnapshotMetaDTO{
		PlanDate:  time.Time(sn.PlanDate).Format("2006-01-02"),
		Station:   sn.Station,
		StartDate: time.Time(sn.StartDate).Format("2006-01-02"),
		DaysCount: sn.DaysCount,
		SavedBy:   sn.SavedBy,
		UpdatedAt: sn.UpdatedAt,
	}
}

func rawJSON(s string) json.RawMessage {
	if s == "" {
		return json.RawMessage("null")
	}
	return json.RawMessage(s)
}

// ── CSV-аналитика ────────────────────────────────────────────────────────────

// SnapshotAnalytics — ZIP из трёх CSV по сохранённым планам периода:
// trains.csv (поезда снапшотов + факт прибытия из vagon_history и отклонение
// прогноза от факта в часах расчётной шкалы), gantt_days.csv (сутки диаграмм),
// free_slots.csv. Разделитель «;», UTF-8 BOM — открывается Excel как есть.
func (s *GtForecastService) SnapshotAnalytics(ctx context.Context, from, to time.Time, station string) ([]byte, error) {
	snaps, err := s.snapshots.ListFull(ctx, domain.LocalTime(from), domain.LocalTime(to), station)
	if err != nil {
		return nil, err
	}
	if len(snaps) == 0 {
		return nil, fmt.Errorf("за период %s — %s сохранённых планов нет",
			from.Format("02.01.2006"), to.Format("02.01.2006"))
	}

	// Факт прибытия: вехи истории окна [from−1; to+7] (прогнозы могли уехать
	// вперёд), минимальный date_prib на плановую нитку index_pp.
	fact := map[string]time.Time{}
	rows, err := s.history.ArrivedRows(ctx,
		domain.LocalTime(from.AddDate(0, 0, -1)), domain.LocalTime(to.AddDate(0, 0, 7)), nil)
	if err != nil {
		return nil, fmt.Errorf("факт прибытия: %w", err)
	}
	for _, r := range rows {
		if r.IndexPp == "" || r.DatePrib == nil {
			continue
		}
		t := time.Time(*r.DatePrib)
		if cur, ok := fact[r.IndexPp]; !ok || t.Before(cur) {
			fact[r.IndexPp] = t
		}
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	trainsCSV := [][]string{{
		"план_на", "станция", "поезд", "терминал", "груз", "вагонов", "статус",
		"задержка_ч", "план_жд", "расчёт_жд", "прогноз_жд", "откл_сут",
		"факт_прибытия_жд", "прогноз_vs_факт_ч",
	}}
	ganttCSV := [][]string{{
		"план_на", "станция", "терминал", "груз", "сутки", "план_скорость",
		"норма", "входящие", "прибыло", "полное_образование",
		"полезное_образование", "выгружено", "остаток", "простой_мин",
	}}
	slotsCSV := [][]string{{"план_на", "станция", "нитка_мск", "нитка_жд"}}

	for _, sn := range snaps {
		planDate := time.Time(sn.PlanDate).Format("2006-01-02")

		var trains []GtTrainDTO
		if err := json.Unmarshal([]byte(sn.Trains), &trains); err != nil {
			return nil, fmt.Errorf("снапшот %s/%s: поезда не разбираются: %w", planDate, sn.Station, err)
		}
		for _, t := range trains {
			terminal, cargo := "", ""
			if len(t.SubGroups) > 0 {
				terminal, cargo = t.SubGroups[0].Naznach, t.SubGroups[0].CargoGroup
			}
			factStr, diffStr := "", ""
			if f, ok := fact[t.Index]; ok {
				factStr = f.Format("2006-01-02 15:04")
				if t.ProgJd != nil {
					// Отклонение в расчётной шкале (эталон railwayToCalcTime):
					// внутри одних ЖД-суток разница «в лоб» врала бы на переходе 18:00.
					d := unloadsim.RailwayToCalc(f).Sub(unloadsim.RailwayToCalc(time.Time(*t.ProgJd))).Hours()
					diffStr = strconv.FormatFloat(d, 'f', 1, 64)
				}
			}
			trainsCSV = append(trainsCSV, []string{
				planDate, sn.Station, t.Index, terminal, cargo,
				strconv.Itoa(t.VagonCount), t.Status,
				strconv.FormatFloat(t.DelayHours, 'f', -1, 64),
				fmtLT(t.PlanJd), fmtLT(t.RaschJd), fmtLT(t.ProgJd),
				fmtF(t.Mistake), factStr, diffStr,
			})
		}

		var flows []GtFlowDTO
		if err := json.Unmarshal([]byte(sn.Flows), &flows); err != nil {
			return nil, fmt.Errorf("снапшот %s/%s: диаграммы не разбираются: %w", planDate, sn.Station, err)
		}
		for _, f := range flows {
			for _, d := range f.Days {
				ganttCSV = append(ganttCSV, []string{
					planDate, sn.Station, f.Terminal, f.CargoKey, d.Date,
					strconv.Itoa(d.PlanSpeed), strconv.Itoa(d.NormSpeed),
					strconv.Itoa(d.IncomingTotal), strconv.Itoa(d.Arrival),
					strconv.Itoa(d.TotalFormation), strconv.Itoa(d.UsefulFormation),
					strconv.Itoa(d.Unloaded), strconv.Itoa(d.Remaining),
					strconv.FormatFloat(d.TotalWaitMin, 'f', 0, 64),
				})
			}
		}

		var slots []GtFreeSlotDTO
		if sn.FreeSlots != "" {
			if err := json.Unmarshal([]byte(sn.FreeSlots), &slots); err != nil {
				return nil, fmt.Errorf("снапшот %s/%s: нитки не разбираются: %w", planDate, sn.Station, err)
			}
		}
		for _, sl := range slots {
			slotsCSV = append(slotsCSV, []string{
				planDate, sn.Station,
				time.Time(sl.Msk).Format("2006-01-02 15:04"),
				time.Time(sl.Jd).Format("2006-01-02 15:04"),
			})
		}
	}

	for name, records := range map[string][][]string{
		"trains.csv": trainsCSV, "gantt_days.csv": ganttCSV, "free_slots.csv": slotsCSV,
	} {
		w, err := zw.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil { // BOM для Excel
			return nil, err
		}
		cw := csv.NewWriter(w)
		cw.Comma = ';'
		if err := cw.WriteAll(records); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func fmtLT(lt *domain.LocalTime) string {
	if lt == nil {
		return ""
	}
	return time.Time(*lt).Format("2006-01-02 15:04")
}

func fmtF(f *float64) string {
	if f == nil {
		return ""
	}
	return strconv.FormatFloat(*f, 'f', 2, 64)
}
