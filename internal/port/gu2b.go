package port

import (
	"context"

	"github.com/Gtport/DPmodule/internal/domain"
)

// GU2BRepository — накопленные уведомления ГУ-2б и курсор их инкремента
// (таблицы gu2b_notification/gu2b_car/gu2b_cursor, миграция 000063).
// Уведомления ХРАНИМ (в отличие от памяток): по ним считается контроль полноты
// сквозной нумерации, дедуп повторов против прошлых тиков и — при выключенной
// перезаписи — накапливается материал для её включения (шаг 3 плана 17.08.2026).
type GU2BRepository interface {
	// GetCursor — курсор клиента; строки нет → пустая строка без ошибки
	// (первый заход: сервис запросит полную перезаливку since=0).
	GetCursor(ctx context.Context, client string) (string, error)
	// SetCursor — upsert курсора клиента.
	SetCursor(ctx context.Context, client, since string) error
	// Upsert сохраняет пачку уведомлений: шапка — по notification_id, вагоны —
	// полной заменой состава документа (повторный приход того же DocId несёт
	// последнюю версию, состав мог измениться).
	Upsert(ctx context.Context, notifications []domain.GU2BNotification) error
	// MaxNumber — наибольший сохранённый сквозной номер клиента (0 — нет ни
	// одного числового). Для контроля полноты нумерации.
	MaxNumber(ctx context.Context, client string) (int64, error)
	// MissingNumbers — дыры сквозной нумерации клиента в сохранённом диапазоне
	// (не больше limit значений). Дыра = уведомление, которое провайдер так и
	// не отдал, — как №1293–1316 attis в его собственном корпусе.
	MissingNumbers(ctx context.Context, client string, limit int) ([]int64, error)
	// PriorUnloadEvents — принятые ранее события выгрузки перечисленных вагонов
	// не старше from (для дедупа 72 ч против прошлых тиков).
	PriorUnloadEvents(ctx context.Context, vagons []string, from domain.LocalTime) ([]domain.GU2BPriorEvent, error)
}
