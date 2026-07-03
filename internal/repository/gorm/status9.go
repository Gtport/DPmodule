package gormrepo

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Gtport/DPmodule/internal/domain"
)

// status9Model — та же раскладка колонок, что dislocationModel (таблица status9
// создана как LIKE dislocation), но своя таблица. Не встраивание, а отдельный тип с
// тем же layout: gorm видит поля/теги напрямую (embedded не разворачивался при
// CreateInBatches). Ключ таблицы — vagon (миграция 000010).
type status9Model dislocationModel

func (status9Model) TableName() string { return "status9" }

// Status9Repository реализует port.Status9Repository.
type Status9Repository struct {
	db *gorm.DB
}

func NewStatus9Repository(db *gorm.DB) *Status9Repository {
	return &Status9Repository{db: db}
}

func (r *Status9Repository) Vagons(ctx context.Context) (map[string]struct{}, error) {
	var vagons []string
	if err := r.db.WithContext(ctx).Model(&status9Model{}).Pluck("vagon", &vagons).Error; err != nil {
		return nil, err
	}
	set := make(map[string]struct{}, len(vagons))
	for _, v := range vagons {
		set[v] = struct{}{}
	}
	return set, nil
}

func (r *Status9Repository) InsertNew(ctx context.Context, items []domain.Dislocation) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}
	models := make([]status9Model, len(items))
	for i, d := range items {
		models[i] = status9Model(dislocationModel(d))
	}
	// При конфликте по vagon — не трогаем существующую запись (операторские правки
	// и created_at сохраняются). RowsAffected = число реально вставленных.
	res := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "vagon"}}, DoNothing: true}).
		CreateInBatches(models, batchSize)
	if res.Error != nil {
		return 0, res.Error
	}
	return int(res.RowsAffected), nil
}

func (r *Status9Repository) DeleteByVagons(ctx context.Context, vagons []string) (int, error) {
	if len(vagons) == 0 {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Where("vagon IN ?", vagons).Delete(&status9Model{})
	if res.Error != nil {
		return 0, res.Error
	}
	return int(res.RowsAffected), nil
}
