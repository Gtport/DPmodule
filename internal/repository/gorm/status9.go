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

func (r *Status9Repository) VagonStatuses(ctx context.Context) (map[string]int, error) {
	var rows []struct {
		Vagon  string
		Status *int
	}
	if err := r.db.WithContext(ctx).Model(&status9Model{}).Select("vagon, status").Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[string]int, len(rows))
	for _, x := range rows {
		s := 0
		if x.Status != nil {
			s = *x.Status
		}
		m[x.Vagon] = s
	}
	return m, nil
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

func (r *Status9Repository) UpsertMissing(ctx context.Context, items []domain.Dislocation) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}
	models := make([]status9Model, len(items))
	for i, d := range items {
		models[i] = status9Model(dislocationModel(d))
	}
	// При конфликте по vagon обновляем только status и updated_at (EXCLUDED —
	// значения из вставляемой строки: сервис проставил status=8 и updated_at).
	// Остальные поля (правки прибытия оператора) сохраняются.
	res := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "vagon"}},
			DoUpdates: clause.AssignmentColumns([]string{"status", "updated_at"}),
		}).
		CreateInBatches(models, batchSize)
	if res.Error != nil {
		return 0, res.Error
	}
	return int(res.RowsAffected), nil
}

func (r *Status9Repository) MissingOlderThan(ctx context.Context, cutoff domain.LocalTime) ([]string, error) {
	var vagons []string
	err := r.db.WithContext(ctx).Model(&status9Model{}).
		Where("status = 8 AND updated_at < ?", cutoff).
		Pluck("vagon", &vagons).Error
	if err != nil {
		return nil, err
	}
	return vagons, nil
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
