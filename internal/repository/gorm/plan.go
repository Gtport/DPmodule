package gormrepo

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Gtport/DPmodule/internal/domain"
)

// planModel — ORM-раскладка заголовка плана (одна строка на plan_code).
type planModel struct {
	PlanCode   string            `gorm:"column:plan_code;primaryKey"`
	SourceFile string            `gorm:"column:source_file"`
	LoadedAt   *domain.LocalTime `gorm:"column:loaded_at"`
	Nitki      int               `gorm:"column:nitki"`
	Matched    int               `gorm:"column:matched"`
	Stamped    int               `gorm:"column:stamped"`
}

func (planModel) TableName() string { return "plan" }

// planNitkaModel — ORM-раскладка нитки плана. id — bigserial (assigned БД).
type planNitkaModel struct {
	ID            int64             `gorm:"column:id;primaryKey;autoIncrement"`
	PlanCode      string            `gorm:"column:plan_code"`
	Ord           int               `gorm:"column:ord"`
	Index         string            `gorm:"column:index"`
	IndexPp       string            `gorm:"column:index_pp"`
	PlanMsk       *domain.LocalTime `gorm:"column:plan_msk"`
	PlanJd        *domain.LocalTime `gorm:"column:plan_jd"`
	FactMsk       *domain.LocalTime `gorm:"column:fact_msk"`
	Otkl          string            `gorm:"column:otkl"`
	Wagons        int               `gorm:"column:wagons"`
	Activ         int               `gorm:"column:activ"`
	Matched       bool              `gorm:"column:matched"`
	MatchedWagons int               `gorm:"column:matched_wagons"`
}

func (planNitkaModel) TableName() string { return "plan_nitka" }

func toPlanModel(p domain.Plan) planModel {
	return planModel{
		PlanCode: p.PlanCode, SourceFile: p.SourceFile, LoadedAt: p.LoadedAt,
		Nitki: p.Nitki, Matched: p.Matched, Stamped: p.Stamped,
	}
}

func (m planModel) toDomain() domain.Plan {
	return domain.Plan{
		PlanCode: m.PlanCode, SourceFile: m.SourceFile, LoadedAt: m.LoadedAt,
		Nitki: m.Nitki, Matched: m.Matched, Stamped: m.Stamped,
	}
}

func toPlanNitkaModel(n domain.PlanNitka) planNitkaModel {
	return planNitkaModel{
		PlanCode: n.PlanCode, Ord: n.Ord, Index: n.Index, IndexPp: n.IndexPp,
		PlanMsk: n.PlanMsk, PlanJd: n.PlanJd, FactMsk: n.FactMsk, Otkl: n.Otkl,
		Wagons: n.Wagons, Activ: n.Activ, Matched: n.Matched, MatchedWagons: n.MatchedWagons,
	}
}

func (m planNitkaModel) toDomain() domain.PlanNitka {
	return domain.PlanNitka{
		PlanCode: m.PlanCode, Ord: m.Ord, Index: m.Index, IndexPp: m.IndexPp,
		PlanMsk: m.PlanMsk, PlanJd: m.PlanJd, FactMsk: m.FactMsk, Otkl: m.Otkl,
		Wagons: m.Wagons, Activ: m.Activ, Matched: m.Matched, MatchedWagons: m.MatchedWagons,
	}
}

// PlanRepository реализует port.PlanRepository.
type PlanRepository struct {
	db *gorm.DB
}

func NewPlanRepository(db *gorm.DB) *PlanRepository {
	return &PlanRepository{db: db}
}

// ReplacePlan атомарно перезаписывает план станции: upsert заголовка + полная
// замена ниток (delete по plan_code → insert). Одна транзакция.
func (r *PlanRepository) ReplacePlan(ctx context.Context, header domain.Plan, nitki []domain.PlanNitka) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		h := toPlanModel(header)
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "plan_code"}},
			UpdateAll: true,
		}).Create(&h).Error; err != nil {
			return err
		}

		if err := tx.Where("plan_code = ?", header.PlanCode).
			Delete(&planNitkaModel{}).Error; err != nil {
			return err
		}

		if len(nitki) == 0 {
			return nil
		}
		models := make([]planNitkaModel, len(nitki))
		for i, n := range nitki {
			models[i] = toPlanNitkaModel(n)
		}
		return tx.CreateInBatches(models, batchSize).Error
	})
}

// GetPlan возвращает заголовок и нитки плана (по возрастанию ord). Нет плана →
// пустой заголовок и пустой срез, без ошибки.
func (r *PlanRepository) GetPlan(ctx context.Context, planCode string) (domain.Plan, []domain.PlanNitka, error) {
	var h planModel
	err := r.db.WithContext(ctx).Where("plan_code = ?", planCode).Take(&h).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.Plan{}, nil, nil
	}
	if err != nil {
		return domain.Plan{}, nil, err
	}

	var rows []planNitkaModel
	if err := r.db.WithContext(ctx).Where("plan_code = ?", planCode).
		Order("ord").Find(&rows).Error; err != nil {
		return domain.Plan{}, nil, err
	}
	nitki := make([]domain.PlanNitka, len(rows))
	for i, m := range rows {
		nitki[i] = m.toDomain()
	}
	return h.toDomain(), nitki, nil
}
