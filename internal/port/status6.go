package port

import (
	"context"

	"github.com/Gtport/DPmodule/internal/domain"
)

// Status6Repository — доноры перегруза (таблица status6, §3.16). Наполнение — при
// переходе вагона на статус 6. Матч-донорство и удаление после использования —
// в S2-3. Реализация — repository/gorm.
type Status6Repository interface {
	// LoadAll читает всех доноров (прогрев RAM-кэша).
	LoadAll(ctx context.Context) ([]domain.Dislocation, error)
	// Upsert добавляет/обновляет доноров по vagon (свежие данные при повторном переходе).
	Upsert(ctx context.Context, items []domain.Dislocation) (int, error)
	// DeleteByVagons удаляет доноров (после использования в S2-3). Число удалённых.
	DeleteByVagons(ctx context.Context, vagons []string) (int, error)
}
