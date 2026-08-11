package service

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/Gtport/DPmodule/internal/auth"
	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
)

// NotificationService — внутренние уведомления (перенос колокольчика gtport).
// Создание — из конвейера дислокации и системных сторожей (типизированные
// эмиттеры), чтение/отметки — ручками API. Fan-out на чтении: уведомление несёт
// аудиторию, видимость считает репозиторий по AudiencesFor(claims).
//
// Nil-safe и best-effort: сервис не собран (nil) либо запись не удалась —
// конвейер дислокации НЕ падает, ошибка только в лог (уведомления — сигнал
// диспетчеру, не данные).
type NotificationService struct {
	repo      port.NotificationRepository
	retention time.Duration // срок хранения (notifications.retention_hours)
	stale     time.Duration // порог сторожа устаревания снимка (notifications.stale_after; ≤0 — выключен)
	journal   *Journal      // для сторожа устаревания (nil — сторож молчит)
	log       *zap.Logger
}

func NewNotificationService(repo port.NotificationRepository, retention, staleAfter time.Duration, log *zap.Logger) *NotificationService {
	return &NotificationService{repo: repo, retention: retention, stale: staleAfter, log: log}
}

// SetJournal подключает журнал событий для сторожа устаревания снимка
// (nil-safe: без журнала сторож молчит).
func (s *NotificationService) SetJournal(j *Journal) { s.journal = j }

// AudiencesFor — аудитории уведомлений, видимые пользователю с такими claims.
// Соответствие наборам доступа — единственное место этой привязки: oper →
// AccessWrite, dicts → AccessDicts, admin → AccessAdmin. nil-claims (Keycloak
// выключен) → только all (fail closed, как Claims.Allows).
func AudiencesFor(c *auth.Claims) []string {
	out := []string{domain.AudienceAll}
	if c.Allows(auth.AccessWrite) {
		out = append(out, domain.AudienceOper)
	}
	if c.Allows(auth.AccessDicts) {
		out = append(out, domain.AudienceDicts)
	}
	if c.Allows(auth.AccessAdmin) {
		out = append(out, domain.AudienceAdmin)
	}
	return out
}

// Notify — создать уведомление (best-effort). Дубль по DedupKey — штатная
// тишина; ошибка БД — warn в лог, вызывающий поток не прерывается.
func (s *NotificationService) Notify(ctx context.Context, n domain.Notification) {
	if s == nil || s.repo == nil {
		return
	}
	inserted, err := s.repo.Insert(ctx, n)
	if err != nil {
		s.log.Warn("notification: запись не удалась",
			zap.String("dedup_key", n.DedupKey), zap.String("title", n.Title), zap.Error(err))
		return
	}
	if inserted {
		s.log.Info("notification", zap.String("type", n.Type),
			zap.String("audience", n.Audience), zap.String("title", n.Title),
			zap.String("dedup_key", n.DedupKey))
	}
}

// List — видимые пользователю уведомления (для дропдауна колокольчика).
func (s *NotificationService) List(ctx context.Context, c *auth.Claims, unreadOnly bool, limit int) ([]domain.UserNotification, error) {
	return s.repo.ListForUser(ctx, username(c), AudiencesFor(c), unreadOnly, limit)
}

// UnreadCount — число непрочитанных (бейдж).
func (s *NotificationService) UnreadCount(ctx context.Context, c *auth.Claims) (int, error) {
	return s.repo.UnreadCount(ctx, username(c), AudiencesFor(c))
}

// MarkRead — отметить прочитанным (идемпотентно; чужое молча игнорируется).
func (s *NotificationService) MarkRead(ctx context.Context, c *auth.Claims, id int64) error {
	return s.repo.MarkRead(ctx, username(c), AudiencesFor(c), id)
}

// MarkAllRead — прочитать все видимые; возврат — сколько отмечено.
func (s *NotificationService) MarkAllRead(ctx context.Context, c *auth.Claims) (int, error) {
	return s.repo.MarkAllRead(ctx, username(c), AudiencesFor(c))
}

// Cleanup — крон-обслуживание (notifications-cleanup): удаление уведомлений
// старше retention (прочитанных и нет — решение владельца, 72 ч; dedup_key
// освобождается, живущая проблема справочника напомнит о себе снова) + сторож
// устаревания снимка дислокации.
func (s *NotificationService) Cleanup(ctx context.Context) error {
	cutoff := domain.LocalTime(clock.Now().Time().Add(-s.retention))
	n, err := s.repo.PurgeOlderThan(ctx, cutoff)
	if err != nil {
		return err
	}
	if n > 0 {
		s.log.Info("notifications cleanup", zap.Int("purged", n))
	}
	s.checkDislStale(ctx)
	return nil
}

// checkDislStale — сторож «АСУ молча отдаёт старое»: возраст последнего
// обновления дислокации (doc_ts журнала) против порога stale_after. Ловит
// случай, который гарды забора не видят: заборы формально успешны, а срез не
// движется. Дедуп — по штампу застрявшего обновления: одно уведомление на
// эпизод, а не каждый тик крона.
func (s *NotificationService) checkDislStale(ctx context.Context) {
	if s.journal == nil || s.stale <= 0 {
		return
	}
	last, ok := s.journal.LastDislDocTS(ctx)
	if !ok || last == nil || last.IsZero() {
		return // обновлений ещё не было (свежий стенд) — не сигналим
	}
	if n, stale := dislStaleNotification(last, clock.Now(), s.stale); stale {
		s.Notify(ctx, n)
	}
}

// username — имя пользователя из claims; nil (Keycloak выключен) → пусто.
func username(c *auth.Claims) string {
	if c == nil {
		return ""
	}
	return c.Username
}
