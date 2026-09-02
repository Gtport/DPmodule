// Package gu2b — HTTP-адаптер инкремента уведомлений ГУ-2б из внешнего сервиса
// (тот же провайдер, что дислокация и памятки). Реализует port.GU2BClient
// поверх маршрута провайдера (клиент — в пути, как у памяток):
//
//	GET <base_url>/<client>/gu2b/update?since=<t>&limit=<n>
//
// Контракт отдачи опубликован владельцем 17.08.2026 (см. docs/GU2B.md); на
// стороне провайдера ручка ещё не реализована — адаптер написан по контракту и
// заработает с её релизом. Авторизация — та же, что у памяток (провайдер один):
// основной режим — токен сервис-аккаунта Keycloak, запасной — X-API-Key.
// Клиент только достаёт сырые байты; разбор — parser.ParseGU2BUpdate.
package gu2b

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/Gtport/DPmodule/internal/port"
	"github.com/Gtport/DPmodule/pkg/logger"
)

const (
	authHeader     = "X-API-Key"
	defaultTimeout = 30 * time.Second
	maxBody        = 256 << 20 // 256 МБ страховочный лимит на тело ответа
)

// HTTPClient реализует port.GU2BClient.
type HTTPClient struct {
	baseURL       string
	authMode      string // "keycloak" | "apikey"; пусто → keycloak, если есть tokens
	authSecretKey string
	secrets       port.SecretSource
	tokens        port.TokenProvider
	hc            *http.Client
	call          logger.CallLog
}

// WithLogger подключает запись исходящих вызовов (см. одноимённый сеттер у
// адаптера памяток: логгер нужен в бою, тесты собирают клиент прежним вызовом).
func (c *HTTPClient) WithLogger(log *zap.Logger) *HTTPClient {
	c.call = c.call.WithLogger(log)
	return c
}

// NewHTTPClient — сборка по образцу адаптера памяток (провайдер и правила те же).
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
		call:          logger.NewCallLog(nil, logger.CompGU2B, "ГУ-2б", baseURL),
	}
}

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

// Update — инкремент по клиенту: GET <base>/<client>/gu2b/update?since=<t>&limit=<n>.
// since — дословный LAST_UPDATE прошлого непустого ответа («YYYY-MM-DD
// HH:MM:SS.sss»); «0» — полная перезаливка с начала накопления.
func (c *HTTPClient) Update(ctx context.Context, client, since string, limit int) ([]byte, error) {
	u := c.baseURL + "/" + url.PathEscape(client) + "/gu2b/update?since=" + url.QueryEscape(since) +
		"&limit=" + strconv.Itoa(limit)
	return c.get(ctx, u, "ГУ-2б update "+client,
		"инкремент ГУ-2б получен", zap.String("client", client))
}

// AuthMode — режим, который ФАКТИЧЕСКИ применится к запросу; только для логов.
func (c *HTTPClient) AuthMode() string {
	if c.useKeycloak() {
		return "keycloak"
	}
	if c.authSecretKey == "" {
		return "none"
	}
	return "apikey:" + authHeader
}

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
	req.Header.Set(authHeader, token)
	return nil
}

func (c *HTTPClient) get(ctx context.Context, u, label, okMsg string, extra ...zap.Field) ([]byte, error) {
	start := time.Now()

	fail := func(err error) ([]byte, error) {
		c.call.Failure(start, err, extra...)
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fail(fmt.Errorf("%s: сборка запроса: %w", label, err))
	}
	req.Header.Set("Accept", "application/json")
	if err := c.authorize(ctx, req, label); err != nil {
		return fail(err)
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return fail(fmt.Errorf("%s: запрос: %w", label, err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fail(fmt.Errorf("%s: чтение ответа: %w", label, err))
	}
	if resp.StatusCode != http.StatusOK {
		c.call.Failure(start, fmt.Errorf("статус %d: %s", resp.StatusCode, snippet(body)),
			append(extra, zap.Int("status", resp.StatusCode))...)
		return nil, fmt.Errorf("%s: статус %d: %s", label, resp.StatusCode, snippet(body))
	}

	c.call.Success(okMsg, start, len(body), extra...)
	return body, nil
}

func snippet(b []byte) string {
	const max = 200
	s := strings.TrimSpace(string(b))
	cut := len(s) > max
	if cut {
		s = s[:max]
	}
	// Обрез мог попасть на середину многобайтовой руны — чистим до валидного
	// UTF-8, иначе текст не ляжет в Postgres.
	s = strings.ToValidUTF8(s, "")
	if cut {
		s += "…"
	}
	return s
}
