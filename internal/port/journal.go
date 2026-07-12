package port

import (
	"context"

	"github.com/Gtport/DPmodule/internal/domain"
)

// JournalRepository — единый журнал событий данных (таблица event_journal).
// Append-only: только добавление записей, прежние не изменяются.
type JournalRepository interface {
	// Append добавляет одну запись журнала.
	Append(ctx context.Context, ev domain.JournalEvent) error
	// LatestByType возвращает самое свежее событие заданного типа (по created_at).
	// Нет события → ok=false (без ошибки). Для гарда актуальности и статус-панели.
	LatestByType(ctx context.Context, eventType string) (domain.JournalEvent, bool, error)
	// Recent возвращает последние N событий (свежие первыми) для панели/истории.
	Recent(ctx context.Context, limit int) ([]domain.JournalEvent, error)
}
