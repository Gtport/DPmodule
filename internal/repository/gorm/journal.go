package gormrepo

import (
	"context"

	"gorm.io/gorm"

	"github.com/Gtport/DPmodule/internal/domain"
)

// eventJournalModel — ORM-раскладка записи журнала. Detail хранится в jsonb-колонке
// как text (канон проекта: jsonb ↔ строка вручную, см. config.go/plan.go).
type eventJournalModel struct {
	ID        int64             `gorm:"column:id;primaryKey;autoIncrement"`
	EventType string            `gorm:"column:event_type"`
	Source    string            `gorm:"column:source"`
	Trigger   string            `gorm:"column:trigger"`
	Actor     string            `gorm:"column:actor"`
	DocTS     *domain.LocalTime `gorm:"column:doc_ts"`
	Detail    string            `gorm:"column:detail"` // jsonb → text
	CreatedAt domain.LocalTime  `gorm:"column:created_at"`
}

func (eventJournalModel) TableName() string { return "event_journal" }

func (m eventJournalModel) toDomain() domain.JournalEvent {
	detail := m.Detail
	if detail == "" {
		detail = "{}"
	}
	return domain.JournalEvent{
		ID: m.ID, EventType: m.EventType, Source: m.Source, Trigger: m.Trigger, Actor: m.Actor,
		DocTS: m.DocTS, Detail: []byte(detail), CreatedAt: m.CreatedAt,
	}
}

// JournalRepository — адаптер единого журнала событий (port.JournalRepository).
type JournalRepository struct {
	db *gorm.DB
}

func NewJournalRepository(db *gorm.DB) *JournalRepository {
	return &JournalRepository{db: db}
}

func (r *JournalRepository) Append(ctx context.Context, ev domain.JournalEvent) error {
	detail := string(ev.Detail)
	if detail == "" {
		detail = "{}"
	}
	m := eventJournalModel{
		EventType: ev.EventType, Source: ev.Source, Trigger: ev.Trigger, Actor: ev.Actor,
		DocTS: ev.DocTS, Detail: detail, CreatedAt: ev.CreatedAt,
	}
	return r.db.WithContext(ctx).Create(&m).Error
}

// Range возвращает события заданных типов за период [from, to] (nil-границы — без
// ограничения), свежие первыми. Для журнала обновлений дислокации с фильтром по периоду.
func (r *JournalRepository) Range(ctx context.Context, from, to *domain.LocalTime, eventTypes []string, limit int) ([]domain.JournalEvent, error) {
	if limit <= 0 {
		limit = 200
	}
	q := r.db.WithContext(ctx).Model(&eventJournalModel{})
	if len(eventTypes) > 0 {
		q = q.Where("event_type IN ?", eventTypes)
	}
	if from != nil && !from.IsZero() {
		q = q.Where("created_at >= ?", from.Time())
	}
	if to != nil && !to.IsZero() {
		q = q.Where("created_at <= ?", to.Time())
	}
	var ms []eventJournalModel
	if err := q.Order("created_at DESC, id DESC").Limit(limit).Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]domain.JournalEvent, len(ms))
	for i, m := range ms {
		out[i] = m.toDomain()
	}
	return out, nil
}

func (r *JournalRepository) LatestByType(ctx context.Context, eventType string) (domain.JournalEvent, bool, error) {
	var m eventJournalModel
	err := r.db.WithContext(ctx).
		Where("event_type = ?", eventType).
		Order("created_at DESC, id DESC").
		Limit(1).Take(&m).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return domain.JournalEvent{}, false, nil
		}
		return domain.JournalEvent{}, false, err
	}
	return m.toDomain(), true, nil
}

func (r *JournalRepository) LatestBySource(ctx context.Context, source string) (domain.JournalEvent, bool, error) {
	var m eventJournalModel
	err := r.db.WithContext(ctx).
		Where("source = ?", source).
		Order("created_at DESC, id DESC").
		Limit(1).Take(&m).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return domain.JournalEvent{}, false, nil
		}
		return domain.JournalEvent{}, false, err
	}
	return m.toDomain(), true, nil
}

func (r *JournalRepository) Recent(ctx context.Context, limit int) ([]domain.JournalEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	var ms []eventJournalModel
	if err := r.db.WithContext(ctx).
		Order("created_at DESC, id DESC").
		Limit(limit).Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]domain.JournalEvent, len(ms))
	for i, m := range ms {
		out[i] = m.toDomain()
	}
	return out, nil
}
