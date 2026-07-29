package gormrepo

import (
	"context"

	"gorm.io/gorm"

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
			Terminal: m.Terminal, SortOrder: m.SortOrder,
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

func (r *NmtpRepository) Terminals(ctx context.Context) ([]string, error) {
	var out []string
	err := r.db.WithContext(ctx).Model(&nmtpColumnModel{}).
		Where("enabled").Distinct().
		Order("terminal").
		Pluck("terminal", &out).Error
	return out, err
}
