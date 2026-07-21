package port

// Репозиторий трейла продвижения (vagon_operation) и очереди запросов 601
// (vagon_op_request). Очередь — в БД: дедуп по trip_key (групповые смены
// статусов не плодят дублей) и живучесть через рестарты/деплои.

import (
	"context"

	"github.com/Gtport/DPmodule/internal/domain"
)

type VagonOperationRepository interface {
	// ReplaceForTrip перезаписывает ВСЕ операции рейса одной транзакцией
	// (DELETE по trip_key + батч-INSERT): каждая последующая история затирает
	// предыдущую, идемпотентно. trip_key проставляется здесь, единый на рейс.
	ReplaceForTrip(ctx context.Context, tripKey int64, ops []domain.VagonOperation) error
	// OperationsByTrip — сохранённый трейл рейса по времени операции.
	OperationsByTrip(ctx context.Context, tripKey int64) ([]domain.VagonOperation, error)

	// Enqueue кладёт заявки в очередь (upsert по trip_key: повторный триггер
	// обновляет причину/приоритет и сбрасывает счётчик неудач).
	Enqueue(ctx context.Context, reqs []domain.VagonOpRequest) error
	// NextBatch — очередная пачка: приоритет ↓, затем старые первыми.
	NextBatch(ctx context.Context, limit int) ([]domain.VagonOpRequest, error)
	// Complete снимает выполненную заявку.
	Complete(ctx context.Context, tripKey int64) error
	// Fail фиксирует неудачу (attempts+1, текст ошибки); после maxAttempts
	// заявка удаляется — по ней запроса больше не будет.
	Fail(ctx context.Context, tripKey int64, msg string, maxAttempts int, now domain.LocalTime) error
	// QueueSize — заявок в очереди (для статус-панели/диагностики).
	QueueSize(ctx context.Context) (int, error)
}

// WagonHistoryClient — исходящий запрос 601 к провайдеру АСУ:
// GET <base_url>/wagons/{vagon}/history/{client}?from=YYYY-MM-DD&to=YYYY-MM-DD.
type WagonHistoryClient interface {
	PullWagonHistory(ctx context.Context, client, vagon, from, to string) ([]byte, error)
}
