package port

import (
	"context"

	"github.com/Gtport/DPmodule/internal/domain"
)

// UnplannedMoveRepository — «бесплановые в подходе» (таблица unplanned_move,
// карточка «Оперативка»): гружёные вагоны без плана, сменившие станцию ближе
// порога. Запись живёт до указания оператора либо автоснятия (план/прибытие).
type UnplannedMoveRepository interface {
	// Upsert добавляет/обновляет записи по вагону (свежая позиция сигнала).
	Upsert(ctx context.Context, items []domain.Dislocation) (int, error)
	// DeleteByVagons удаляет записи (указание оператора или автоснятие).
	DeleteByVagons(ctx context.Context, vagons []string) (int, error)
	// LoadAll возвращает все записи для агрегации в «Оперативке».
	LoadAll(ctx context.Context) ([]domain.Dislocation, error)
}
