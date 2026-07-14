// Package reference — HTTP-адаптер забора памяток на подачу/уборку из внешнего
// сервиса (тот же провайдер, что дислокация). Реализует port.ReferenceClient
// поверх двух маршрутов провайдера:
//
//	GET <base_url>/reference?number=<n>                     — памятка по номеру
//	GET <base_url>/reference/update/<client>?last_update=<t> — инкремент по клиенту
//
// Авторизация — заголовок X-API-Key (ключ из SecretSource). Клиент только достаёт
// сырые байты; разбор JSON — не здесь (на этом этапе разбор ещё не подключён).
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
	authSecretKey string // ключ секрета в SecretSource; пусто → без авторизации
	secrets       port.SecretSource
	hc            *http.Client
}

// NewHTTPClient собирает клиент из base_url провайдера, флага insecureTLS
// (самоподписанный серт), имени ключа секрета и SecretSource.
func NewHTTPClient(baseURL string, insecureTLS bool, authSecretKey string, secrets port.SecretSource) *HTTPClient {
	hc := &http.Client{Timeout: defaultTimeout}
	if insecureTLS {
		hc.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	return &HTTPClient{
		baseURL:       strings.TrimRight(baseURL, "/"),
		authSecretKey: authSecretKey,
		secrets:       secrets,
		hc:            hc,
	}
}

// ByNumber — памятка по номеру: GET <base>/reference?number=<n>.
func (c *HTTPClient) ByNumber(ctx context.Context, number string) ([]byte, error) {
	u := c.baseURL + "/reference?number=" + url.QueryEscape(number)
	return c.get(ctx, u, "памятка number="+number)
}

// Update — инкремент по клиенту: GET <base>/reference/update/<client>?last_update=<t>.
func (c *HTTPClient) Update(ctx context.Context, client, lastUpdate string) ([]byte, error) {
	u := c.baseURL + "/reference/update/" + url.PathEscape(client) + "?last_update=" + url.QueryEscape(lastUpdate)
	return c.get(ctx, u, "памятки update "+client)
}

func (c *HTTPClient) get(ctx context.Context, u, label string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: сборка запроса: %w", label, err)
	}
	req.Header.Set("Accept", "application/json")
	if c.authSecretKey != "" {
		token, err := c.secrets.Get(ctx, c.authSecretKey)
		if err != nil {
			return nil, fmt.Errorf("%s: секрет %q: %w", label, c.authSecretKey, err)
		}
		req.Header.Set(authHeader, token) // X-API-Key: <ключ>
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
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
