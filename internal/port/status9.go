package port

import (
	"context"

	"github.com/Gtport/DPmodule/internal/domain"
)

// Status9Repository — персистентность таблицы кандидатов в прибытие (status9).
// Реализация — repository/gorm. См. §3.14 и миграцию 000010.
type Status9Repository interface {
	// VagonStatuses возвращает vagon → статус (8/9) для всех кандидатов в таблице.
	VagonStatuses(ctx context.Context) (map[string]int, error)
	// InsertNew добавляет записи; при конфликте по vagon НЕ перезаписывает
	// (сохраняет операторские правки и created_at). Возвращает число вставленных.
	InsertNew(ctx context.Context, items []domain.Dislocation) (int, error)
	// UpsertMissing добавляет пропавших (статус 8); при конфликте по vagon
	// обновляет только status и updated_at (правки прибытия сохраняются — так
	// живой кандидат 9 переводится в защищённый 8). Возвращает число затронутых.
	UpsertMissing(ctx context.Context, items []domain.Dislocation) (int, error)
	// DeleteByVagons удаляет кандидатов по номерам вагонов. Возвращает число удалённых.
	DeleteByVagons(ctx context.Context, vagons []string) (int, error)
}
