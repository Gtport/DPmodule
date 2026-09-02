package service

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

// StatusService собирает статус-панель (актуальность дислокации и планов подвода)
// из единого журнала событий. Возрасты пересчитываются от clock.Now() (МСК), а не
// берутся из момента записи — панель показывает «сколько прошло сейчас».
type StatusService struct {
	journal  *Journal
	dir      *DirectoryCache
	provider *ProviderModeService // nil — источник АСУ выключен, строки на панели нет
}

func NewStatusService(journal *Journal, dir *DirectoryCache) *StatusService {
	return &StatusService{journal: journal, dir: dir}
}

// SetProviderMode подключает режим источника провайдера (asu/lk/paused) к
// статус-панели. Отдельный сеттер: сервис режима существует только при
// включённом источнике data_source id=asu.
func (s *StatusService) SetProviderMode(p *ProviderModeService) { s.provider = p }

// DislTermStatusDTO — актуальность одной ветки дислокации (файл ЛК грузополучателя),
// аналог d_attis/d_nmtp в gtport.
type DislTermStatusDTO struct {
	Organisation string            `json:"organisation"`
	Terminals    []string          `json:"terminals"`
	FormationTS  *domain.LocalTime `json:"formation_ts"`
	AgeMinutes   int               `json:"age_minutes"`
	// Label — готовая подпись ветки для панели: короткое имя организации из
	// реестра ports (org_short, 000060). Источники пишут ветку в журнал
	// по-разному (ЛК/робот — ОКПО и терминалы, АСУ — код клиента провайдера),
	// поэтому подпись сводится к одному виду здесь, при чтении, — старые
	// записи журнала показываются так же, как новые. Пусто — реестр ветку не
	// узнал, фронт подписывает по-старому (терминалы либо organisation).
	Label string `json:"label"`
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

// ProviderStatusDTO — режим источника провайдера rwgate для панели: каким
// шлюзом он добывает данные прямо сейчас. Фронт рисует его парой
// «дислокация-история»: asu → «АСУ-АСУ», lk → «АСУ-ЛК» (см. providermode.go).
type ProviderStatusDTO struct {
	Source    string            `json:"source"`     // asu | lk | paused | unknown
	OK        bool              `json:"ok"`         // ответ провайдера получен и разобран
	CheckedAt *domain.LocalTime `json:"checked_at"` // момент последнего похода (МСК)
}

// StatusDTO — полный статус для панели.
type StatusDTO struct {
	Now         domain.LocalTime   `json:"now"`
	Dislocation *DislStatusDTO     `json:"dislocation"` // nil, если снимок ещё не обновлялся
	Plans       []PlanStatusDTO    `json:"plans"`
	Provider    *ProviderStatusDTO `json:"provider,omitempty"` // nil — источник АСУ выключен
}

// Status собирает актуальность дислокации и планов из журнала.
func (s *StatusService) Status(ctx context.Context) StatusDTO {
	now := clock.Now()
	out := StatusDTO{Now: now, Plans: []PlanStatusDTO{}}

	if ev, ok := s.journal.LatestDislUpdate(ctx); ok {
		out.Dislocation = s.dislStatusFrom(ev, now)
	}
	if s.provider != nil {
		m := s.provider.Mode(ctx) // кэш минуту; протух и провайдер лежит — ждём не дольше 5 с
		ca := m.CheckedAt
		out.Provider = &ProviderStatusDTO{Source: m.Source, OK: m.OK, CheckedAt: &ca}
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

func (s *StatusService) dislStatusFrom(ev domain.JournalEvent, now domain.LocalTime) *DislStatusDTO {
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
				Label: s.termLabel(tm),
			})
		}
	}
	return d
}

// termLabel сводит подпись ветки к короткому имени организации из реестра
// (ports.org_short): ЛК и робот пишут в журнал ОКПО грузополучателя, АСУ —
// код клиента провайдера в поле organisation. Не узнали — пустая строка,
// фронт подпишет по-старому.
func (s *StatusService) termLabel(tm dislTermJournal) string {
	if okpo, err := strconv.ParseInt(strings.TrimSpace(tm.Okpo), 10, 64); err == nil {
		if ports, ok := s.dir.PortsByOkpo(okpo); ok && len(ports) > 0 && ports[0].OrgShort != "" {
			return ports[0].OrgShort
		}
	}
	if p, ok := s.dir.PortByProviderClient(tm.Organisation); ok && p.OrgShort != "" {
		return p.OrgShort
	}
	return ""
}

// DislJournalEntry — одна запись журнала обновлений дислокации (обновление снимка
// либо отклонённая гардом попытка).
type DislJournalEntry struct {
	At        domain.LocalTime  `json:"at"`               // когда записано (МСК)
	EventType string            `json:"event_type"`       // disl_update | disl_rejected | plan_upload
	Source    string            `json:"source"`           // lk | asu | plan_ma | plan_nk
	Trigger   string            `json:"trigger"`          // manual (кнопка) | scheduled (крон) | plan
	ActorType string            `json:"actor_type"`       // system | user
	Actor     string            `json:"actor"`            // имя пользователя (пусто для system)
	DocTS     *domain.LocalTime `json:"doc_ts"`           // метка формирования (ЛК/АСУ) / дата плана
	Wagons    int               `json:"wagons"`           // затронуто вагонов (снимок для ЛК, застолблено для плана)
	Result    string            `json:"result"`           // ok | rejected
	Guard     string            `json:"guard,omitempty"`  // код сработавшей защиты (для rejected)
	Reason    string            `json:"reason,omitempty"` // причина отказа: какой поток некачественный и почему
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
	// Результат и детали: для отклонённых — код гарда и причина (какой поток
	// некачественный); для успешных — кол-во вагонов (снимок для ЛК/АСУ, застолблено
	// для плана).
	e.Result = "ok"
	switch ev.EventType {
	case domain.EventDislRejected:
		e.Result = "rejected"
		var det dislRejectJournalDetail
		if json.Unmarshal(ev.Detail, &det) == nil {
			e.Guard = det.Guard
			e.Reason = det.Reason
		}
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
