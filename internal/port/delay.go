package port

import (
	"context"

	"github.com/Gtport/DPmodule/internal/domain"
)

// VagonDelayRepository — эпизоды задержек вагонов (таблица vagon_delay).
// Кэш в RAM не держим: открытых эпизодов немного (вагоны статуса 4/5 текущего
// снимка), reconcile-слой читает их пакетом каждый пересбор.
// Update — динамический UPDATE только затронутых колонок (канон GORM-гибрида).
type VagonDelayRepository interface {
	// Open — открытые эпизоды (date_to IS NULL), не больше одного на вагон.
	Open(ctx context.Context) ([]domain.VagonDelay, error)
	// Insert вставляет новый эпизод.
	Insert(ctx context.Context, d domain.VagonDelay) error
	// Update точечно обновляет колонки эпизода по id (ключи — имена колонок).
	Update(ctx context.Context, id int64, fields map[string]any) error
	// PurgeClosedOlderThan удаляет закрытые эпизоды (date_to не NULL), завершённые
	// раньше cutoff. Открытые не трогает. Возвращает число удалённых.
	PurgeClosedOlderThan(ctx context.Context, cutoff domain.LocalTime) (int, error)
}
