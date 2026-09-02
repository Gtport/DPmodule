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

// ProviderModeClient — режим источника данных провайдера (GET /wagons/history/source
// у rwgate): каким шлюзом провайдер добывает данные ПРЯМО СЕЙЧАС — asu (штатно),
// lk (фолбэк через робота ЛК РЖД) или paused (оба источника лежат). Провайдер
// сделал эту ручку именно для внешних серверов — чтобы им не дублировать его
// логику фолбэка АСУ↔ЛК. Возвращает сырые байты; разбор — в сервисе.
type ProviderModeClient interface {
	PullProviderMode(ctx context.Context) ([]byte, error)
}

// GU2BClient — инкремент уведомлений ГУ-2б (завершение грузовой операции =
// факт выгрузки) у того же провайдера: GET <base>/<client>/gu2b/update.
// Контракт отдачи опубликован владельцем 17.08.2026 (см. docs/GU2B.md);
// провайдер отдаёт уведомления, у которых max(created_at, updated_at) > since,
// не больше limit за ответ; since=0 — полная перезаливка. Возвращает сырые
// байты; разбор — parser.ParseGU2BUpdate.
type GU2BClient interface {
	Update(ctx context.Context, client, since string, limit int) ([]byte, error)
}
