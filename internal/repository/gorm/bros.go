package gormrepo

import (
	"context"

	"gorm.io/gorm"

	"github.com/Gtport/DPmodule/internal/domain"
)

// BrosReasonCodesRepository — чтение справочника кодов бросания.
type BrosReasonCodesRepository struct {
	db *gorm.DB
}

func NewBrosReasonCodesRepository(db *gorm.DB) *BrosReasonCodesRepository {
	return &BrosReasonCodesRepository{db: db}
}

// brosReasonCodeModel — ORM-раскладка справочника кодов бросания.
type brosReasonCodeModel struct {
	Code        string `gorm:"column:code;primaryKey"`
	Description string `gorm:"column:description"`
}

func (brosReasonCodeModel) TableName() string { return "bros_reason_codes" }

// ReasonCodes — весь справочник, отсортированный по коду.
func (r *BrosReasonCodesRepository) ReasonCodes(ctx context.Context) ([]domain.BrosReasonCode, error) {
	var rows []brosReasonCodeModel
	if err := r.db.WithContext(ctx).Order("code").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domain.BrosReasonCode, 0, len(rows))
	for _, m := range rows {
		out = append(out, domain.BrosReasonCode{Code: m.Code, Description: m.Description})
	}
	return out, nil
}
