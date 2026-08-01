package gormrepo

import (
	"context"

	"gorm.io/gorm"

	"github.com/Gtport/DPmodule/internal/domain"
)

// LKAccountRepository — чтение аккаунтов ЛК РЖД для автовыгрузки дислокации.
type LKAccountRepository struct {
	db *gorm.DB
}

func NewLKAccountRepository(db *gorm.DB) *LKAccountRepository {
	return &LKAccountRepository{db: db}
}

// lkAccountModel — ORM-раскладка настроечной таблицы lk_account.
type lkAccountModel struct {
	ID        int64  `gorm:"column:id;primaryKey;autoIncrement"`
	OKPO      int64  `gorm:"column:okpo"`
	Login     string `gorm:"column:login"`
	Name      string `gorm:"column:name"`
	SortOrder int    `gorm:"column:sort_order"`
	Enabled   bool   `gorm:"column:enabled"`
}

func (lkAccountModel) TableName() string { return "lk_account" }

// Accounts — включённые аккаунты по порядку.
func (r *LKAccountRepository) Accounts(ctx context.Context) ([]domain.LKAccount, error) {
	var rows []lkAccountModel
	if err := r.db.WithContext(ctx).
		Where("enabled").
		Order("sort_order, okpo").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domain.LKAccount, 0, len(rows))
	for _, m := range rows {
		out = append(out, domain.LKAccount{OKPO: m.OKPO, Login: m.Login, Name: m.Name})
	}
	return out, nil
}
