package port

import (
	"context"
)

// SecretSource abstracts secret loading (env now, Vault later).
type SecretSource interface {
	Get(ctx context.Context, key string) (string, error)
}

// TokenProvider — источник access-токена для ИСХОДЯЩИХ запросов к провайдеру
// (client_credentials к Keycloak сервис-аккаунтом). Реализация сама держит кэш и
// обновляет токен по истечении — вызывающий просто просит токен на каждый запрос.
// Реализация — adapter/oauth.ClientCredentials.
type TokenProvider interface {
	Token(ctx context.Context) (string, error)
}

// ASUClient abstracts integration with external АСУ systems.
type ASUClient interface {
	Pull(ctx context.Context, resource string) ([]byte, error)
	Push(ctx context.Context, resource string, payload []byte) error
}

// ReferenceClient — забор памяток на подачу/уборку у внешнего сервиса (тот же
// провайдер, что дислокация). Оба маршрута адресуются по клиенту: ByNumber — памятка
// по номеру, Update — инкремент с курсором last_update. Возвращает сырые байты
// ответа; разбор — выше.
//
// Памятку определяет ТРОЙКА «клиент + номер + дата создания» (контракт провайдера
// от 30.07.2026): номер повторяется у разных портов и переиспользуется внутри
// одного, поэтому ByNumber обязан нести dateCreate — дословное DATE_CREATE из
// инкремента по этой памятке.
type ReferenceClient interface {
	ByNumber(ctx context.Context, client, number, dateCreate string) ([]byte, error)
	Update(ctx context.Context, client, lastUpdate string) ([]byte, error)
}
