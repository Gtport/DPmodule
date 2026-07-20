package gormrepo

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Gtport/DPmodule/internal/domain"
)

// unplannedMoveModel — раскладка = dislocation (LIKE), таблица unplanned_move.
type unplannedMoveModel dislocationModel

func (unplannedMoveModel) TableName() string { return "unplanned_move" }

// UnplannedMoveRepository реализует port.UnplannedMoveRepository.
type UnplannedMoveRepository struct {
	db *gorm.DB
}

func NewUnplannedMoveRepository(db *gorm.DB) *UnplannedMoveRepository {
	return &UnplannedMoveRepository{db: db}
}

func (r *UnplannedMoveRepository) Upsert(ctx context.Context, items []domain.Dislocation) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}
	models := make([]unplannedMoveModel, len(items))
	for i, d := range items {
		models[i] = unplannedMoveModel(dislocationModel(d))
	}
	// При конфликте по vagon — полная замена записи (свежая позиция сигнала).
	res := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "vagon"}}, UpdateAll: true}).
		CreateInBatches(models, batchSize)
	if res.Error != nil {
		return 0, res.Error
	}
	return int(res.RowsAffected), nil
}

func (r *UnplannedMoveRepository) DeleteByVagons(ctx context.Context, vagons []string) (int, error) {
	if len(vagons) == 0 {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Where("vagon IN ?", vagons).Delete(&unplannedMoveModel{})
	return int(res.RowsAffected), res.Error
}

func (r *UnplannedMoveRepository) LoadAll(ctx context.Context) ([]domain.Dislocation, error) {
	var ms []unplannedMoveModel
	if err := r.db.WithContext(ctx).Order("\"index\", vagon").Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]domain.Dislocation, len(ms))
	for i, m := range ms {
		out[i] = domain.Dislocation(dislocationModel(m))
	}
	return out, nil
}
