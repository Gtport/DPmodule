package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
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
	// Паспорт: вид (manual|auto) и момент расчёта (миграция 000064).
	Kind       string            `json:"kind"`
	ComputedAt *domain.LocalTime `json:"computed_at"`
	UpdatedAt  *domain.LocalTime `json:"updated_at"`
}

// GtSnapshotDTO — полный сохранённый план (архивный просмотр read-only).
type GtSnapshotDTO struct {
	GtSnapshotMetaDTO
	Request   json.RawMessage `json:"request"`
	Trains    json.RawMessage `json:"trains"`
	Flows     json.RawMessage `json:"flows"`
	FreeSlots json.RawMessage `json:"free_slots"`
	Journal   json.RawMessage `json:"journal"`
	Meta      json.RawMessage `json:"meta"`
}

// GtSnapshotPassport — паспорт расчёта (jsonb meta): чем считали. Скорости
// линий — из первых суток диаграмм, то есть те, что движок реально подставил
// (с учётом тумблера нормы и правок скоростей сеанса).
type GtSnapshotPassport struct {
	Engine     string                   `json:"engine"`
	Days       int                      `json:"days"`
	UseNorm    bool                     `json:"use_norm"`
	CutoffHour int                      `json:"cutoff_hour"`
	Overrides  int                      `json:"overrides"` // what-if правок в сеансе
	Trains     int                      `json:"trains"`    // поездов в очереди
	Lines      []GtSnapshotLinePassport `json:"lines"`
}

// GtSnapshotLinePassport — скорости одной линии выгрузки на день расчёта.
type GtSnapshotLinePassport struct {
	Terminal  string `json:"terminal"`
	CargoKey  string `json:"cargo_key"`
	PlanSpeed int    `json:"plan_speed"`
	NormSpeed int    `json:"norm_speed"`
}

// SaveSnapshot пересчитывает план по входу сеанса и сохраняет слепок (upsert
// по суткам × станции), kind=manual.
func (s *GtForecastService) SaveSnapshot(ctx context.Context, req GtSnapshotSaveRequest, savedBy string) error {
	planDate, err := time.Parse("2006-01-02", req.PlanDate)
	if err != nil {
		return fmt.Errorf("plan_date: %w", err)
	}
	journal := "[]"
	if len(req.Journal) > 0 {
		journal = string(req.Journal)
	}
	return s.saveSnapshot(ctx, planDate, req.Request, journal, savedBy, domain.GtSnapshotManual)
}

// saveSnapshot — общий путь ручного и автоматического сохранения: пересчёт по
// входу, паспорт расчёта, upsert по (сутки, станция).
func (s *GtForecastService) saveSnapshot(ctx context.Context, planDate time.Time, req GtSimulateRequest, journal, savedBy, kind string) error {
	res, err := s.Simulate(ctx, req)
	if err != nil {
		return fmt.Errorf("пересчёт перед сохранением: %w", err)
	}
	startDate, err := time.Parse("2006-01-02", req.StartDate)
	if err != nil {
		return fmt.Errorf("start_date: %w", err)
	}

	marshal := func(v any) string {
		b, _ := json.Marshal(v)
		return string(b)
	}
	now := domain.LocalTime(clock.Now().Time())
	return s.snapshots.Upsert(ctx, domain.GtSnapshot{
		PlanDate:   domain.LocalTime(planDate),
		Station:    req.Station,
		StartDate:  domain.LocalTime(startDate),
		DaysCount:  req.Days,
		Request:    marshal(req),
		Trains:     marshal(res.Trains),
		Flows:      marshal(res.Flows),
		FreeSlots:  marshal(res.FreeSlots),
		Journal:    journal,
		SavedBy:    savedBy,
		ComputedAt: &now,
		Kind:       kind,
		Meta:       marshal(s.snapshotPassport(req, res)),
		CreatedAt:  &now, UpdatedAt: &now,
	})
}

// snapshotPassport собирает паспорт расчёта из входа и результата симуляции.
func (s *GtForecastService) snapshotPassport(req GtSimulateRequest, res GtSimulateDTO) GtSnapshotPassport {
	p := GtSnapshotPassport{
		Engine: "unloadsim", Days: req.Days, UseNorm: req.UseNorm,
		CutoffHour: s.cutoffHour(), Overrides: len(req.Overrides), Trains: len(res.Trains),
		Lines: []GtSnapshotLinePassport{},
	}
	for _, f := range res.Flows {
		l := GtSnapshotLinePassport{Terminal: f.Terminal, CargoKey: f.CargoKey}
		if len(f.Days) > 0 {
			l.PlanSpeed, l.NormSpeed = f.Days[0].PlanSpeed, f.Days[0].NormSpeed
		}
		p.Lines = append(p.Lines, l)
	}
	return p
}

// cutoffHour — час начала ЖД-суток из настроек источника дислокации (как в
// грузовой работе); без настройки — 18.
func (s *GtForecastService) cutoffHour() int {
	if s.cfg == nil {
		return 18
	}
	if ds, ok := s.cfg.DataSource("lk"); ok && ds.Config.DateCutoffHour > 0 && ds.Config.DateCutoffHour < 24 {
		return ds.Config.DateCutoffHour
	}
	return 18
}

// AutoSnapshot — ежедневный автоснапшот по всем причальным станциям (крон
// gt_snapshot): расчёт на текущие ЖД-сутки, без правок, kind=auto. Ручной
// снапшот тех же суток не трогаем (он богаче: журнал сеанса); свой авто-снапшот
// перезаписываем — повтор в те же сутки идемпотентен. Ошибка одной станции не
// останавливает остальные: собираем и возвращаем вместе.
func (s *GtForecastService) AutoSnapshot(ctx context.Context, days int) error {
	dto, err := s.Context(ctx)
	if err != nil {
		return fmt.Errorf("режимы вкладки: %w", err)
	}
	planDate := gtAutoPlanDate(clock.Now().Time())
	var errs []string
	for _, st := range dto.Stations {
		existing, err := s.snapshots.Get(ctx, domain.LocalTime(planDate), st.Code)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: чтение снапшота: %v", st.Code, err))
			continue
		}
		if !gtAutoShouldSave(existing) {
			continue
		}
		req := GtSimulateRequest{Station: st.Code, StartDate: planDate.Format("2006-01-02"), Days: days}
		if err := s.saveSnapshot(ctx, planDate, req, "[]", "cron", domain.GtSnapshotAuto); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", st.Code, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("автоснапшот: %s", strings.Join(errs, "; "))
	}
	return nil
}

// gtAutoPlanDate — сутки автоснапшота: текущие ЖД-сутки (час ≥ 18 → завтра),
// без времени.
func gtAutoPlanDate(now time.Time) time.Time {
	d := jd18(now)
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
}

// gtAutoShouldSave — крон не перезаписывает ручной снапшот; пустой kind у
// снапшотов до миграции 000064 читается как ручной.
func gtAutoShouldSave(existing *domain.GtSnapshot) bool {
	return existing == nil || existing.Kind == domain.GtSnapshotAuto
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
		Meta:              rawJSON(sn.Meta),
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
		DaysCount:  sn.DaysCount,
		SavedBy:    sn.SavedBy,
		Kind:       sn.Kind,
		ComputedAt: sn.ComputedAt,
		UpdatedAt:  sn.UpdatedAt,
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

	// trains.csv — строка на ПОДГРУППУ поезда (станция погрузки × адрес × груз),
	// поля поезда повторяются (эталон экспорта gtport); аналитика группирует по
	// «план_на + поезд». Дислокация и паспорт — на момент расчёта
	// (docs/ANALYTICS.md §7.1).
	trainsCSV := [][]string{{
		"план_на", "вид", "время_расчёта", "станция", "поезд", "терминал", "адрес", "груз",
		"вагонов_поезд", "вагонов_группа", "станция_погрузки", "дата_погрузки",
		"грузоотправитель", "клиент", "статус", "станция_операции", "дорога",
		"операция", "время_операции", "стоит_ч", "км_до_назначения", "часов_хода",
		"задержка_ч", "план_жд", "расчёт_жд", "прогноз_жд", "откл_сут",
		"факт_прибытия_жд", "прогноз_vs_факт_ч",
		"бросок_дата", "бросок_станция", "бросок_дорога", "бросок_код", "ответственность", "подъём_план",
	}}
	ganttCSV := [][]string{{
		"план_на", "вид", "станция", "терминал", "груз", "сутки", "план_скорость",
		"норма", "входящие", "прибыло", "полное_образование",
		"полезное_образование", "выгружено", "остаток", "простой_мин",
		"ожиданий", "перенесено_поездов", "перегрузка",
	}}
	slotsCSV := [][]string{{"план_на", "станция", "нитка_мск", "нитка_жд"}}
	metaCSV := [][]string{{"план_на", "станция", "вид", "время_расчёта", "сохранил", "старт", "горизонт", "паспорт"}}

	for _, sn := range snaps {
		planDate := time.Time(sn.PlanDate).Format("2006-01-02")
		computedAt := fmtLT(sn.ComputedAt)
		metaCSV = append(metaCSV, []string{
			planDate, sn.Station, sn.Kind, computedAt, sn.SavedBy,
			time.Time(sn.StartDate).Format("2006-01-02"), strconv.Itoa(sn.DaysCount), sn.Meta,
		})

		var trains []GtTrainDTO
		if err := json.Unmarshal([]byte(sn.Trains), &trains); err != nil {
			return nil, fmt.Errorf("снапшот %s/%s: поезда не разбираются: %w", planDate, sn.Station, err)
		}
		for _, t := range trains {
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
			bros := [6]string{}
			if b := t.Bros; b != nil {
				bros = [6]string{fmtDate(b.DateBr), b.StationBr, b.DorogaBr, b.Reason, b.Responsibility, fmtDate(b.DatePod)}
			}
			subs := t.SubGroups
			if len(subs) == 0 {
				subs = []GtSubGroupDTO{{}}
			}
			for _, sg := range subs {
				trainsCSV = append(trainsCSV, []string{
					planDate, sn.Kind, computedAt, sn.Station, t.Index, sg.Naznach, sg.GruzpolS, sg.CargoGroup,
					strconv.Itoa(t.VagonCount), strconv.Itoa(sg.VagonCount), sg.StationNach, fmtLT(sg.DateNach),
					sg.Gruzotpr, sg.Client, t.Status, t.StationOper, t.DorogaOper,
					t.Oper, fmtLT(t.TimeOp), fmtF1(t.IdleHours), fmtInt(t.RasstStanNazn), fmtF1(t.ToGo),
					strconv.FormatFloat(t.DelayHours, 'f', -1, 64),
					fmtLT(t.PlanJd), fmtLT(t.RaschJd), fmtLT(t.ProgJd),
					fmtF(t.Mistake), factStr, diffStr,
					bros[0], bros[1], bros[2], bros[3], bros[4], bros[5],
				})
			}
		}

		var flows []GtFlowDTO
		if err := json.Unmarshal([]byte(sn.Flows), &flows); err != nil {
			return nil, fmt.Errorf("снапшот %s/%s: диаграммы не разбираются: %w", planDate, sn.Station, err)
		}
		for _, f := range flows {
			for _, d := range f.Days {
				// «ожиданий» — операций-простоев линии за сутки (терминал ждал
				// поезд); «перегрузка» — образование к плановой скорости (>1 —
				// остаток растёт); эталон gt_export_handler gtport.
				waits := 0
				for _, op := range d.Operations {
					if !op.IsRemainder && op.WaitMin > 0 {
						waits++
					}
				}
				overload := ""
				if d.PlanSpeed > 0 {
					overload = strconv.FormatFloat(float64(d.TotalFormation)/float64(d.PlanSpeed), 'f', 2, 64)
				}
				ganttCSV = append(ganttCSV, []string{
					planDate, sn.Kind, sn.Station, f.Terminal, f.CargoKey, d.Date,
					strconv.Itoa(d.PlanSpeed), strconv.Itoa(d.NormSpeed),
					strconv.Itoa(d.IncomingTotal), strconv.Itoa(d.Arrival),
					strconv.Itoa(d.TotalFormation), strconv.Itoa(d.UsefulFormation),
					strconv.Itoa(d.Unloaded), strconv.Itoa(d.Remaining),
					strconv.FormatFloat(d.TotalWaitMin, 'f', 0, 64),
					strconv.Itoa(waits), strconv.Itoa(len(d.CarriedOver)), overload,
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
		"meta.csv": metaCSV,
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

func fmtDate(lt *domain.LocalTime) string {
	if lt == nil {
		return ""
	}
	return time.Time(*lt).Format("2006-01-02")
}

func fmtF1(f *float64) string {
	if f == nil {
		return ""
	}
	return strconv.FormatFloat(*f, 'f', 1, 64)
}

func fmtInt(i *int) string {
	if i == nil {
		return ""
	}
	return strconv.Itoa(*i)
}
