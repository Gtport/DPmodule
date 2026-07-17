package port

import (
	"context"

	"github.com/Gtport/DPmodule/internal/domain"
)

// AdminTablesRepository — универсальный CRUD справочников для админ-редактора.
// Работает ТОЛЬКО с таблицами из реестра dpport.list_tables (editable=true);
// имена таблиц/колонок валидируются по реестру и information_schema (динамический
// SQL с параметризованными значениями — канон GORM-гибрида).
type AdminTablesRepository interface {
	ListTables(ctx context.Context) ([]domain.AdminTable, error)
	Columns(ctx context.Context, table string) ([]domain.AdminColumn, error)
	Rows(ctx context.Context, table string, pk string) ([]domain.AdminRow, error)
	Insert(ctx context.Context, table string, cols []domain.AdminColumn, values domain.AdminRow) error
	Update(ctx context.Context, table string, pk string, id string, cols []domain.AdminColumn, values domain.AdminRow) error
	Delete(ctx context.Context, table string, pk string, id string) error
}
