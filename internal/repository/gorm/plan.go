package gormrepo

import (
	"context"
	"encoding/json"
	"errors"

	"gorm.io/gorm"

	"github.com/Gtport/DPmodule/internal/domain"
)

// planModel — ORM-раскладка заголовка одной загрузки плана (история: id — PK).
type planModel struct {
	ID         int64             `gorm:"column:id;primaryKey;autoIncrement"`
	PlanCode   string            `gorm:"column:plan_code"`
	SourceFile string            `gorm:"column:source_file"`
	LoadedAt   *domain.LocalTime `gorm:"column:loaded_at"`
	PlanDate   *domain.LocalTime `gorm:"column:plan_date"`
	Nitki      int               `gorm:"column:nitki"`
	Matched    int               `gorm:"column:matched"`
	Stamped    int               `gorm:"column:stamped"`
}

func (planModel) TableName() string { return "plan" }

// planNitkaModel — ORM-раскладка нитки плана. Ports хранится в jsonb-колонке как
// text (канон проекта: jsonb ↔ строка, marshal/unmarshal вручную, см. config.go).
type planNitkaModel struct {
	ID            int64             `gorm:"column:id;primaryKey;autoIncrement"`
	PlanID        int64             `gorm:"column:plan_id"`
	PlanCode      string            `gorm:"column:plan_code"`
	Ord           int               `gorm:"column:ord"`
	Index         string            `gorm:"column:index"`
	IndexPp       string            `gorm:"column:index_pp"`
	StationOper   string            `gorm:"column:station_oper"`
	PlanMsk       *domain.LocalTime `gorm:"column:plan_msk"`
	PlanJd        *domain.LocalTime `gorm:"column:plan_jd"`
	FactMsk       *domain.LocalTime `gorm:"column:fact_msk"`
	Otkl          string            `gorm:"column:otkl"`
	PlanRaw       string            `gorm:"column:plan_raw"`
	Wagons        int               `gorm:"column:wagons"`
	Activ         int               `gorm:"column:activ"`
	Ports         string            `gorm:"column:ports"` // jsonb → text ([]PortCell)
	Sostav        string            `gorm:"column:sostav"`
	Comment       string            `gorm:"column:comment"`
	Matched       bool              `gorm:"column:matched"`
	MatchedWagons int               `gorm:"column:matched_wagons"`
	IsOstatok     bool              `gorm:"column:is_ostatok"`
	IsSf          bool              `gorm:"column:is_sf"`
}

func (planNitkaModel) TableName() string { return "plan_nitka" }

func toPlanModel(p domain.Plan) planModel {
	return planModel{
		PlanCode: p.PlanCode, SourceFile: p.SourceFile, LoadedAt: p.LoadedAt,
		PlanDate: p.PlanDate, Nitki: p.Nitki, Matched: p.Matched, Stamped: p.Stamped,
	}
}

func (m planModel) toDomain() domain.Plan {
	return domain.Plan{
		ID: m.ID, PlanCode: m.PlanCode, SourceFile: m.SourceFile, LoadedAt: m.LoadedAt,
		PlanDate: m.PlanDate, Nitki: m.Nitki, Matched: m.Matched, Stamped: m.Stamped,
	}
}

func (m planModel) toSummary() domain.PlanSummary {
	return domain.PlanSummary{
		ID: m.ID, PlanCode: m.PlanCode, SourceFile: m.SourceFile, LoadedAt: m.LoadedAt,
		PlanDate: m.PlanDate, Nitki: m.Nitki, Matched: m.Matched, Stamped: m.Stamped,
	}
}

// marshalPorts сериализует ячейки портов в JSON для jsonb-колонки. Пустой набор
// → "[]" (валидный jsonb, соответствует DEFAULT в схеме).
func marshalPorts(cells []domain.PortCell) string {
	if len(cells) == 0 {
		return "[]"
	}
	b, err := json.Marshal(cells)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// unmarshalPorts разбирает jsonb-текст в ячейки портов; пусто/мусор → nil.
func unmarshalPorts(s string) []domain.PortCell {
	if s == "" || s == "[]" {
		return nil
	}
	var cells []domain.PortCell
	if err := json.Unmarshal([]byte(s), &cells); err != nil {
		return nil
	}
	return cells
}

func toPlanNitkaModel(planID int64, n domain.PlanNitka) planNitkaModel {
	return planNitkaModel{
		PlanID: planID, PlanCode: n.PlanCode, Ord: n.Ord, Index: n.Index, IndexPp: n.IndexPp,
		StationOper: n.StationOper, PlanMsk: n.PlanMsk, PlanJd: n.PlanJd, FactMsk: n.FactMsk,
		Otkl: n.Otkl, PlanRaw: n.PlanRaw, Wagons: n.Wagons, Activ: n.Activ, Ports: marshalPorts(n.Ports),
		Sostav: n.Sostav, Comment: n.Comment, Matched: n.Matched, MatchedWagons: n.MatchedWagons,
		IsOstatok: n.IsOstatok, IsSf: n.IsSf,
	}
}

func (m planNitkaModel) toDomain() domain.PlanNitka {
	return domain.PlanNitka{
		PlanCode: m.PlanCode, Ord: m.Ord, Index: m.Index, IndexPp: m.IndexPp,
		StationOper: m.StationOper, PlanMsk: m.PlanMsk, PlanJd: m.PlanJd, FactMsk: m.FactMsk,
		Otkl: m.Otkl, PlanRaw: m.PlanRaw, Wagons: m.Wagons, Activ: m.Activ, Ports: unmarshalPorts(m.Ports),
		Sostav: m.Sostav, Comment: m.Comment, Matched: m.Matched, MatchedWagons: m.MatchedWagons,
		IsOstatok: m.IsOstatok, IsSf: m.IsSf,
	}
}

// PlanRepository реализует port.PlanRepository.
type PlanRepository struct {
	db *gorm.DB
}

func NewPlanRepository(db *gorm.DB) *PlanRepository {
	return &PlanRepository{db: db}
}

// SavePlan добавляет новую загрузку плана: INSERT заголовка (id присваивает БД) +
// INSERT ниток одной транзакцией. Прежние загрузки станции не трогает (история).
func (r *PlanRepository) SavePlan(ctx context.Context, header domain.Plan, nitki []domain.PlanNitka) (int64, error) {
	var id int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		h := toPlanModel(header)
		if err := tx.Create(&h).Error; err != nil {
			return err
		}
		id = h.ID
		if len(nitki) == 0 {
			return nil
		}
		models := make([]planNitkaModel, len(nitki))
		for i, n := range nitki {
			models[i] = toPlanNitkaModel(id, n)
		}
		return tx.CreateInBatches(models, batchSize).Error
	})
	return id, err
}

// ListPlans возвращает загрузки станции, свежие первыми (loaded_at DESC, затем id DESC).
func (r *PlanRepository) ListPlans(ctx context.Context, planCode string) ([]domain.PlanSummary, error) {
	var rows []planModel
	if err := r.db.WithContext(ctx).Where("plan_code = ?", planCode).
		Order("loaded_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domain.PlanSummary, len(rows))
	for i, m := range rows {
		out[i] = m.toSummary()
	}
	return out, nil
}

// GetLatestPlan возвращает самую свежую загрузку станции. Нет загрузок → пустой
// заголовок и пустой срез, без ошибки.
func (r *PlanRepository) GetLatestPlan(ctx context.Context, planCode string) (domain.Plan, []domain.PlanNitka, error) {
	var h planModel
	err := r.db.WithContext(ctx).Where("plan_code = ?", planCode).
		Order("loaded_at DESC, id DESC").Take(&h).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.Plan{}, nil, nil
	}
	if err != nil {
		return domain.Plan{}, nil, err
	}
	return r.loadNitki(ctx, h)
}

// GetPlanByID возвращает конкретную загрузку по id. Нет такой → пустой заголовок.
func (r *PlanRepository) GetPlanByID(ctx context.Context, id int64) (domain.Plan, []domain.PlanNitka, error) {
	var h planModel
	err := r.db.WithContext(ctx).Where("id = ?", id).Take(&h).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.Plan{}, nil, nil
	}
	if err != nil {
		return domain.Plan{}, nil, err
	}
	return r.loadNitki(ctx, h)
}

// loadNitki дочитывает нитки заголовка (по возрастанию ord) и собирает результат.
func (r *PlanRepository) loadNitki(ctx context.Context, h planModel) (domain.Plan, []domain.PlanNitka, error) {
	var rows []planNitkaModel
	if err := r.db.WithContext(ctx).Where("plan_id = ?", h.ID).
		Order("ord").Find(&rows).Error; err != nil {
		return domain.Plan{}, nil, err
	}
	nitki := make([]domain.PlanNitka, len(rows))
	for i, m := range rows {
		nitki[i] = m.toDomain()
	}
	return h.toDomain(), nitki, nil
}

// sfModel — ORM-раскладка справочника sf (синонимы станций формирования).
type sfModel struct {
	Sinonim  string `gorm:"column:sinonim"`
	Station  string `gorm:"column:station"`
	Quantity int    `gorm:"column:quantity"`
}

func (sfModel) TableName() string { return "sf" }

// ListSF возвращает включённые записи справочника sf.
func (r *PlanRepository) ListSF(ctx context.Context) ([]domain.SFRecord, error) {
	var rows []sfModel
	if err := r.db.WithContext(ctx).Where("enabled = ?", true).Order("sinonim").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domain.SFRecord, len(rows))
	for i, m := range rows {
		out[i] = domain.SFRecord{Sinonim: m.Sinonim, Station: m.Station, Quantity: m.Quantity}
	}
	return out, nil
}
