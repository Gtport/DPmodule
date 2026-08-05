package port

import (
	"context"

	"github.com/Gtport/DPmodule/internal/domain"
)

// GtSnapshotRepository — хранение сохранённых планов прогноза ГТ.
type GtSnapshotRepository interface {
	// Upsert сохраняет план; повтор по (plan_date, station) перезаписывает.
	Upsert(ctx context.Context, s domain.GtSnapshot) error
	// List — снапшоты периода (границы включительно), station пустой = все.
	// Без тяжёлых json-полей (только реквизиты списка).
	List(ctx context.Context, from, to domain.LocalTime, station string) ([]domain.GtSnapshot, error)
	// Get — полный снапшот; nil, если не найден.
	Get(ctx context.Context, planDate domain.LocalTime, station string) (*domain.GtSnapshot, error)
	// ListFull — полные снапшоты периода (для CSV-аналитики).
	ListFull(ctx context.Context, from, to domain.LocalTime, station string) ([]domain.GtSnapshot, error)
	Delete(ctx context.Context, planDate domain.LocalTime, station string) error
}
