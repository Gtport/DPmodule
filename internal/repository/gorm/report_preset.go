package gormrepo

import (
	"context"

	"gorm.io/gorm"

	"github.com/Gtport/DPmodule/internal/domain"
)

// ReportPresetRepository реализует port.ReportPresetRepository (таблица report_preset).
type ReportPresetRepository struct {
	db *gorm.DB
}

func NewReportPresetRepository(db *gorm.DB) *ReportPresetRepository {
	return &ReportPresetRepository{db: db}
}

// reportPresetModel — ORM-раскладка колонок report_preset.
type reportPresetModel struct {
	ID        int64  `gorm:"column:id;primaryKey"`
	Report    string `gorm:"column:report"`
	Name      string `gorm:"column:name"`
	Clients   string `gorm:"column:clients"`
	SortOrder int    `gorm:"column:sort_order"`
	Enabled   bool   `gorm:"column:enabled"`
}

func (reportPresetModel) TableName() string { return "dpport.report_preset" }

func toReportPresetDomain(m reportPresetModel) domain.ReportPreset {
	return domain.ReportPreset{
		ID: m.ID, Report: m.Report, Name: m.Name, Clients: m.Clients,
		SortOrder: m.SortOrder, Enabled: m.Enabled,
	}
}

// List — включённые пресеты формы report, по sort_order (затем по имени).
func (r *ReportPresetRepository) List(ctx context.Context, report string) ([]domain.ReportPreset, error) {
	var ms []reportPresetModel
	if err := r.db.WithContext(ctx).
		Where("report = ? AND enabled", report).
		Order("sort_order, name").Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]domain.ReportPreset, 0, len(ms))
	for _, m := range ms {
		out = append(out, toReportPresetDomain(m))
	}
	return out, nil
}
