package service

import (
	"context"
	"encoding/json"
	"sort"

	"go.uber.org/zap"

	"github.com/Gtport/DPmodule/internal/auth"
	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
)

// Journal — запись единого журнала событий данных (обновления дислокации, загрузки
// планов). Best-effort: сбой записи журнала НЕ роняет основную операцию (снимок уже
// заменён) — только предупреждение в лог. nil-safe: без репозитория/приёмника запись
// пропускается (cmd-утилиты, режим без БД). «Кто» — из JWT в контексте, «когда» —
// из clock.Now() (МСК). doc_ts — время из документа, ставится вызывающим.
type Journal struct {
	repo port.JournalRepository
	log  *zap.Logger
}

func NewJournal(repo port.JournalRepository, log *zap.Logger) *Journal {
	return &Journal{repo: repo, log: log}
}

// dislTermJournal — одна ветка дислокации (файл ЛК одного грузополучателя) в detail.
type dislTermJournal struct {
	Okpo         string           `json:"okpo"`
	Organisation string           `json:"organisation"`
	Terminals    []string         `json:"terminals"`
	FormationTS  domain.LocalTime `json:"formation_ts"`
	AgeMinutes   int              `json:"age_minutes"`
}

// dislJournalDetail — detail события disl_update.
type dislJournalDetail struct {
	Files     int               `json:"files"`
	Count     int               `json:"count"`
	Terminals []dislTermJournal `json:"terminals"`
}

// dislRejectJournalDetail — detail события disl_rejected: какой гард сработал и
// почему (текст с именем некачественного потока), метки формирования по потокам.
type dislRejectJournalDetail struct {
	Guard     string            `json:"guard"`  // skew|no_formation_ts|stale|older|not_newer|process|fetch|parse
	Reason    string            `json:"reason"` // человекочитаемая причина (текст ошибки)
	Terminals []dislTermJournal `json:"terminals,omitempty"`
}

// planOverrideJournal — одно переопределение индекса нитки оператором (с.ф.→реальный
// индекс либо исправление опечатки) для аудита действия пользователя.
type planOverrideJournal struct {
	Ord     int    `json:"ord"`
	IndexPp string `json:"index_pp"`
}

// planJournalDetail — detail события plan_upload.
type planJournalDetail struct {
	Filename  string                `json:"filename"`
	PlanCode  string                `json:"plan_code"`
	Nitki     int                   `json:"nitki"`
	Matched   int                   `json:"matched"`
	Stamped   int                   `json:"stamped"`
	PlanDate  *domain.LocalTime     `json:"plan_date,omitempty"`
	Overrides []planOverrideJournal `json:"overrides,omitempty"` // ручные правки индексов (если были)
}

// RecordDislUpdate фиксирует пересборку снимка дислокации. trigger — что вызвало
// обновление (см. domain.Trigger*). doc_ts — самая старая метка формирования среди
// файлов (худшая свежесть снимка → на неё смотрит гард).
func (j *Journal) RecordDislUpdate(ctx context.Context, source, trigger string, files []LKFileInfo, count int) {
	if j == nil || j.repo == nil {
		return
	}
	det := dislJournalDetail{Files: len(files), Count: count, Terminals: make([]dislTermJournal, 0, len(files))}
	var oldest *domain.LocalTime
	for _, f := range files {
		det.Terminals = append(det.Terminals, dislTermJournal{
			Okpo: f.Okpo, Organisation: f.Organisation, Terminals: f.Terminals,
			FormationTS: f.FormationTS, AgeMinutes: f.AgeMinutes,
		})
		ts := f.FormationTS
		if !ts.IsZero() && (oldest == nil || ts.Time().Before(oldest.Time())) {
			cp := ts
			oldest = &cp
		}
	}
	j.append(ctx, domain.JournalEvent{
		EventType: domain.EventDislUpdate, Source: source, Trigger: trigger, DocTS: oldest,
	}, det)
}

// RecordDislRejected фиксирует ОТКЛОНЁННУЮ попытку обновления дислокации (гард или
// сбой забора/обработки): снимок не тронут, но факт и причина видны в журнале.
// guard — код сработавшей защиты, reason — человекочитаемый текст (какой поток
// некачественный и почему). trigger — кто запускал (manual — кнопка, scheduled — cron).
func (j *Journal) RecordDislRejected(ctx context.Context, source, trigger string, files []LKFileInfo, guard, reason string) {
	if j == nil || j.repo == nil {
		return
	}
	det := dislRejectJournalDetail{Guard: guard, Reason: reason, Terminals: make([]dislTermJournal, 0, len(files))}
	var oldest *domain.LocalTime
	for _, f := range files {
		det.Terminals = append(det.Terminals, dislTermJournal{
			Okpo: f.Okpo, Organisation: f.Organisation, Terminals: f.Terminals,
			FormationTS: f.FormationTS, AgeMinutes: f.AgeMinutes,
		})
		ts := f.FormationTS
		if !ts.IsZero() && (oldest == nil || ts.Time().Before(oldest.Time())) {
			cp := ts
			oldest = &cp
		}
	}
	j.append(ctx, domain.JournalEvent{
		EventType: domain.EventDislRejected, Source: source, Trigger: trigger, DocTS: oldest,
	}, det)
}

// RecordPlanUpload фиксирует загрузку плана подвода. doc_ts — дата плана из документа.
// overrides — ручные переопределения индексов ниток оператором (ord→индекс), могут
// быть nil (одношаговая загрузка/без правок); пишутся в detail как аудит действия.
func (j *Journal) RecordPlanUpload(ctx context.Context, planCode, filename string, planDate *domain.LocalTime, nitki, matched, stamped int, overrides map[int]string) {
	if j == nil || j.repo == nil {
		return
	}
	det := planJournalDetail{
		Filename: filename, PlanCode: planCode,
		Nitki: nitki, Matched: matched, Stamped: stamped, PlanDate: planDate,
		Overrides: overridesForJournal(overrides),
	}
	j.append(ctx, domain.JournalEvent{
		EventType: domain.EventPlanUpload, Source: "plan_" + planCode,
		Trigger: domain.TriggerPlan, DocTS: planDate, // загрузка плана перезаписывает снимок
	}, det)
}

// overridesForJournal переводит карту ord→индекс в детерминированный (по ord) срез
// для detail журнала. nil/пусто → nil (в JSON поле опускается).
func overridesForJournal(overrides map[int]string) []planOverrideJournal {
	if len(overrides) == 0 {
		return nil
	}
	out := make([]planOverrideJournal, 0, len(overrides))
	for ord, idx := range overrides {
		out = append(out, planOverrideJournal{Ord: ord, IndexPp: idx})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ord < out[j].Ord })
	return out
}

// RecordDictReload фиксирует «Обновить справочники»: горячая перезагрузка
// словарей + пересчёт снимка (trigger=actualization — без новых данных потока).
func (j *Journal) RecordDictReload(ctx context.Context, detail any) {
	if j == nil || j.repo == nil {
		return
	}
	now := clock.Now()
	j.append(ctx, domain.JournalEvent{
		EventType: domain.EventDictReload, Source: "directories",
		Trigger: domain.TriggerActualization, DocTS: &now,
	}, detail)
}

// RecordRearrangement фиксирует перестановку/переадресацию: батч-правка снимка
// оператором. source: rearrangement | redirection; count — изменено вагонов.
func (j *Journal) RecordRearrangement(ctx context.Context, source string, count int, extra map[string]any) {
	if j == nil || j.repo == nil {
		return
	}
	now := clock.Now()
	detail := map[string]any{"count": count}
	for k, v := range extra {
		if v != "" && v != nil {
			detail[k] = v
		}
	}
	j.append(ctx, domain.JournalEvent{
		EventType: domain.EventRearrange, Source: source,
		Trigger: domain.TriggerManual, DocTS: &now,
	}, detail)
}

// RecordArrivalsEdit фиксирует операторские действия с прибывшими и кандидатами
// (правки истории прибытий/выгрузок, скрытия кандидатов и бесплановых): action —
// код действия (edit_arrival / unload / set_naznach / dismiss_candidate /
// dismiss_unplanned), count — затронуто вагонов, extra — детали.
func (j *Journal) RecordArrivalsEdit(ctx context.Context, action string, count int, extra map[string]any) {
	if j == nil || j.repo == nil {
		return
	}
	now := clock.Now()
	detail := map[string]any{"count": count}
	for k, v := range extra {
		if v != "" && v != nil {
			detail[k] = v
		}
	}
	j.append(ctx, domain.JournalEvent{
		EventType: domain.EventArrivalsEdit, Source: action,
		Trigger: domain.TriggerManual, DocTS: &now,
	}, detail)
}

// SnapshotUpdates возвращает события перестроения снимка дислокации (обновления
// ЛК/JSON + загрузки планов + отклонённые гардами попытки + перестановки) за
// период [from, to] — источник журнала обновлений дислокации.
func (j *Journal) SnapshotUpdates(ctx context.Context, from, to *domain.LocalTime, limit int) ([]domain.JournalEvent, error) {
	if j == nil || j.repo == nil {
		return nil, nil
	}
	return j.repo.Range(ctx, from, to,
		[]string{domain.EventDislUpdate, domain.EventDislRejected, domain.EventPlanUpload, domain.EventDictReload, domain.EventRearrange, domain.EventArrivalsEdit}, limit)
}

// append дописывает событие: проставляет actor из контекста, created_at из clock.Now(),
// сериализует detail. Ошибку только логирует (best-effort).
func (j *Journal) append(ctx context.Context, ev domain.JournalEvent, detail any) {
	ev.Actor = actorFromContext(ctx)
	ev.CreatedAt = clock.Now()
	if b, err := json.Marshal(detail); err == nil {
		ev.Detail = b
	}
	if err := j.repo.Append(ctx, ev); err != nil && j.log != nil {
		j.log.Warn("journal append failed",
			zap.String("event", ev.EventType), zap.String("source", ev.Source), zap.Error(err))
	}
}

// LastDislDocTS возвращает метку формирования из документа последнего обновления
// дислокации (doc_ts события disl_update) — источник актуальности для гарда загрузки
// плана. ok=false, если журнал недоступен или событий ещё нет (гард тогда пропускает).
func (j *Journal) LastDislDocTS(ctx context.Context) (*domain.LocalTime, bool) {
	if j == nil || j.repo == nil {
		return nil, false
	}
	ev, ok, err := j.repo.LatestByType(ctx, domain.EventDislUpdate)
	if err != nil || !ok || ev.DocTS == nil || ev.DocTS.IsZero() {
		return nil, false
	}
	return ev.DocTS, true
}

// LastDislFormationTS возвращает метки формирования по потокам (organisation →
// formation_ts) из последнего обновления дислокации — для гарда «данные не
// обновились» на пути АСУ. ok=false, если журнала/событий нет или detail не
// разбирается (гард тогда пропускает — не блокируем на неполных данных).
func (j *Journal) LastDislFormationTS(ctx context.Context) (map[string]domain.LocalTime, bool) {
	if j == nil || j.repo == nil {
		return nil, false
	}
	ev, ok, err := j.repo.LatestByType(ctx, domain.EventDislUpdate)
	if err != nil || !ok || len(ev.Detail) == 0 {
		return nil, false
	}
	var det dislJournalDetail
	if err := json.Unmarshal(ev.Detail, &det); err != nil || len(det.Terminals) == 0 {
		return nil, false
	}
	out := make(map[string]domain.LocalTime, len(det.Terminals))
	for _, t := range det.Terminals {
		if !t.FormationTS.IsZero() && t.Organisation != "" {
			out[t.Organisation] = t.FormationTS
		}
	}
	return out, len(out) > 0
}

// LatestDislUpdate возвращает последнее событие обновления дислокации (для панели).
func (j *Journal) LatestDislUpdate(ctx context.Context) (domain.JournalEvent, bool) {
	if j == nil || j.repo == nil {
		return domain.JournalEvent{}, false
	}
	ev, ok, err := j.repo.LatestByType(ctx, domain.EventDislUpdate)
	if err != nil || !ok {
		return domain.JournalEvent{}, false
	}
	return ev, true
}

// LatestPlanUpload возвращает последнюю загрузку плана данного кода (для панели).
func (j *Journal) LatestPlanUpload(ctx context.Context, planCode string) (domain.JournalEvent, bool) {
	if j == nil || j.repo == nil {
		return domain.JournalEvent{}, false
	}
	ev, ok, err := j.repo.LatestBySource(ctx, "plan_"+planCode)
	if err != nil || !ok {
		return domain.JournalEvent{}, false
	}
	return ev, true
}

// actorFromContext извлекает «кто» из проверенного JWT: имя → email → subject.
// Пусто, если контекст без claims (неаутентифицированный путь/cmd-утилиты).
func actorFromContext(ctx context.Context) string {
	c := auth.ClaimsFromContext(ctx)
	if c == nil {
		return ""
	}
	switch {
	case c.Username != "":
		return c.Username
	case c.Email != "":
		return c.Email
	default:
		return c.Subject
	}
}
