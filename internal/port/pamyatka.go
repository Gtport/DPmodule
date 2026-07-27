package port

import "context"

// PamyatkaCursorRepository — курсор инкремента памяток ГУ-45 по клиентам
// провайдера (таблица pamyatka_cursor). Провайдер отдаёт своё LAST_UPDATE в
// каждом ответе, следующий запрос уходит ровно с ним; в памяти курсор держать
// нельзя — рестарт потерял бы позицию.
type PamyatkaCursorRepository interface {
	// Get возвращает курсор клиента. Курсора нет → пустая строка без ошибки
	// (первый заход: интервал подставит сервис).
	Get(ctx context.Context, client string) (string, error)
	// Set сохраняет курсор клиента (upsert).
	Set(ctx context.Context, client, lastUpdate string) error
}
