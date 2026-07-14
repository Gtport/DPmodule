package port

import (
	"context"
)

// SecretSource abstracts secret loading (env now, Vault later).
type SecretSource interface {
	Get(ctx context.Context, key string) (string, error)
}

// ASUClient abstracts integration with external АСУ systems.
type ASUClient interface {
	Pull(ctx context.Context, resource string) ([]byte, error)
	Push(ctx context.Context, resource string, payload []byte) error
}

// ReferenceClient — забор памяток на подачу/уборку у внешнего сервиса (тот же
// провайдер, что дислокация). ByNumber — по номеру памятки; Update — инкремент по
// клиенту с курсором last_update. Возвращает сырые байты ответа; разбор — выше.
type ReferenceClient interface {
	ByNumber(ctx context.Context, number string) ([]byte, error)
	Update(ctx context.Context, client, lastUpdate string) ([]byte, error)
}
