package gormrepo

import (
	"context"

	"gorm.io/gorm"

	"github.com/Gtport/DPmodule/internal/domain"
)

// MaxChatRepository — чтение справочника чатов MAX и маршрутов рассылки.
type MaxChatRepository struct {
	db *gorm.DB
}

func NewMaxChatRepository(db *gorm.DB) *MaxChatRepository {
	return &MaxChatRepository{db: db}
}

// maxChatModel — ORM-раскладка справочника чатов.
type maxChatModel struct {
	Name        string `gorm:"column:name;primaryKey"`
	ChatID      string `gorm:"column:chat_id"`
	Description string `gorm:"column:description"`
	IsActive    bool   `gorm:"column:is_active"`
}

func (maxChatModel) TableName() string { return "max_chat" }

// maxRouteModel — ORM-раскладка маршрутов рассылки.
type maxRouteModel struct {
	ID        int64  `gorm:"column:id;primaryKey;autoIncrement"`
	Report    string `gorm:"column:report"`
	Terminal  string `gorm:"column:terminal"`
	ChatName  string `gorm:"column:chat_name"`
	SortOrder int    `gorm:"column:sort_order"`
	Enabled   bool   `gorm:"column:enabled"`
}

func (maxRouteModel) TableName() string { return "max_route" }

// Chats — все чаты по имени.
func (r *MaxChatRepository) Chats(ctx context.Context) ([]domain.MaxChat, error) {
	var rows []maxChatModel
	if err := r.db.WithContext(ctx).Order("name").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domain.MaxChat, 0, len(rows))
	for _, m := range rows {
		out = append(out, domain.MaxChat{
			Name:        m.Name,
			ChatID:      m.ChatID,
			Description: m.Description,
			IsActive:    m.IsActive,
		})
	}
	return out, nil
}

// Routes — включённые маршруты формы report для терминала terminal, по порядку.
func (r *MaxChatRepository) Routes(ctx context.Context, report, terminal string) ([]domain.MaxRoute, error) {
	var rows []maxRouteModel
	err := r.db.WithContext(ctx).
		Where("report = ? AND terminal = ? AND enabled = true", report, terminal).
		Order("sort_order, id").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]domain.MaxRoute, 0, len(rows))
	for _, m := range rows {
		out = append(out, domain.MaxRoute{
			ID:        m.ID,
			Report:    m.Report,
			Terminal:  m.Terminal,
			ChatName:  m.ChatName,
			SortOrder: m.SortOrder,
			Enabled:   m.Enabled,
		})
	}
	return out, nil
}
