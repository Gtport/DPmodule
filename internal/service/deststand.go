package service

import (
	"sort"
	"time"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

// DestStandService — «Долгостой» карточки «Работа»: вагоны, которые прибыли на
// станцию назначения и стоят там дольше порога (client_settings.extra.status →
// dest_stand_hours, дефолт 48 ч).
//
// Охват — все статусы ≥ 10 (решение владельца 11.08.2026): не только гружёный,
// который ждёт выгрузки (10), но и уже выгруженный (12) — вагон могли сломать
// при выгрузке, и он стоит на путях порожним. Оба случая одинаково занимают
// фронт выгрузки, поэтому список один.
//
// ⚠️ Отсчёт строго от DatePrib, а не от простоя РЖД (prost_dn/prost_ch): тот
// считает время с последней операции и обнуляется подачей. На боевом снимке
// 11.08.2026 по «простою» гружёных долгостоев не было НИ ОДНОГО, хотя девять
// вагонов реально стояли больше суток — их пересчитала подача.
//
// DatePrib в снимке — сырой МСК-штамп (ЖД-сдвиг «час ≥ 18 → +сутки»
// накладывается только при записи в vagon_history, см. historyArrival), поэтому
// сравнение с clock.Now() прямое, без пересчёта шкалы.
type DestStandService struct {
	actual *ActualCache
	cfg    *ConfigCache
}

func NewDestStandService(actual *ActualCache, cfg *ConfigCache) *DestStandService {
	return &DestStandService{actual: actual, cfg: cfg}
}

// DestStandVagonDTO — строка списка. Поля совпадают с общей формой списка
// вагонов на фронте (VagonListRow), плюс State — гружёный он или выгруженный:
// для этого списка это главное различие, «стоит под выгрузкой» и «выгружен, но
// не убран» — разные разговоры с терминалом.
type DestStandVagonDTO struct {
	ID          string            `json:"id"`
	Vagon       string            `json:"vagon"`
	Index       string            `json:"index"`
	StationOper string            `json:"station_oper"`
	DorogaOper  string            `json:"doroga_oper"`
	OperS       string            `json:"oper_s"`
	TimeOp      *domain.LocalTime `json:"time_op"`
	Naznach     string            `json:"naznach"`
	StationNach string            `json:"station_nach"`
	Gruzotpr    string            `json:"gruzotpr"`
	CargoS      string            `json:"cargo_s"`
	Ves         *float64          `json:"ves"`
	Status      *int              `json:"status"`
	// State — «гружён» (статус 10) либо «выгружен» (12 и прочие ≥ 10).
	State string `json:"state"`
	// Since — момент прибытия (date_prib), от него и считается стоянка.
	Since *domain.LocalTime `json:"since"`
	// Hours/Days — сколько стоит: часы точные, сутки округлены вниз (колонка «Дней»).
	Hours int `json:"hours"`
	Days  int `json:"days"`
}

// List — вагоны в долгостое, дольше стоящие первыми.
func (s *DestStandService) List() []DestStandVagonDTO {
	return s.list(time.Time(clock.Now()), s.cfg.Settings().Status.DestStandHoursOrDefault())
}

// ThresholdHours — действующий порог (интерфейс подписывает им список).
func (s *DestStandService) ThresholdHours() int {
	return s.cfg.Settings().Status.DestStandHoursOrDefault()
}

func (s *DestStandService) list(now time.Time, hours int) []DestStandVagonDTO {
	rows := s.actual.All()
	out := make([]DestStandVagonDTO, 0, 16)
	for i := range rows {
		r := &rows[i]
		if r.Vagon == "" || r.Status == nil || *r.Status < 10 {
			continue
		}
		if r.DatePrib == nil {
			continue
		}
		prib := time.Time(*r.DatePrib)
		if prib.IsZero() {
			continue
		}
		stood := now.Sub(prib)
		if stood <= time.Duration(hours)*time.Hour {
			continue
		}
		state := "выгружен"
		if *r.Status == 10 {
			state = "гружён"
		}
		out = append(out, DestStandVagonDTO{
			ID: r.ID, Vagon: r.Vagon, Index: r.Index,
			StationOper: r.StationOper, DorogaOper: r.DorogaOper,
			OperS: r.OperS, TimeOp: r.TimeOp, Naznach: r.Naznach,
			StationNach: r.StationNach, Gruzotpr: r.Gruzotpr,
			CargoS: r.CargoS, Ves: r.Ves, Status: r.Status, State: state,
			Since: r.DatePrib,
			Hours: int(stood / time.Hour),
			Days:  int(stood / (24 * time.Hour)),
		})
	}
	// ActualCache.All() отдаёт вагоны в порядке мапы — сортируем для стабильного
	// экрана: дольше стоящие сверху, при равной стоянке по номеру вагона.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Hours != out[j].Hours {
			return out[i].Hours > out[j].Hours
		}
		return out[i].Vagon < out[j].Vagon
	})
	return out
}
