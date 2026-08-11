package port

import (
	"context"

	"github.com/Gtport/DPmodule/internal/domain"
)

// NotificationRepository — хранилище внутренних уведомлений (таблицы
// notifications/notification_read). Видимость для пользователя: строки его
// аудиторий (audiences — из ролей claims, см. service.AudiencesFor) плюс
// адресованные ему лично (target_username).
type NotificationRepository interface {
	// Insert вставляет уведомление. Возврат false — вставка погашена дедупом
	// (dedup_key уже есть, ON CONFLICT DO NOTHING); это штатно, не ошибка.
	Insert(ctx context.Context, n domain.Notification) (bool, error)
	// ListForUser — видимые пользователю уведомления, новые сверху.
	// unreadOnly = true — только без строки в notification_read.
	ListForUser(ctx context.Context, username string, audiences []string, unreadOnly bool, limit int) ([]domain.UserNotification, error)
	// UnreadCount — число непрочитанных видимых (бейдж колокольчика).
	UnreadCount(ctx context.Context, username string, audiences []string) (int, error)
	// MarkRead отмечает уведомление прочитанным (идемпотентно). Чужое/несуществующее
	// уведомление молча игнорируется — строка прочтения не создаётся.
	MarkRead(ctx context.Context, username string, audiences []string, id int64) error
	// MarkAllRead отмечает прочитанными все видимые; возврат — сколько отмечено.
	MarkAllRead(ctx context.Context, username string, audiences []string) (int, error)
	// PurgeOlderThan удаляет уведомления старше cutoff (каскад чистит прочтения,
	// dedup_key освобождается); возврат — сколько удалено.
	PurgeOlderThan(ctx context.Context, cutoff domain.LocalTime) (int, error)
}
