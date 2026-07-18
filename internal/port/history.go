package port

import (
	"context"

	"github.com/Gtport/DPmodule/internal/domain"
)

// HistoryRepository — запись бизнес-истории рейса (vagon_history, §3.19). Кэш в RAM
// не держим (история растёт безгранично): существование id проверяем пакетным
// запросом по батчу. UpdateFields — динамический UPDATE только затронутых колонок.
type HistoryRepository interface {
	// ExistingIDs возвращает множество id из переданных, которые уже есть в vagon_history.
	ExistingIDs(ctx context.Context, ids []string) (map[string]struct{}, error)
	// Insert вставляет новые строки истории (полные вехи рейса).
	Insert(ctx context.Context, rows []domain.VagonHistory) error
	// UpdateFields точечно обновляет колонки строки по id (ключи — имена колонок).
	UpdateFields(ctx context.Context, id string, fields map[string]any) error
	// ArrivedRows — строки истории с фактом прибытия: date_prib_d ∈ [from; to]
	// (даты без времени), naznach из набора (пустой набор — все). Для «Истории
	// прибывших» домашней страницы.
	ArrivedRows(ctx context.Context, from, to domain.LocalTime, naznach []string) ([]domain.VagonHistory, error)
}
