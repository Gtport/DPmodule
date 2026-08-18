package gormrepo

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

// pamyatkaCursorModel — курсор инкремента памяток по клиентам провайдера.
// Без gorm.Model/хуков: штамп времени ставим сами из clock.Now() (канон —
// единственный источник «сейчас»).
type pamyatkaCursorModel struct {
	Client     string            `gorm:"column:client;primaryKey"`
	LastUpdate string            `gorm:"column:last_update"`
	UpdatedAt  *domain.LocalTime `gorm:"column:updated_at"`
}

func (pamyatkaCursorModel) TableName() string { return "dpport.pamyatka_cursor" }

// PamyatkaCursorRepository реализует port.PamyatkaCursorRepository.
type PamyatkaCursorRepository struct{ db *gorm.DB }

func NewPamyatkaCursorRepository(db *gorm.DB) *PamyatkaCursorRepository {
	return &PamyatkaCursorRepository{db: db}
}

// Get — курсор клиента; строки нет → пустая строка без ошибки (первый заход).
func (r *PamyatkaCursorRepository) Get(ctx context.Context, client string) (string, error) {
	var m pamyatkaCursorModel
	err := r.db.WithContext(ctx).Where("client = ?", client).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return m.LastUpdate, nil
}

// Set — upsert курсора клиента.
func (r *PamyatkaCursorRepository) Set(ctx context.Context, client, lastUpdate string) error {
	now := clock.Now()
	m := pamyatkaCursorModel{Client: client, LastUpdate: lastUpdate, UpdatedAt: &now}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "client"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_update", "updated_at"}),
	}).Create(&m).Error
}
