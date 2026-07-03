package port

import (
	"context"

	"github.com/Gtport/DPmodule/internal/domain"
)

// Status9Repository — персистентность таблицы кандидатов в прибытие (status9).
// Реализация — repository/gorm. См. §3.14 и миграцию 000010.
type Status9Repository interface {
	// Vagons возвращает множество номеров вагонов, уже лежащих в таблице.
	Vagons(ctx context.Context) (map[string]struct{}, error)
	// InsertNew добавляет записи; при конфликте по vagon НЕ перезаписывает
	// (сохраняет операторские правки и created_at). Возвращает число вставленных.
	InsertNew(ctx context.Context, items []domain.Dislocation) (int, error)
	// DeleteByVagons удаляет кандидатов по номерам вагонов. Возвращает число удалённых.
	DeleteByVagons(ctx context.Context, vagons []string) (int, error)
}
