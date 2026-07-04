package gormrepo

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Gtport/DPmodule/internal/domain"
)

// status6Model — раскладка колонок dislocation (таблица status6 = LIKE dislocation),
// своя таблица, ключ vagon. Не встраивание — иначе gorm не разворачивает поля
// (см. status9Model).
type status6Model dislocationModel

func (status6Model) TableName() string { return "status6" }

// Status6Repository реализует port.Status6Repository.
type Status6Repository struct {
	db *gorm.DB
}

func NewStatus6Repository(db *gorm.DB) *Status6Repository {
	return &Status6Repository{db: db}
}

func (r *Status6Repository) LoadAll(ctx context.Context) ([]domain.Dislocation, error) {
	var ms []status6Model
	if err := r.db.WithContext(ctx).Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]domain.Dislocation, len(ms))
	for i, m := range ms {
		out[i] = domain.Dislocation(dislocationModel(m))
	}
	return out, nil
}

func (r *Status6Repository) DeleteByVagons(ctx context.Context, vagons []string) (int, error) {
	if len(vagons) == 0 {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Where("vagon IN ?", vagons).Delete(&status6Model{})
	if res.Error != nil {
		return 0, res.Error
	}
	return int(res.RowsAffected), nil
}

func (r *Status6Repository) Upsert(ctx context.Context, items []domain.Dislocation) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}
	models := make([]status6Model, len(items))
	for i, d := range items {
		models[i] = status6Model(dislocationModel(d))
	}
	// При конфликте по vagon обновляем запись донора целиком (свежие данные) —
	// операторских правок в status6 нет, редактирования оператором не предусмотрено.
	res := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "vagon"}}, UpdateAll: true}).
		CreateInBatches(models, batchSize)
	if res.Error != nil {
		return 0, res.Error
	}
	return int(res.RowsAffected), nil
}
