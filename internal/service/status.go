package service

import (
	"context"
	"encoding/json"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

// StatusService собирает статус-панель (актуальность дислокации и планов подвода)
// из единого журнала событий. Возрасты пересчитываются от clock.Now() (МСК), а не
// берутся из момента записи — панель показывает «сколько прошло сейчас».
type StatusService struct {
	journal *Journal
	dir     *DirectoryCache
}

func NewStatusService(journal *Journal, dir *DirectoryCache) *StatusService {
	return &StatusService{journal: journal, dir: dir}
}

// DislTermStatusDTO — актуальность одной ветки дислокации (файл ЛК грузополучателя),
// аналог d_attis/d_nmtp в gtport.
type DislTermStatusDTO struct {
	Organisation string            `json:"organisation"`
	Terminals    []string          `json:"terminals"`
	FormationTS  *domain.LocalTime `json:"formation_ts"`
	AgeMinutes   int               `json:"age_minutes"`
}

// DislStatusDTO — актуальность снимка дислокации в целом.
type DislStatusDTO struct {
	Source     string              `json:"source"`      // способ обновления (lk/json)
	DocTS      *domain.LocalTime   `json:"doc_ts"`      // общая метка формирования (самая старая)
	UpdatedAt  *domain.LocalTime   `json:"updated_at"`  // когда снимок пересобран
	Actor      string              `json:"actor"`       // кто обновил
	AgeMinutes int                 `json:"age_minutes"` // возраст по doc_ts, мин
	Terminals  []DislTermStatusDTO `json:"terminals"`
}

// PlanStatusDTO — актуальность загрузки плана подвода станции (ma_actual/nk_actual).
type PlanStatusDTO struct {
	PlanCode   string            `json:"plan_code"`
	Loaded     bool              `json:"loaded"`
	DocTS      *domain.LocalTime `json:"doc_ts"`      // дата плана из документа
	UpdatedAt  *domain.LocalTime `json:"updated_at"`  // когда загружен
	Actor      string            `json:"actor"`       // кто загрузил
	Filename   string            `json:"filename"`
	AgeMinutes int               `json:"age_minutes"` // с момента загрузки, мин
}

// StatusDTO — полный статус для панели.
type StatusDTO struct {
	Now         domain.LocalTime `json:"now"`
	Dislocation *DislStatusDTO   `json:"dislocation"` // nil, если снимок ещё не обновлялся
	Plans       []PlanStatusDTO  `json:"plans"`
}

// Status собирает актуальность дислокации и планов из журнала.
func (s *StatusService) Status(ctx context.Context) StatusDTO {
	now := clock.Now()
	out := StatusDTO{Now: now, Plans: []PlanStatusDTO{}}

	if ev, ok := s.journal.LatestDislUpdate(ctx); ok {
		out.Dislocation = dislStatusFrom(ev, now)
	}
	for _, code := range s.dir.PlanCodes() {
		ps := PlanStatusDTO{PlanCode: code}
		if ev, ok := s.journal.LatestPlanUpload(ctx, code); ok {
			ps.Loaded = true
			ps.DocTS = ev.DocTS
			ua := ev.CreatedAt
			ps.UpdatedAt = &ua
			ps.Actor = ev.Actor
			ps.AgeMinutes = minutesSince(ua, now)
			var det planJournalDetail
			if json.Unmarshal(ev.Detail, &det) == nil {
				ps.Filename = det.Filename
			}
		}
		out.Plans = append(out.Plans, ps)
	}
	return out
}

func dislStatusFrom(ev domain.JournalEvent, now domain.LocalTime) *DislStatusDTO {
	ua := ev.CreatedAt
	d := &DislStatusDTO{
		Source: ev.Source, DocTS: ev.DocTS, UpdatedAt: &ua, Actor: ev.Actor,
		Terminals: []DislTermStatusDTO{},
	}
	if ev.DocTS != nil {
		d.AgeMinutes = minutesSince(*ev.DocTS, now)
	}
	var det dislJournalDetail
	if json.Unmarshal(ev.Detail, &det) == nil {
		for _, tm := range det.Terminals {
			ft := tm.FormationTS
			d.Terminals = append(d.Terminals, DislTermStatusDTO{
				Organisation: tm.Organisation, Terminals: tm.Terminals,
				FormationTS: &ft, AgeMinutes: minutesSince(ft, now),
			})
		}
	}
	return d
}

// DislJournalEntry — одна запись журнала обновлений дислокации (обновление снимка).
type DislJournalEntry struct {
	At        domain.LocalTime  `json:"at"`         // когда записано (МСК)
	EventType string            `json:"event_type"` // disl_update | plan_upload (справочно)
	Source    string            `json:"source"`     // lk | json | plan_ma | plan_nk
	Trigger   string            `json:"trigger"`    // manual | scheduled | actualization | plan
	ActorType string            `json:"actor_type"` // system | user
	Actor     string            `json:"actor"`      // имя пользователя (пусто для system)
	DocTS     *domain.LocalTime `json:"doc_ts"`     // метка формирования (ЛК) / дата плана
	Wagons    int               `json:"wagons"`     // затронуто вагонов (снимок для ЛК, застолблено для плана)
}

// DislocationJournal возвращает журнал обновлений дислокации за период [from, to]
// (nil — без границы). Включает и обновления ЛК/JSON, и загрузки планов (они тоже
// перезаписывают снимок). Пусто — если журнал недоступен.
func (s *StatusService) DislocationJournal(ctx context.Context, from, to *domain.LocalTime, limit int) ([]DislJournalEntry, error) {
	events, err := s.journal.SnapshotUpdates(ctx, from, to, limit)
	if err != nil {
		return nil, err
	}
	out := make([]DislJournalEntry, 0, len(events))
	for _, ev := range events {
		out = append(out, dislJournalEntryFrom(ev))
	}
	return out, nil
}

func dislJournalEntryFrom(ev domain.JournalEvent) DislJournalEntry {
	e := DislJournalEntry{
		At: ev.CreatedAt, EventType: ev.EventType, Source: ev.Source,
		Trigger: ev.Trigger, Actor: ev.Actor, DocTS: ev.DocTS,
	}
	// «Кто»: есть имя → пользователь, иначе система (авто/расписание).
	if ev.Actor != "" {
		e.ActorType = "user"
	} else {
		e.ActorType = "system"
	}
	// Триггер старых строк (до колонки trigger) доопределяем по типу события.
	if e.Trigger == "" {
		if ev.EventType == domain.EventPlanUpload {
			e.Trigger = domain.TriggerPlan
		} else {
			e.Trigger = domain.TriggerManual
		}
	}
	// Кол-во вагонов: для обновления ЛК/JSON — размер снимка, для плана — застолблено.
	switch ev.EventType {
	case domain.EventPlanUpload:
		var det planJournalDetail
		if json.Unmarshal(ev.Detail, &det) == nil {
			e.Wagons = det.Stamped
		}
	default:
		var det dislJournalDetail
		if json.Unmarshal(ev.Detail, &det) == nil {
			e.Wagons = det.Count
		}
	}
	return e
}

// minutesSince — целые минуты от t до now (МСК). Нулевое t → 0.
func minutesSince(t, now domain.LocalTime) int {
	if t.IsZero() {
		return 0
	}
	return int(now.Time().Sub(t.Time()).Minutes())
}
