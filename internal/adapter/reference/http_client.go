// Package reference — HTTP-адаптер забора памяток на подачу/уборку из внешнего
// сервиса (тот же провайдер, что дислокация). Реализует port.ReferenceClient
// поверх двух маршрутов провайдера (клиент — в пути, как у дислокации):
//
//	GET <base_url>/<client>/reference?number=<n>&date_create=<d> — памятка по тройке
//	GET <base_url>/<client>/reference/update?last_update=<t>     — инкремент по клиенту
//
// Авторизация — та же, что у дислокации (провайдер один): основной режим —
// access-токен сервис-аккаунта Keycloak в "Authorization: Bearer", запасной —
// заголовок X-API-Key с ключом из SecretSource. Клиент только достаёт сырые
// байты; разбор JSON — не здесь (на этом этапе разбор ещё не подключён).
package reference

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Gtport/DPmodule/internal/port"
)

const (
	authHeader     = "X-API-Key"
	defaultTimeout = 30 * time.Second
	maxBody        = 256 << 20 // 256 МБ страховочный лимит на тело ответа
)

// HTTPClient реализует port.ReferenceClient.
type HTTPClient struct {
	baseURL       string
	authMode      string // "keycloak" | "apikey"; пусто → keycloak, если есть tokens
	authSecretKey string // режим apikey: ключ секрета в SecretSource; пусто → без авторизации
	secrets       port.SecretSource
	tokens        port.TokenProvider
	hc            *http.Client
}

// NewHTTPClient собирает клиент из base_url провайдера, флага insecureTLS
// (самоподписанный серт), режима авторизации, имени ключа секрета (запасной режим)
// и источников секрета/токена. tokens=nil — сервис-аккаунт не настроен, тогда
// работает X-API-Key, как до перевода провайдера на Keycloak.
func NewHTTPClient(baseURL string, insecureTLS bool, authMode, authSecretKey string, secrets port.SecretSource, tokens port.TokenProvider) *HTTPClient {
	hc := &http.Client{Timeout: defaultTimeout}
	if insecureTLS {
		hc.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	return &HTTPClient{
		baseURL:       strings.TrimRight(baseURL, "/"),
		authMode:      strings.ToLower(strings.TrimSpace(authMode)),
		authSecretKey: authSecretKey,
		secrets:       secrets,
		tokens:        tokens,
		hc:            hc,
	}
}

// useKeycloak — см. одноимённое правило у адаптера АСУ: провайдер один, режим
// выбирается одинаково.
func (c *HTTPClient) useKeycloak() bool {
	switch c.authMode {
	case "keycloak":
		return true
	case "apikey":
		return false
	default:
		return c.tokens != nil
	}
}

// ByNumber — памятка по тройке «клиент + номер + дата создания»:
// GET <base>/<client>/reference?number=<n>&date_create=<d>.
//
// dateCreate передаём ДОСЛОВНО, как пришло в DATE_CREATE инкремента (MM-DD-YYYY;
// провайдер принимает и YYYY-MM-DD, но не DD-MM-YYYY — для чисел ≤ 12 он
// неотличим от MM-DD-YYYY, и вместо ошибки вернулась бы чужая памятка).
// Параметр ставим ВСЕГДА, даже пустым: у документа без даты создания DATE_CREATE
// приходит пустой строкой, и «пусто» — законное значение, тогда как отсутствие
// параметра провайдер отбивает 400.
func (c *HTTPClient) ByNumber(ctx context.Context, client, number, dateCreate string) ([]byte, error) {
	u := c.baseURL + "/" + url.PathEscape(client) + "/reference?number=" + url.QueryEscape(number) +
		"&date_create=" + url.QueryEscape(dateCreate)
	return c.get(ctx, u, "памятка "+client+" number="+number+" date_create="+dateCreate)
}

// Update — инкремент по клиенту: GET <base>/<client>/reference/update?last_update=<t>.
func (c *HTTPClient) Update(ctx context.Context, client, lastUpdate string) ([]byte, error) {
	u := c.baseURL + "/" + url.PathEscape(client) + "/reference/update?last_update=" + url.QueryEscape(lastUpdate)
	return c.get(ctx, u, "памятки update "+client)
}

// AuthMode — режим, который ФАКТИЧЕСКИ применится к запросу; только для логов.
// Пустой auth_mode сам по себе ничего не говорит: он значит keycloak или apikey
// в зависимости от того, настроен ли сервис-аккаунт.
func (c *HTTPClient) AuthMode() string {
	if c.useKeycloak() {
		return "keycloak"
	}
	if c.authSecretKey == "" {
		return "none"
	}
	return "apikey:" + authHeader
}

// authorize вешает авторизацию согласно режиму (основной — токен сервис-аккаунта).
func (c *HTTPClient) authorize(ctx context.Context, req *http.Request, label string) error {
	if c.useKeycloak() {
		if c.tokens == nil {
			return fmt.Errorf("%s: режим keycloak, но сервис-аккаунт не настроен (keycloak.service_account)", label)
		}
		token, err := c.tokens.Token(ctx)
		if err != nil {
			return fmt.Errorf("%s: токен сервис-аккаунта: %w", label, err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	}

	if c.authSecretKey == "" {
		return nil
	}
	token, err := c.secrets.Get(ctx, c.authSecretKey)
	if err != nil {
		return fmt.Errorf("%s: секрет %q: %w", label, c.authSecretKey, err)
	}
	req.Header.Set(authHeader, token) // X-API-Key: <ключ>
	return nil
}

func (c *HTTPClient) get(ctx context.Context, u, label string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: сборка запроса: %w", label, err)
	}
	req.Header.Set("Accept", "application/json")
	if err := c.authorize(ctx, req, label); err != nil {
		return nil, err
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: запрос: %w", label, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("%s: чтение ответа: %w", label, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: статус %d: %s", label, resp.StatusCode, snippet(body))
	}
	return body, nil
}

func snippet(b []byte) string {
	const max = 200
	s := strings.TrimSpace(string(b))
	cut := len(s) > max
	if cut {
		s = s[:max]
	}
	// Обрез мог попасть на середину многобайтовой руны (русский текст ошибки
	// провайдера) — чистим до валидного UTF-8, иначе текст не ляжет в Postgres.
	s = strings.ToValidUTF8(s, "")
	if cut {
		s += "…"
	}
	return s
}
