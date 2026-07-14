// Package asu — HTTP-адаптер интеграции АСУ-АСУ: реализация port.ASUClient поверх
// REST-сервиса провайдера. Провайдер отдаёт снимок дислокации по маршруту
// <base_url>/<client>/dislocation в формате {timestamp,count,wagons} (envelope).
// Разбор тела — не здесь, а в parser.JSONParser; клиент только достаёт сырые байты.
package asu

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
)

const (
	defaultPathTemplate = "/{client}/dislocation"
	defaultTimeout      = 30 * time.Second
)

// HTTPClient реализует port.ASUClient: GET к сервису АСУ, авторизация секретом из
// SecretSource (ключ — auth_secret_key источника; пусто → без авторизации). Секрет
// уходит в заголовке auth_header (напр. "X-API-Key"); пустой auth_header → в
// "Authorization: Bearer <секрет>".
type HTTPClient struct {
	baseURL      string
	pathTemplate string
	method       string
	authKey      string
	authHeader   string
	secrets      port.SecretSource
	hc           *http.Client
}

// NewHTTPClient собирает клиент из config источника (base_url/path_template/method/
// timeout_secs/auth_secret_key/auth_header/insecure_tls) и SecretSource для токена к АСУ.
func NewHTTPClient(cfg domain.DataSourceConfig, secrets port.SecretSource) *HTTPClient {
	pathTemplate := cfg.PathTemplate
	if pathTemplate == "" {
		pathTemplate = defaultPathTemplate
	}
	method := strings.ToUpper(cfg.Method)
	if method == "" {
		method = http.MethodGet
	}
	timeout := defaultTimeout
	if cfg.TimeoutSecs > 0 {
		timeout = time.Duration(cfg.TimeoutSecs) * time.Second
	}
	hc := &http.Client{Timeout: timeout}
	// insecure_tls: провайдер отдаёт самоподписанный серт (напр. на IP:8443) —
	// отключаем проверку цепочки ТОЛЬКО для этого источника (эквивалент curl -k).
	if cfg.InsecureTLS {
		hc.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	return &HTTPClient{
		baseURL:      strings.TrimRight(cfg.BaseURL, "/"),
		pathTemplate: pathTemplate,
		method:       method,
		authKey:      cfg.AuthSecretKey,
		authHeader:   cfg.AuthHeader,
		secrets:      secrets,
		hc:           hc,
	}
}

// Pull забирает сырой снимок дислокации по коду клиента провайдера (resource →
// {client} в шаблоне пути). Возвращает тело ответа как есть; парсинг — выше.
func (c *HTTPClient) Pull(ctx context.Context, resource string) ([]byte, error) {
	url := c.baseURL + strings.ReplaceAll(c.pathTemplate, "{client}", resource)
	req, err := http.NewRequestWithContext(ctx, c.method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("АСУ %q: сборка запроса: %w", resource, err)
	}
	req.Header.Set("Accept", "application/json")
	if c.authKey != "" {
		token, err := c.secrets.Get(ctx, c.authKey)
		if err != nil {
			return nil, fmt.Errorf("АСУ %q: секрет %q: %w", resource, c.authKey, err)
		}
		if c.authHeader != "" {
			req.Header.Set(c.authHeader, token) // напр. X-API-Key: <ключ>
		} else {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("АСУ %q: запрос: %w", resource, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20)) // 256 МБ страховочный лимит
	if err != nil {
		return nil, fmt.Errorf("АСУ %q: чтение ответа: %w", resource, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("АСУ %q: статус %d: %s", resource, resp.StatusCode, snippet(body))
	}
	return body, nil
}

// Push пока не используется (обмен односторонний: только забор дислокации).
func (c *HTTPClient) Push(context.Context, string, []byte) error {
	return fmt.Errorf("АСУ Push: не реализовано")
}

func snippet(b []byte) string {
	const max = 200
	s := strings.TrimSpace(string(b))
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
