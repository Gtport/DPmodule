package gormrepo

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

// Справочники раскладки НМТП-отчёта (миграция 000053). Билдер — обычный CRUD-кейс.

type nmtpColumnModel struct {
	ID            int64  `gorm:"column:id;primaryKey"`
	Terminal      string `gorm:"column:terminal"`
	SortOrder     int    `gorm:"column:sort_order"`
	GroupLabel    string `gorm:"column:group_label"`
	StationLabel  string `gorm:"column:station_label"`
	MarkLabel     string `gorm:"column:mark_label"`
	MatchClients  string `gorm:"column:match_clients"`
	MatchStations string `gorm:"column:match_stations"`
	MatchMarks    string `gorm:"column:match_marks"`
	Enabled       bool   `gorm:"column:enabled"`
}

func (nmtpColumnModel) TableName() string { return "nmtp_column" }

type nmtpMarkModel struct {
	Mark      string `gorm:"column:mark;primaryKey"`
	SortOrder int    `gorm:"column:sort_order"`
}

func (nmtpMarkModel) TableName() string { return "nmtp_mark" }

type NmtpRepository struct {
	db *gorm.DB
}

func NewNmtpRepository(db *gorm.DB) *NmtpRepository {
	return &NmtpRepository{db: db}
}

func (r *NmtpRepository) Columns(ctx context.Context, terminal string) ([]domain.NmtpColumn, error) {
	var ms []nmtpColumnModel
	err := r.db.WithContext(ctx).
		Where("terminal = ? AND enabled", terminal).
		Order("sort_order, id").
		Find(&ms).Error
	if err != nil {
		return nil, err
	}
	out := make([]domain.NmtpColumn, len(ms))
	for i, m := range ms {
		out[i] = domain.NmtpColumn{
			ID: m.ID, Terminal: m.Terminal, SortOrder: m.SortOrder,
			GroupLabel: m.GroupLabel, StationLabel: m.StationLabel, MarkLabel: m.MarkLabel,
			MatchClients: m.MatchClients, MatchStations: m.MatchStations, MatchMarks: m.MatchMarks,
		}
	}
	return out, nil
}

func (r *NmtpRepository) Marks(ctx context.Context) ([]string, error) {
	var ms []nmtpMarkModel
	if err := r.db.WithContext(ctx).Order("sort_order, mark").Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Mark
	}
	return out, nil
}

// ── Привязки «вагон → колонка» (nmtp_vagon_column, миграция 000055) ─────────

type nmtpVagonColumnModel struct {
	Vagon     string    `gorm:"column:vagon;primaryKey"`
	ColumnID  int64     `gorm:"column:column_id"`
	CreatedAt time.Time `gorm:"column:created_at"`
	CreatedBy string    `gorm:"column:created_by"`
}

func (nmtpVagonColumnModel) TableName() string { return "nmtp_vagon_column" }

func (r *NmtpRepository) VagonColumns(ctx context.Context) (map[string]domain.NmtpVagonBinding, error) {
	var ms []nmtpVagonColumnModel
	if err := r.db.WithContext(ctx).Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make(map[string]domain.NmtpVagonBinding, len(ms))
	for _, m := range ms {
		out[m.Vagon] = domain.NmtpVagonBinding{ColumnID: m.ColumnID, CreatedAt: domain.LocalTime(m.CreatedAt)}
	}
	return out, nil
}

func (r *NmtpRepository) SetVagonColumns(ctx context.Context, vagons []string, columnID int64, who string) error {
	if len(vagons) == 0 {
		return nil
	}
	now := clock.Now().Time()
	ms := make([]nmtpVagonColumnModel, len(vagons))
	for i, v := range vagons {
		ms[i] = nmtpVagonColumnModel{Vagon: v, ColumnID: columnID, CreatedAt: now, CreatedBy: who}
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "vagon"}},
		DoUpdates: clause.AssignmentColumns([]string{"column_id", "created_at", "created_by"}),
	}).Create(&ms).Error
}

func (r *NmtpRepository) DeleteVagonColumns(ctx context.Context, vagons []string) error {
	if len(vagons) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).
		Where("vagon IN ?", vagons).
		Delete(&nmtpVagonColumnModel{}).Error
}

func (r *NmtpRepository) DeleteVagonColumnsBefore(ctx context.Context, before time.Time) error {
	return r.db.WithContext(ctx).
		Where("created_at < ?", before).
		Delete(&nmtpVagonColumnModel{}).Error
}
