package gormrepo

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Gtport/DPmodule/internal/domain"
)

// gtSnapshotModel — ORM-раскладка сохранённого плана прогноза ГТ.
// json-слепки — text-строками (канон: jsonb в модели строкой).
type gtSnapshotModel struct {
	ID        int64             `gorm:"column:id;primaryKey;autoIncrement"`
	PlanDate  domain.LocalTime  `gorm:"column:plan_date"`
	Station   string            `gorm:"column:station"`
	StartDate domain.LocalTime  `gorm:"column:start_date"`
	DaysCount int               `gorm:"column:days_count"`
	Request   string            `gorm:"column:request"`
	Trains    string            `gorm:"column:trains"`
	Flows     string            `gorm:"column:flows"`
	FreeSlots string            `gorm:"column:free_slots"`
	Journal   string            `gorm:"column:journal"`
	SavedBy   string            `gorm:"column:saved_by"`
	// Паспорт расчёта (миграция 000064).
	ComputedAt *domain.LocalTime `gorm:"column:computed_at"`
	Kind       string            `gorm:"column:kind"`
	Meta       string            `gorm:"column:meta"`
	CreatedAt  *domain.LocalTime `gorm:"column:created_at"`
	UpdatedAt  *domain.LocalTime `gorm:"column:updated_at"`
}

func (gtSnapshotModel) TableName() string { return "dpport.gt_forecast_snapshot" }

// GtSnapshotRepository — хранение сохранённых планов прогноза ГТ (билдер: CRUD/upsert).
type GtSnapshotRepository struct {
	db *gorm.DB
}

func NewGtSnapshotRepository(db *gorm.DB) *GtSnapshotRepository {
	return &GtSnapshotRepository{db: db}
}

func (r *GtSnapshotRepository) Upsert(ctx context.Context, s domain.GtSnapshot) error {
	m := gtSnapshotModel{
		PlanDate: s.PlanDate, Station: s.Station, StartDate: s.StartDate,
		DaysCount: s.DaysCount, Request: s.Request, Trains: s.Trains,
		Flows: s.Flows, FreeSlots: s.FreeSlots, Journal: s.Journal,
		SavedBy: s.SavedBy, ComputedAt: s.ComputedAt, Kind: s.Kind, Meta: s.Meta,
		CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
	if m.Kind == "" {
		m.Kind = domain.GtSnapshotManual
	}
	if m.Meta == "" {
		m.Meta = "null" // jsonb: пустая строка не разбирается
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "plan_date"}, {Name: "station"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"start_date", "days_count", "request", "trains", "flows",
			"free_slots", "journal", "saved_by", "computed_at", "kind", "meta", "updated_at",
		}),
	}).Create(&m).Error
}

func (r *GtSnapshotRepository) List(ctx context.Context, from, to domain.LocalTime, station string) ([]domain.GtSnapshot, error) {
	return r.list(ctx, from, to, station,
		"id, plan_date, station, start_date, days_count, saved_by, computed_at, kind, created_at, updated_at")
}

func (r *GtSnapshotRepository) ListFull(ctx context.Context, from, to domain.LocalTime, station string) ([]domain.GtSnapshot, error) {
	return r.list(ctx, from, to, station, "*")
}

func (r *GtSnapshotRepository) list(ctx context.Context, from, to domain.LocalTime, station, sel string) ([]domain.GtSnapshot, error) {
	q := r.db.WithContext(ctx).Model(&gtSnapshotModel{}).Select(sel).
		Where("plan_date BETWEEN ? AND ?", from, to)
	if station != "" {
		q = q.Where("station = ?", station)
	}
	var models []gtSnapshotModel
	if err := q.Order("plan_date DESC, station").Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]domain.GtSnapshot, 0, len(models))
	for _, m := range models {
		out = append(out, toDomainSnapshot(m))
	}
	return out, nil
}

func (r *GtSnapshotRepository) Get(ctx context.Context, planDate domain.LocalTime, station string) (*domain.GtSnapshot, error) {
	var m gtSnapshotModel
	err := r.db.WithContext(ctx).
		Where("plan_date = ? AND station = ?", planDate, station).
		First(&m).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s := toDomainSnapshot(m)
	return &s, nil
}

func (r *GtSnapshotRepository) Delete(ctx context.Context, planDate domain.LocalTime, station string) error {
	return r.db.WithContext(ctx).
		Where("plan_date = ? AND station = ?", planDate, station).
		Delete(&gtSnapshotModel{}).Error
}

func toDomainSnapshot(m gtSnapshotModel) domain.GtSnapshot {
	return domain.GtSnapshot{
		ID: m.ID, PlanDate: m.PlanDate, Station: m.Station, StartDate: m.StartDate,
		DaysCount: m.DaysCount, Request: m.Request, Trains: m.Trains, Flows: m.Flows,
		FreeSlots: m.FreeSlots, Journal: m.Journal, SavedBy: m.SavedBy,
		ComputedAt: m.ComputedAt, Kind: m.Kind, Meta: m.Meta,
		CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}
