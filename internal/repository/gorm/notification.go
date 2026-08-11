package gormrepo

import (
	"context"
	"encoding/json"

	"gorm.io/gorm"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

// NotificationRepository — внутренние уведомления (notifications/notification_read).
// Fan-out на чтении: видимость = (пустой target_username И аудитория из ролей)
// ИЛИ адресовано лично. «Прочитано» — ленивый INSERT в notification_read.
type NotificationRepository struct {
	db *gorm.DB
}

func NewNotificationRepository(db *gorm.DB) *NotificationRepository {
	return &NotificationRepository{db: db}
}

// visibleCond — SQL-условие видимости уведомления пользователю. Аргументы —
// к плейсхолдерам: audiences, username.
const visibleCond = "((n.target_username = '' AND n.audience IN ?) OR n.target_username = ?)"

// Insert — вставка с дедупом: конфликт по частичному уникальному индексу
// dedup_key гасится молча (false = событие уже есть). Пустой dedup_key под
// индекс не попадает — такие строки вставляются всегда.
func (r *NotificationRepository) Insert(ctx context.Context, n domain.Notification) (bool, error) {
	created := n.CreatedAt
	if created.IsZero() {
		created = clock.Now()
	}
	var params any // nil → NULL в jsonb
	if len(n.ActionParams) > 0 {
		params = string(n.ActionParams)
	}
	var ids []int64
	err := r.db.WithContext(ctx).Raw(`
		INSERT INTO notifications
			(ntype, title, message, audience, target_username,
			 action_component, action_params, dedup_key, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (dedup_key) WHERE dedup_key <> '' DO NOTHING
		RETURNING id`,
		n.Type, n.Title, n.Message, n.Audience, n.TargetUsername,
		n.ActionComponent, params, n.DedupKey, created).Scan(&ids).Error
	if err != nil {
		return false, err
	}
	return len(ids) > 0, nil
}

// ListForUser — видимые уведомления, новые сверху; unreadOnly отбрасывает
// прочитанные (строка в notification_read).
func (r *NotificationRepository) ListForUser(ctx context.Context, username string, audiences []string, unreadOnly bool, limit int) ([]domain.UserNotification, error) {
	if limit <= 0 {
		limit = 100
	}
	type row struct {
		ID              int64             `gorm:"column:id"`
		Ntype           string            `gorm:"column:ntype"`
		Title           string            `gorm:"column:title"`
		Message         string            `gorm:"column:message"`
		Audience        string            `gorm:"column:audience"`
		TargetUsername  string            `gorm:"column:target_username"`
		ActionComponent string            `gorm:"column:action_component"`
		ActionParams    []byte            `gorm:"column:action_params"`
		DedupKey        string            `gorm:"column:dedup_key"`
		CreatedAt       domain.LocalTime  `gorm:"column:created_at"`
		ReadAt          *domain.LocalTime `gorm:"column:read_at"`
	}
	q := `
		SELECT n.*, nr.read_at
		FROM notifications n
		LEFT JOIN notification_read nr
			ON nr.notification_id = n.id AND nr.username = ?
		WHERE ` + visibleCond
	if unreadOnly {
		q += " AND nr.notification_id IS NULL"
	}
	q += " ORDER BY n.created_at DESC, n.id DESC LIMIT ?"
	var rows []row
	if err := r.db.WithContext(ctx).Raw(q, username, audiences, username, limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domain.UserNotification, 0, len(rows))
	for _, m := range rows {
		out = append(out, domain.UserNotification{
			Notification: domain.Notification{
				ID:              m.ID,
				Type:            m.Ntype,
				Title:           m.Title,
				Message:         m.Message,
				Audience:        m.Audience,
				TargetUsername:  m.TargetUsername,
				ActionComponent: m.ActionComponent,
				ActionParams:    json.RawMessage(m.ActionParams),
				DedupKey:        m.DedupKey,
				CreatedAt:       m.CreatedAt,
			},
			IsRead: m.ReadAt != nil,
			ReadAt: m.ReadAt,
		})
	}
	return out, nil
}

// UnreadCount — число непрочитанных видимых (бейдж).
func (r *NotificationRepository) UnreadCount(ctx context.Context, username string, audiences []string) (int, error) {
	var cnt int
	err := r.db.WithContext(ctx).Raw(`
		SELECT count(*)
		FROM notifications n
		LEFT JOIN notification_read nr
			ON nr.notification_id = n.id AND nr.username = ?
		WHERE `+visibleCond+` AND nr.notification_id IS NULL`,
		username, audiences, username).Scan(&cnt).Error
	return cnt, err
}

// MarkRead — идемпотентная отметка прочтения; невидимое пользователю
// уведомление игнорируется (INSERT..SELECT с условием видимости).
func (r *NotificationRepository) MarkRead(ctx context.Context, username string, audiences []string, id int64) error {
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO notification_read (username, notification_id, read_at)
		SELECT ?, n.id, ?
		FROM notifications n
		WHERE n.id = ? AND `+visibleCond+`
		ON CONFLICT (username, notification_id) DO NOTHING`,
		username, clock.Now(), id, audiences, username).Error
}

// MarkAllRead — отметить прочитанными все видимые; возврат — сколько отмечено.
func (r *NotificationRepository) MarkAllRead(ctx context.Context, username string, audiences []string) (int, error) {
	res := r.db.WithContext(ctx).Exec(`
		INSERT INTO notification_read (username, notification_id, read_at)
		SELECT ?, n.id, ?
		FROM notifications n
		WHERE `+visibleCond+`
		ON CONFLICT (username, notification_id) DO NOTHING`,
		username, clock.Now(), audiences, username)
	return int(res.RowsAffected), res.Error
}

// PurgeOlderThan — крон-очистка (retention 72 ч): каскад удаляет прочтения,
// dedup_key освобождается — живущая проблема напомнит о себе снова.
func (r *NotificationRepository) PurgeOlderThan(ctx context.Context, cutoff domain.LocalTime) (int, error) {
	res := r.db.WithContext(ctx).Exec(
		"DELETE FROM notifications WHERE created_at < ?", cutoff)
	return int(res.RowsAffected), res.Error
}
