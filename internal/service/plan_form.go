package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
)

// PlanFormService — форма «План подвода» экрана «Рассылка» (перенос gtport
// SmsPlan.GenerateSMSPlan). По терминалу карточка «ЖД сутки»:
//
//	«Вчера» (факт)     — учётный лист грузовой работы за прошлые сутки + ПРИБЫВШИЕ
//	                     поезда вчера («приб», из истории).
//	«Сегодня» (данные) — остаток + прогноз движком над поездами суток (прибывшие
//	                     сегодня + плановые сегодня) + поезда сегодня.
//	Будущие дни        — только поезда из плана подвода (снимок по плановому времени).
//
// Поезда: прибывшие — из vagon_history (по date_prib_d), плановые — из снимка
// дислокации, разложенные по ПЛАНОВОЙ дате прибытия (PlanJd → RaschJd). Формат
// строки и подгрупп — дословный порт gtport (середина индекса, «приб», подгруппы
// «(N) середина SMS от терминал»).
type PlanFormService struct {
	cw      *CargoWorkService
	actual  *ActualCache
	history port.HistoryRepository
	dir     *DirectoryCache
}

func NewPlanFormService(cw *CargoWorkService, actual *ActualCache, history port.HistoryRepository, dir *DirectoryCache) *PlanFormService {
	return &PlanFormService{cw: cw, actual: actual, history: history, dir: dir}
}

// PlanFormLineDTO — линия учёта: факт вчера + прогноз сегодня.
type PlanFormLineDTO struct {
	CargoKey string `json:"cargo_key"`
	Label    string `json:"label"`

	Ost18    int `json:"ost_18"`
	Prib     int `json:"prib"`
	UsefulY  int `json:"useful_y"`
	TotalY   int `json:"total_y"`
	VigrFact int `json:"vigr_fact"`
	OstY     int `json:"ost_y"`

	OstToday      int    `json:"ost_today"`
	UsefulToday   int    `json:"useful_today"`
	TotalToday    int    `json:"total_today"`
	DowntimeToday string `json:"downtime_today"`
}

// PlanFormTrainDTO — поезд формы: готовая строка + обе даты суток.
//
// DateJd — ЖД-сутки (18:00→18:00), по ним строится «Утренняя СМС» (план подвода).
// DateMsk/TimeMsk — грузовые (МСК) сутки: «Оперативная СМС» умеет показывать
// поезда и в этой раскладке (переключатель ЖД/ГР, как в gtport SmsOper).
type PlanFormTrainDTO struct {
	Display string `json:"display"`  // «904 - приб 19:23 (13) 175 ЛК-1 от ГУТ-2»
	DateJd  string `json:"date_jd"`  // yyyy-MM-dd
	DateMsk string `json:"date_msk"` // yyyy-MM-dd
	TimeMsk string `json:"time_msk"` // HH:MM — сортировка в ГР-режиме
}

// PlanFormTerminalDTO — карточка терминала. Trains уже отсортированы по ЖД-суткам
// и позиции внутри них (отсечка 18:00), так что ЖД-раскладка = порядок как есть.
type PlanFormTerminalDTO struct {
	Terminal string             `json:"terminal"`
	Color    string             `json:"color"`
	Lines    []PlanFormLineDTO  `json:"lines"`
	Trains   []PlanFormTrainDTO `json:"trains"`
}

// Form собирает карточки всех терминалов на дату (ЖД-сутки, yyyy-MM-dd).
func (s *PlanFormService) Form(ctx context.Context, date time.Time) ([]PlanFormTerminalDTO, error) {
	targets := terminalTargets(s.dir)
	out := make([]PlanFormTerminalDTO, 0, len(targets))
	for _, t := range targets {
		card, err := s.terminalCard(ctx, date, t)
		if err != nil {
			return nil, err
		}
		out = append(out, card)
	}
	return out, nil
}

func (s *PlanFormService) terminalCard(ctx context.Context, date time.Time, t TargetDTO) (PlanFormTerminalDTO, error) {
	card := PlanFormTerminalDTO{Terminal: t.Name, Color: t.Color}
	start := dayStart(date)
	todayKey := start.Format("2006-01-02")

	yest, err := s.cw.Day(ctx, date.AddDate(0, 0, -1), t.Name)
	if err != nil {
		return card, err
	}
	today, err := s.cw.Day(ctx, date, t.Name)
	if err != nil {
		return card, err
	}
	yestByKey := map[string]CargoWorkLineDTO{}
	for _, l := range yest.Lines {
		yestByKey[l.CargoKey] = l
	}

	// Поезда: прибывшие (история, вчера+сегодня) + плановые (снимок, сегодня+вперёд).
	cutoff := s.cw.CutoffHour()
	arrived, err := s.arrivedTrains(ctx, date, t.Name)
	if err != nil {
		return card, err
	}
	plan := s.planTrains(t.Name, start)

	// Прогноз «сегодня» — движок над поездами сегодня (приб-сегодня + план-сегодня).
	for _, tl := range today.Lines {
		y := yestByKey[tl.CargoKey]
		trains := lineTrains(arrived, tl.CargoKey, todayKey)
		trains = append(trains, lineTrains(plan, tl.CargoKey, todayKey)...)
		a := calcCargoWorkDay(start, tl.Ost18, tl.Pc, cutoff, trains)
		card.Lines = append(card.Lines, PlanFormLineDTO{
			CargoKey: tl.CargoKey, Label: tl.Label,
			Ost18: y.Ost18, Prib: y.Prib, UsefulY: y.UsefulFormation,
			TotalY: y.TotalFormation, VigrFact: y.VigrFact, OstY: y.Ost,
			OstToday: tl.Ost18, UsefulToday: a.UsefulFormation,
			TotalToday: a.TotalFormation, DowntimeToday: a.Downtime,
		})
	}

	card.Trains = buildTrains(append(arrived, plan...), cutoff)
	return card, nil
}

// ── Сбор поездов ─────────────────────────────────────────────────────────────

// pfSub — подгруппа поезда (одно назначение/получатель).
type pfSub struct {
	indexMain, sms1, naznach, gruzpol string
	count                             int
}

// pfTrain — поезд для показа и аналитики.
type pfTrain struct {
	indexPp  string
	arrived  bool
	t        time.Time      // время показа/группировки (ЖД)
	tMsk     time.Time      // то же событие в грузовых (МСК) сутках
	date     string         // yyyy-MM-dd от t (ЖД-сутки)
	subs     []*pfSub       // в порядке появления
	subIdx   map[string]int // ключ подгруппы → индекс в subs
	cargoCnt map[string]int // группа груза → вагонов (для аналитики линии)
}

func (tr *pfTrain) add(indexMain, sms1, naznach, gruzpol, cargo string) {
	k := indexMain + "|" + sms1 + "|" + naznach + "|" + gruzpol
	i, ok := tr.subIdx[k]
	if !ok {
		i = len(tr.subs)
		tr.subIdx[k] = i
		tr.subs = append(tr.subs, &pfSub{indexMain: indexMain, sms1: sms1, naznach: naznach, gruzpol: gruzpol})
	}
	tr.subs[i].count++
	if cargo != "" {
		tr.cargoCnt[cargo]++
	}
}

// arrivedTrains — прибывшие поезда вчера+сегодня из истории (веха прибытия).
func (s *PlanFormService) arrivedTrains(ctx context.Context, date time.Time, terminal string) ([]*pfTrain, error) {
	from := domain.LocalTime(dayStart(date).AddDate(0, 0, -1))
	to := domain.LocalTime(dayStart(date))
	rows, err := s.history.ArrivedRows(ctx, from, to, []string{terminal})
	if err != nil {
		return nil, fmt.Errorf("вехи прибытия: %w", err)
	}
	byKey := map[string]*pfTrain{}
	var order []string
	for _, r := range rows {
		if r.DatePrib == nil || r.DatePrib.IsZero() {
			continue
		}
		tm := r.DatePrib.Time()
		key := r.IndexPp + "|" + tm.Format("2006-01-02")
		tr, ok := byKey[key]
		if !ok {
			tr = newTrain(r.IndexPp, true, tm, time.Time{}) // МСК выводим из ЖД-штампа
			byKey[key] = tr
			order = append(order, key)
		}
		tr.add(r.IndexMain, r.Sms1, r.Naznach, r.GruzpolS, r.CargoGroup)
	}
	return ordered(byKey, order), nil
}

// planTrains — поезда ИЗ ПЛАНА ПОДВОДА из снимка: не на станции назначения, с
// плановыми данными (PlanJd задан — иначе поезд не в плане, не показываем),
// разложены по ПЛАНОВОМУ времени, только с расчётной даты и вперёд.
func (s *PlanFormService) planTrains(terminal string, start time.Time) []*pfTrain {
	if s.actual == nil {
		return nil
	}
	byKey := map[string]*pfTrain{}
	var order []string
	for _, r := range s.actual.All() {
		st := 0
		if r.Status != nil {
			st = *r.Status
		}
		if st == 9 || st >= 10 || r.Naznach != terminal {
			continue
		}
		tm := r.PlanJd // только плановые: без нитки плана поезд в форму не попадает
		if tm == nil || tm.IsZero() {
			continue
		}
		t := tm.Time()
		if t.Before(start) { // раньше расчётных суток — не показываем
			continue
		}
		// Индекс нитки — index_pp (плановая портовая нитка: у с.ф. это «с.ф.СТАНЦИЯ»,
		// а не фактический индексный номер поезда). Fallback — текущий индекс.
		idx := r.IndexPp
		if idx == "" {
			idx = r.Index
		}
		if idx == "" {
			idx = r.IndexMain
		}
		key := idx + "|" + t.Format("2006-01-02")
		tr, ok := byKey[key]
		if !ok {
			var mskT time.Time // плановое время в МСК — как есть из снимка
			if r.PlanMsk != nil && !r.PlanMsk.IsZero() {
				mskT = r.PlanMsk.Time()
			}
			tr = newTrain(idx, false, t, mskT)
			byKey[key] = tr
			order = append(order, key)
		}
		tr.add(r.IndexMain, r.Sms1, r.Naznach, r.GruzpolS, r.CargoGroup)
	}
	return ordered(byKey, order)
}

func newTrain(indexPp string, arrived bool, t, tMsk time.Time) *pfTrain {
	if tMsk.IsZero() {
		tMsk = mskFromJd(t)
	}
	return &pfTrain{
		indexPp: indexPp, arrived: arrived, t: t, tMsk: tMsk, date: t.Format("2006-01-02"),
		subIdx: map[string]int{}, cargoCnt: map[string]int{},
	}
}

// mskFromJd — ЖД-штамп → грузовые (МСК) сутки: час ≥ 18 относится к предыдущим
// календарным суткам (перенос gtport calculateDatesFromHistory).
func mskFromJd(t time.Time) time.Time {
	if t.Hour() >= 18 {
		return t.AddDate(0, 0, -1)
	}
	return t
}

func ordered(m map[string]*pfTrain, order []string) []*pfTrain {
	out := make([]*pfTrain, 0, len(order))
	for _, k := range order {
		out = append(out, m[k])
	}
	return out
}

// lineTrains — поезда линии cargoKey на дату dateKey для движка аналитики
// (Wagons = вагонов этой группы груза; cargoKey пусто — все вагоны поезда).
func lineTrains(trains []*pfTrain, cargoKey, dateKey string) []CargoWorkTrain {
	out := []CargoWorkTrain{}
	for _, tr := range trains {
		if tr.date != dateKey {
			continue
		}
		n := 0
		if cargoKey == "" {
			for _, c := range tr.cargoCnt {
				n += c
			}
		} else {
			n = tr.cargoCnt[cargoKey]
		}
		if n <= 0 {
			continue
		}
		out = append(out, CargoWorkTrain{Name: tr.indexPp, Wagons: n, Arrival: domain.LocalTime(tr.t)})
	}
	return out
}

// buildTrains — поезда одним списком, отсортированные по ЖД-суткам и ПОЗИЦИИ
// ВНУТРИ СУТОК (час отсечки = начало): нитки 18:00–23:59 идут раньше 00:00–17:59,
// а не по сырому времени (та же отсечка, что у движка аналитики). Раскладка по
// дням — на стороне интерфейса (ЖД по date_jd в этом порядке, ГР по date_msk).
func buildTrains(trains []*pfTrain, cutoff int) []PlanFormTrainDTO {
	list := append([]*pfTrain(nil), trains...)
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].date != list[j].date {
			return list[i].date < list[j].date
		}
		return toCargoWorkCalc(list[i].t, cutoff).Before(toCargoWorkCalc(list[j].t, cutoff))
	})

	out := make([]PlanFormTrainDTO, 0, len(list))
	for _, tr := range list {
		out = append(out, PlanFormTrainDTO{
			Display: trainDisplay(tr),
			DateJd:  tr.date,
			DateMsk: tr.tMsk.Format("2006-01-02"),
			TimeMsk: tr.tMsk.Format("15:04"),
		})
	}
	return out
}

// ── Формат строки поезда (дословный порт gtport) ─────────────────────────────

// trainDisplay: «904 - приб 19:23 (13) 175 ЛК-1 от ГУТ-2, (9) 784 Челутай».
func trainDisplay(tr *pfTrain) string {
	parts := []string{fmt.Sprintf("%s -", indexPart(tr.indexPp))}
	if tr.arrived {
		parts = append(parts, "приб")
	}
	parts = append(parts, tr.t.Format("15:04"))

	var subs []string
	for _, sg := range tr.subs {
		if d := subDisplay(sg.indexMain, tr.indexPp, sg.sms1, sg.naznach, sg.gruzpol, sg.count); d != "" {
			subs = append(subs, d)
		}
	}
	if len(subs) > 0 {
		parts = append(parts, strings.Join(subs, ", "))
	}
	return strings.Join(parts, " ")
}

// indexPart — индекс поезда: середина (байты 6–8) если 6-й символ цифра, иначе
// целиком (порт gtport; для «8631-877-9847» даёт «877», для «с.ф.НАХОДКА» — целиком).
func indexPart(indexPp string) string {
	if len(indexPp) >= 8 {
		if c := indexPp[5]; c >= '0' && c <= '9' {
			return indexPp[5:8]
		}
		return indexPp
	}
	if indexPp != "" {
		return indexPp
	}
	return "???"
}

// subDisplay — подгруппа: «(N) середина-индекса SMS от терминал» (порт gtport).
func subDisplay(indexMain, indexPp, sms1, naznach, gruzpol string, count int) string {
	parts := []string{}
	if count > 0 {
		parts = append(parts, fmt.Sprintf("(%d)", count))
	}
	if indexMain != "" && indexMain != indexPp {
		if len(indexMain) >= 8 {
			parts = append(parts, indexMain[5:8])
		} else {
			parts = append(parts, indexMain)
		}
	}
	if sms1 != "" {
		parts = append(parts, sms1)
	}
	if gruzpol != "" && gruzpol != naznach {
		parts = append(parts, "от "+gruzpol)
	}
	if len(parts) <= 1 {
		if count > 0 {
			return fmt.Sprintf("(%d) Основная группа", count)
		}
		return "Основная группа"
	}
	return strings.Join(parts, " ")
}
