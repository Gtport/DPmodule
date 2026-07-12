package service

import (
	"context"
	"encoding/json"

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

// planJournalDetail — detail события plan_upload.
type planJournalDetail struct {
	Filename string            `json:"filename"`
	PlanCode string            `json:"plan_code"`
	Nitki    int               `json:"nitki"`
	Matched  int               `json:"matched"`
	Stamped  int               `json:"stamped"`
	PlanDate *domain.LocalTime `json:"plan_date,omitempty"`
}

// RecordDislUpdate фиксирует пересборку снимка дислокации. doc_ts — самая старая
// метка формирования среди файлов (худшая свежесть снимка → на неё смотрит гард).
func (j *Journal) RecordDislUpdate(ctx context.Context, source string, files []LKFileInfo, count int) {
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
		EventType: domain.EventDislUpdate, Source: source, DocTS: oldest,
	}, det)
}

// RecordPlanUpload фиксирует загрузку плана подвода. doc_ts — дата плана из документа.
func (j *Journal) RecordPlanUpload(ctx context.Context, planCode, filename string, planDate *domain.LocalTime, nitki, matched, stamped int) {
	if j == nil || j.repo == nil {
		return
	}
	det := planJournalDetail{
		Filename: filename, PlanCode: planCode,
		Nitki: nitki, Matched: matched, Stamped: stamped, PlanDate: planDate,
	}
	j.append(ctx, domain.JournalEvent{
		EventType: domain.EventPlanUpload, Source: "plan_" + planCode, DocTS: planDate,
	}, det)
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
