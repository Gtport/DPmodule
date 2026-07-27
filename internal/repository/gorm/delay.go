package gormrepo

import (
	"context"

	"gorm.io/gorm"

	"github.com/Gtport/DPmodule/internal/domain"
)

// VagonDelayRepository реализует port.VagonDelayRepository (таблица vagon_delay).
type VagonDelayRepository struct {
	db *gorm.DB
}

func NewVagonDelayRepository(db *gorm.DB) *VagonDelayRepository {
	return &VagonDelayRepository{db: db}
}

// vagonDelayModel — ORM-раскладка колонок vagon_delay. Всё время — LocalTime
// (Московское naive).
type vagonDelayModel struct {
	ID          int64             `gorm:"column:id;primaryKey"`
	Vagon       string            `gorm:"column:vagon"`
	DateNachD   *domain.LocalTime `gorm:"column:date_nach_d"`
	Kind        int               `gorm:"column:kind"`
	GroupKey    string            `gorm:"column:group_key"`
	Index       string            `gorm:"column:index_poezd"`
	IndexMain   string            `gorm:"column:index_main"`
	StationCode string            `gorm:"column:station_code"`
	StationName string            `gorm:"column:station_name"`
	Doroga      string            `gorm:"column:doroga"`
	DateFrom    *domain.LocalTime `gorm:"column:date_from"`
	DateTo      *domain.LocalTime `gorm:"column:date_to"`
	Hours       *float64          `gorm:"column:hours"`
	CreatedAt   *domain.LocalTime `gorm:"column:created_at"`
	UpdatedAt   *domain.LocalTime `gorm:"column:updated_at"`
}

func (vagonDelayModel) TableName() string { return "vagon_delay" }

func toVagonDelayModel(d domain.VagonDelay) vagonDelayModel {
	return vagonDelayModel{
		ID: d.ID, Vagon: d.Vagon, DateNachD: d.DateNachD, Kind: d.Kind,
		GroupKey: d.GroupKey, Index: d.Index, IndexMain: d.IndexMain,
		StationCode: d.StationCode, StationName: d.StationName, Doroga: d.Doroga,
		DateFrom: d.DateFrom, DateTo: d.DateTo, Hours: d.Hours,
		CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
	}
}

func toVagonDelayDomain(m vagonDelayModel) domain.VagonDelay {
	return domain.VagonDelay{
		ID: m.ID, Vagon: m.Vagon, DateNachD: m.DateNachD, Kind: m.Kind,
		GroupKey: m.GroupKey, Index: m.Index, IndexMain: m.IndexMain,
		StationCode: m.StationCode, StationName: m.StationName, Doroga: m.Doroga,
		DateFrom: m.DateFrom, DateTo: m.DateTo, Hours: m.Hours,
		CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

// Open — открытые эпизоды (date_to IS NULL), не больше одного на вагон
// (частичный уникальный индекс uq_vagon_delay_open).
func (r *VagonDelayRepository) Open(ctx context.Context) ([]domain.VagonDelay, error) {
	var ms []vagonDelayModel
	if err := r.db.WithContext(ctx).Where("date_to IS NULL").
		Order("date_from").Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]domain.VagonDelay, len(ms))
	for i, m := range ms {
		out[i] = toVagonDelayDomain(m)
	}
	return out, nil
}

func (r *VagonDelayRepository) Insert(ctx context.Context, d domain.VagonDelay) error {
	m := toVagonDelayModel(d)
	return r.db.WithContext(ctx).Create(&m).Error
}

func (r *VagonDelayRepository) Update(ctx context.Context, id int64, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&vagonDelayModel{}).
		Where("id = ?", id).Updates(fields).Error
}

// PurgeClosedOlderThan удаляет закрытые эпизоды старше cutoff (по date_to);
// открытые (date_to IS NULL) не трогает.
func (r *VagonDelayRepository) PurgeClosedOlderThan(ctx context.Context, cutoff domain.LocalTime) (int, error) {
	res := r.db.WithContext(ctx).
		Where("date_to IS NOT NULL AND date_to < ?", cutoff).
		Delete(&vagonDelayModel{})
	if res.Error != nil {
		return 0, res.Error
	}
	return int(res.RowsAffected), nil
}
