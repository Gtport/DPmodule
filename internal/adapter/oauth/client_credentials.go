// Package oauth — получение access-токена для ИСХОДЯЩИХ запросов к провайдеру.
// Поток client_credentials: сервис-аккаунт Keycloak, никакого человека в цикле —
// поэтому годится и фоновым кронам (забор дислокации, очередь 601, памятки).
//
// Токен кэшируется до истечения: провайдера дёргают часто (крон 10 минут + очередь
// пачками по 50), и ходить за токеном на каждый запрос незачем.
package oauth

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/port"
	"github.com/Gtport/DPmodule/pkg/logger"
)

const (
	defaultTimeout = 30 * time.Second
	// Запас перед истечением: токен, живущий последние секунды, в пути протухнет.
	expiryMargin = 30 * time.Second
	maxBody      = 1 << 20
)

// ClientCredentials реализует port.TokenProvider поверх token-эндпоинта Keycloak.
type ClientCredentials struct {
	tokenURL  string
	clientID  string
	secretKey string // имя ключа секрета в SecretSource
	scope     string
	secrets   port.SecretSource
	hc        *http.Client
	call      logger.CallLog

	mu      sync.Mutex
	token   string
	expires time.Time // момент, после которого кэш непригоден (уже с запасом)
}

// New собирает источник токенов. secretKey — имя ключа в SecretSource (значение
// приходит из конфига шаблоном Vault либо из env запасным путём). insecureTLS —
// для стендов с самоподписанным сертом, как у прочих клиентов провайдера.
func New(tokenURL, clientID, secretKey, scope string, secrets port.SecretSource, insecureTLS bool, timeout time.Duration) *ClientCredentials {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	hc := &http.Client{Timeout: timeout}
	if insecureTLS {
		hc.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	return &ClientCredentials{
		tokenURL:  strings.TrimSpace(tokenURL),
		clientID:  clientID,
		secretKey: secretKey,
		scope:     scope,
		secrets:   secrets,
		hc:        hc,
		// Компонент auth: получение токена — не дислокация и не памятки, хотя
		// нужен он всем троим. Отказ Keycloak иначе выглядел бы поломкой той
		// подсистемы, которая первой попросила токен.
		call: logger.NewCallLog(nil, logger.CompAuth, "Keycloak", tokenURL),
	}
}

// WithLogger подключает запись походов за токеном.
func (c *ClientCredentials) WithLogger(log *zap.Logger) *ClientCredentials {
	c.call = c.call.WithLogger(log)
	return c
}

// Token отдаёт живой access-токен: из кэша, пока не истёк, иначе просит новый.
func (c *ClientCredentials) Token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// «Сейчас» — только через clock (инвариант проекта: московское naive-время).
	now := clock.Now().Time()
	if c.token != "" && now.Before(c.expires) {
		return c.token, nil
	}

	tok, ttl, err := c.fetch(ctx)
	if err != nil {
		return "", err
	}
	c.token = tok
	// Держим кэш чуть меньше выданного срока: запас на дорогу и часы стендов.
	if ttl > expiryMargin {
		ttl -= expiryMargin
	}
	c.expires = now.Add(ttl)
	return c.token, nil
}

func (c *ClientCredentials) fetch(ctx context.Context) (string, time.Duration, error) {
	start := time.Now()

	fail := func(err error) (string, time.Duration, error) {
		c.call.Failure(start, err, zap.String("client", c.clientID))
		return "", 0, err
	}

	if c.tokenURL == "" || c.clientID == "" {
		return fail(fmt.Errorf("keycloak: сервис-аккаунт не настроен (token_url/client_id пусты)"))
	}
	secret, err := c.secrets.Get(ctx, c.secretKey)
	if err != nil {
		return fail(fmt.Errorf("keycloak: секрет %q: %w", c.secretKey, err))
	}

	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.clientID},
		"client_secret": {secret},
	}
	if c.scope != "" {
		form.Set("scope", c.scope)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fail(fmt.Errorf("keycloak: сборка запроса токена: %w", err))
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return fail(fmt.Errorf("keycloak: запрос токена: %w", err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fail(fmt.Errorf("keycloak: чтение ответа токена: %w", err))
	}
	if resp.StatusCode != http.StatusOK {
		// Тело ответа Keycloak на ошибку короткое ({"error":"unauthorized_client"}) —
		// показываем как есть: без него причина отказа не восстанавливается.
		c.call.Failure(start, fmt.Errorf("статус %d: %s", resp.StatusCode, snippet(body)),
			zap.String("client", c.clientID), zap.Int("status", resp.StatusCode))
		return "", 0, fmt.Errorf("keycloak: токен, статус %d: %s", resp.StatusCode, snippet(body))
	}

	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return fail(fmt.Errorf("keycloak: разбор ответа токена: %w", err))
	}
	if out.AccessToken == "" {
		return fail(fmt.Errorf("keycloak: в ответе нет access_token"))
	}
	ttl := time.Duration(out.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = time.Minute // провайдер не сказал срок — перезапросим через минуту
	}
	// Токен берут кроны, а живёт он минутами: на INFO это была бы строка каждые
	// несколько минут круглосуточно. Успех — в DEBUG, отказ остаётся в WARN.
	c.call.SuccessQuiet("токен получен", start, len(body),
		zap.String("client", c.clientID), zap.Duration("ttl", ttl))
	return out.AccessToken, ttl, nil
}

func snippet(b []byte) string {
	const max = 200
	s := strings.TrimSpace(string(b))
	cut := len(s) > max
	if cut {
		s = s[:max]
	}
	s = strings.ToValidUTF8(s, "")
	if cut {
		s += "…"
	}
	return s
}
