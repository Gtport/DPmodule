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
	log       *zap.Logger
}

func NewNotificationService(repo port.NotificationRepository, retention time.Duration, log *zap.Logger) *NotificationService {
	return &NotificationService{repo: repo, retention: retention, log: log}
}

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
// освобождается, живущая проблема справочника напомнит о себе снова).
func (s *NotificationService) Cleanup(ctx context.Context) error {
	cutoff := domain.LocalTime(clock.Now().Time().Add(-s.retention))
	n, err := s.repo.PurgeOlderThan(ctx, cutoff)
	if err != nil {
		return err
	}
	if n > 0 {
		s.log.Info("notifications cleanup", zap.Int("purged", n))
	}
	return nil
}

// username — имя пользователя из claims; nil (Keycloak выключен) → пусто.
func username(c *auth.Claims) string {
	if c == nil {
		return ""
	}
	return c.Username
}
