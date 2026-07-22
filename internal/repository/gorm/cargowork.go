package gormrepo

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Gtport/DPmodule/internal/domain"
)

// portCargoLineModel — ORM-раскладка справочника линий учёта терминала.
type portCargoLineModel struct {
	ID        int64  `gorm:"column:id;primaryKey;autoIncrement"`
	Terminal  string `gorm:"column:terminal"`
	Kind      string `gorm:"column:kind"`
	CargoKey  string `gorm:"column:cargo_key"`
	Label     string `gorm:"column:label"`
	Pc        *int   `gorm:"column:pc"`
	SortOrder int    `gorm:"column:sort_order"`
	Enabled   bool   `gorm:"column:enabled"`
	PlanLabel string `gorm:"column:plan_label"`
}

func (portCargoLineModel) TableName() string { return "port_cargo_line" }

// cargoWorkModel — ORM-раскладка суточного учётного листа выгрузки.
// JSON-снимки лежат в jsonb-колонках, в модели — строкой (канон проекта).
type cargoWorkModel struct {
	ID       int64            `gorm:"column:id;primaryKey;autoIncrement"`
	DateJd   domain.LocalTime `gorm:"column:date_jd"`
	Terminal string           `gorm:"column:terminal"`
	CargoKey string           `gorm:"column:cargo_key"`

	Ost18           int    `gorm:"column:ost_18"`
	OstSt           int    `gorm:"column:ost_st"`
	Prib            int    `gorm:"column:prib"`
	VigrStan        int    `gorm:"column:vigr_stan"`
	UsefulFormation int    `gorm:"column:useful_formation"`
	TotalFormation  int    `gorm:"column:total_formation"`
	Downtime        string `gorm:"column:downtime"`

	Plan     int    `gorm:"column:plan"`
	VigrFact int    `gorm:"column:vigr_fact"`
	Prim     string `gorm:"column:prim"`

	Ost       int `gorm:"column:ost"`
	Effectiv  int `gorm:"column:effectiv"`
	Perepokaz int `gorm:"column:perepokaz"`

	AnalyticsJSON      *string `gorm:"column:analytics_json"`
	TrainStructureJSON *string `gorm:"column:train_structure_json"`

	CreatedAt *domain.LocalTime `gorm:"column:created_at"`
	UpdatedAt *domain.LocalTime `gorm:"column:updated_at"`
}

func (cargoWorkModel) TableName() string { return "cargo_work" }

// cargoWorkLoadModel — ORM-раскладка суточной строки погрузки.
type cargoWorkLoadModel struct {
	ID        int64             `gorm:"column:id;primaryKey;autoIncrement"`
	DateJd    domain.LocalTime  `gorm:"column:date_jd"`
	Terminal  string            `gorm:"column:terminal"`
	CargoKey  string            `gorm:"column:cargo_key"`
	LoadFact  int               `gorm:"column:load_fact"`
	Plan      int               `gorm:"column:plan"`
	Ost       int               `gorm:"column:ost"`
	CreatedAt *domain.LocalTime `gorm:"column:created_at"`
	UpdatedAt *domain.LocalTime `gorm:"column:updated_at"`
}

func (cargoWorkLoadModel) TableName() string { return "cargo_work_load" }

// CargoWorkRepository — хранение «Грузовой работы» (билдер: CRUD и upsert).
type CargoWorkRepository struct {
	db *gorm.DB
}

func NewCargoWorkRepository(db *gorm.DB) *CargoWorkRepository {
	return &CargoWorkRepository{db: db}
}

func (r *CargoWorkRepository) Lines(ctx context.Context) ([]domain.PortCargoLine, error) {
	var models []portCargoLineModel
	if err := r.db.WithContext(ctx).
		Where("enabled = true").
		Order("terminal, kind, sort_order, id").
		Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]domain.PortCargoLine, 0, len(models))
	for _, m := range models {
		out = append(out, domain.PortCargoLine{
			ID: m.ID, Terminal: m.Terminal, Kind: m.Kind, CargoKey: m.CargoKey,
			Label: m.Label, Pc: m.Pc, SortOrder: m.SortOrder, Enabled: m.Enabled,
			PlanLabel: m.PlanLabel,
		})
	}
	return out, nil
}

func (r *CargoWorkRepository) Rows(ctx context.Context, from, to domain.LocalTime, terminal string) ([]domain.CargoWorkRow, error) {
	q := r.db.WithContext(ctx).Model(&cargoWorkModel{}).
		Where("date_jd BETWEEN ? AND ?", from, to)
	if terminal != "" {
		q = q.Where("terminal = ?", terminal)
	}
	var models []cargoWorkModel
	if err := q.Order("date_jd DESC, terminal, cargo_key").Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]domain.CargoWorkRow, 0, len(models))
	for _, m := range models {
		out = append(out, domain.CargoWorkRow{
			ID: m.ID, DateJd: m.DateJd, Terminal: m.Terminal, CargoKey: m.CargoKey,
			Ost18: m.Ost18, OstSt: m.OstSt, Prib: m.Prib, VigrStan: m.VigrStan,
			UsefulFormation: m.UsefulFormation, TotalFormation: m.TotalFormation,
			Downtime: m.Downtime,
			Plan:     m.Plan, VigrFact: m.VigrFact, Prim: m.Prim,
			Ost: m.Ost, Effectiv: m.Effectiv, Perepokaz: m.Perepokaz,
			AnalyticsJSON:      derefStr(m.AnalyticsJSON),
			TrainStructureJSON: derefStr(m.TrainStructureJSON),
			CreatedAt:          m.CreatedAt, UpdatedAt: m.UpdatedAt,
		})
	}
	return out, nil
}

func (r *CargoWorkRepository) LoadRows(ctx context.Context, from, to domain.LocalTime, terminal string) ([]domain.CargoWorkLoadRow, error) {
	q := r.db.WithContext(ctx).Model(&cargoWorkLoadModel{}).
		Where("date_jd BETWEEN ? AND ?", from, to)
	if terminal != "" {
		q = q.Where("terminal = ?", terminal)
	}
	var models []cargoWorkLoadModel
	if err := q.Order("date_jd DESC, terminal, cargo_key").Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]domain.CargoWorkLoadRow, 0, len(models))
	for _, m := range models {
		out = append(out, domain.CargoWorkLoadRow{
			ID: m.ID, DateJd: m.DateJd, Terminal: m.Terminal, CargoKey: m.CargoKey,
			LoadFact: m.LoadFact, Plan: m.Plan, Ost: m.Ost,
			CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
		})
	}
	return out, nil
}

// cargoWorkKey — естественный ключ строки учёта (он же цель ON CONFLICT).
var cargoWorkKey = []clause.Column{
	{Name: "date_jd"}, {Name: "terminal"}, {Name: "cargo_key"},
}

// UpsertRows — вставка/обновление по ключу. created_at при конфликте не трогаем
// (строка «родилась» один раз), id не перезаписываем.
func (r *CargoWorkRepository) UpsertRows(ctx context.Context, rows []domain.CargoWorkRow) error {
	if len(rows) == 0 {
		return nil
	}
	models := make([]cargoWorkModel, 0, len(rows))
	for _, x := range rows {
		models = append(models, cargoWorkModel{
			DateJd: x.DateJd, Terminal: x.Terminal, CargoKey: x.CargoKey,
			Ost18: x.Ost18, OstSt: x.OstSt, Prib: x.Prib, VigrStan: x.VigrStan,
			UsefulFormation: x.UsefulFormation, TotalFormation: x.TotalFormation,
			Downtime: x.Downtime,
			Plan:     x.Plan, VigrFact: x.VigrFact, Prim: x.Prim,
			Ost: x.Ost, Effectiv: x.Effectiv, Perepokaz: x.Perepokaz,
			AnalyticsJSON:      nullableStr(x.AnalyticsJSON),
			TrainStructureJSON: nullableStr(x.TrainStructureJSON),
			CreatedAt:          x.CreatedAt, UpdatedAt: x.UpdatedAt,
		})
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: cargoWorkKey,
		DoUpdates: clause.AssignmentColumns([]string{
			"ost_18", "ost_st", "prib", "vigr_stan",
			"useful_formation", "total_formation", "downtime",
			"plan", "vigr_fact", "prim",
			"ost", "effectiv", "perepokaz",
			"analytics_json", "train_structure_json", "updated_at",
		}),
	}).Create(&models).Error
}

func (r *CargoWorkRepository) UpsertLoadRows(ctx context.Context, rows []domain.CargoWorkLoadRow) error {
	if len(rows) == 0 {
		return nil
	}
	models := make([]cargoWorkLoadModel, 0, len(rows))
	for _, x := range rows {
		models = append(models, cargoWorkLoadModel{
			DateJd: x.DateJd, Terminal: x.Terminal, CargoKey: x.CargoKey,
			LoadFact: x.LoadFact, Plan: x.Plan, Ost: x.Ost,
			CreatedAt: x.CreatedAt, UpdatedAt: x.UpdatedAt,
		})
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: cargoWorkKey,
		DoUpdates: clause.AssignmentColumns([]string{
			"load_fact", "plan", "ost", "updated_at",
		}),
	}).Create(&models).Error
}

// DeleteDay — удаление учёта суток терминала: выгрузка и погрузка одной
// транзакцией, чтобы не осталось «половины» листа.
func (r *CargoWorkRepository) DeleteDay(ctx context.Context, day domain.LocalTime, terminal string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("date_jd = ? AND terminal = ?", day, terminal).
			Delete(&cargoWorkModel{}).Error; err != nil {
			return err
		}
		return tx.Where("date_jd = ? AND terminal = ?", day, terminal).
			Delete(&cargoWorkLoadModel{}).Error
	})
}

// nullableStr — пустая строка в jsonb-колонку идёт как NULL (пустое ≠ "").
func nullableStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
