package asu

import (
	"context"
	"fmt"
	"testing"

	"github.com/Gtport/DPmodule/internal/domain"
)

// staticToken — TokenProvider-заглушка: отдаёт готовый токен либо ошибку.
type staticToken struct {
	token string
	err   error
}

func (s staticToken) Token(context.Context) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.token, nil
}

// Основной режим после перевода провайдера: токен сервис-аккаунта Keycloak.
// Старый X-API-Key при этом не ставится, даже если ключ остался в настройках
// источника — иначе провайдер увидел бы обе авторизации сразу.
func TestPull_KeycloakBearer(t *testing.T) {
	srv, hdr, _ := captureServer(t, false)
	defer srv.Close()

	cfg := domain.DataSourceConfig{
		BaseURL:       srv.URL,
		AuthSecretKey: "ASU_TOKEN", // остаток прежней настройки
		AuthHeader:    "X-API-Key",
	}
	cl := NewHTTPClient(cfg, staticSecrets{"ASU_TOKEN": "SEKRET"}, staticToken{token: "KC-TOKEN"})

	if _, err := cl.Pull(context.Background(), "attis"); err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if got := hdr.Get("Authorization"); got != "Bearer KC-TOKEN" {
		t.Errorf("Authorization = %q, ждали 'Bearer KC-TOKEN'", got)
	}
	if got := hdr.Get("X-API-Key"); got != "" {
		t.Errorf("X-API-Key не должен ставиться в режиме keycloak, получили %q", got)
	}
}

// auth_mode: apikey — явный откат на прежнюю авторизацию, даже когда
// сервис-аккаунт настроен (провайдер ещё не перевёл этот маршрут).
func TestPull_ExplicitApiKeyModeWinsOverToken(t *testing.T) {
	srv, hdr, _ := captureServer(t, false)
	defer srv.Close()

	cfg := domain.DataSourceConfig{
		BaseURL:       srv.URL,
		AuthMode:      "apikey",
		AuthSecretKey: "ASU_TOKEN",
		AuthHeader:    "X-API-Key",
	}
	cl := NewHTTPClient(cfg, staticSecrets{"ASU_TOKEN": "SEKRET"}, staticToken{token: "KC-TOKEN"})

	if _, err := cl.Pull(context.Background(), "attis"); err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if got := hdr.Get("X-API-Key"); got != "SEKRET" {
		t.Errorf("X-API-Key = %q, ждали SEKRET", got)
	}
	if got := hdr.Get("Authorization"); got != "" {
		t.Errorf("Authorization не должен ставиться в режиме apikey, получили %q", got)
	}
}

// Сервис-аккаунт не настроен (tokens=nil) — стенд продолжает ходить по-старому.
// Это и есть страховка от «обновили код, а интеграция встала».
func TestPull_FallsBackToApiKeyWithoutServiceAccount(t *testing.T) {
	srv, hdr, _ := captureServer(t, false)
	defer srv.Close()

	cfg := domain.DataSourceConfig{BaseURL: srv.URL, AuthSecretKey: "ASU_TOKEN", AuthHeader: "X-API-Key"}
	cl := NewHTTPClient(cfg, staticSecrets{"ASU_TOKEN": "SEKRET"}, nil)

	if _, err := cl.Pull(context.Background(), "attis"); err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if got := hdr.Get("X-API-Key"); got != "SEKRET" {
		t.Errorf("X-API-Key = %q, ждали SEKRET", got)
	}
}

// Запрос 601 идёт к тому же провайдеру — авторизация та же.
func TestPullWagonHistory_KeycloakBearer(t *testing.T) {
	srv, hdr, path := captureServer(t, false)
	defer srv.Close()

	cl := NewHTTPClient(domain.DataSourceConfig{BaseURL: srv.URL}, staticSecrets{}, staticToken{token: "KC"})

	if _, err := cl.PullWagonHistory(context.Background(), "attis", "54117452", "2026-07-01", "2026-07-31"); err != nil {
		t.Fatalf("PullWagonHistory: %v", err)
	}
	if got := hdr.Get("Authorization"); got != "Bearer KC" {
		t.Errorf("Authorization = %q, ждали 'Bearer KC'", got)
	}
	if *path != "/wagons/54117452/history/attis" {
		t.Errorf("путь = %q", *path)
	}
}

// Keycloak не отдал токен — запрос НЕ уходит без авторизации: провайдер ответил
// бы 401, и причина потерялась бы за чужой ошибкой.
func TestPull_TokenErrorStopsRequest(t *testing.T) {
	srv, hdr, _ := captureServer(t, false)
	defer srv.Close()

	cl := NewHTTPClient(domain.DataSourceConfig{BaseURL: srv.URL}, staticSecrets{},
		staticToken{err: fmt.Errorf("keycloak: токен, статус 401: unauthorized_client")})

	_, err := cl.Pull(context.Background(), "attis")
	if err == nil {
		t.Fatal("ждали ошибку получения токена")
	}
	if hdr.Get("Authorization") != "" {
		t.Error("запрос не должен был уйти на провайдера")
	}
}

// AuthMode — то, что уходит в лог; проверяем все четыре исхода. Особенно важен
// последний: ключ без auth_header идёт в "Authorization: Bearer <ключ>" и в
// перехваченном запросе неотличим от токена Keycloak, поэтому лог обязан
// называть его иначе.
func TestAuthMode(t *testing.T) {
	cases := []struct {
		name   string
		cfg    domain.DataSourceConfig
		tokens *staticToken
		want   string
	}{
		{"сервис-аккаунт настроен, режим не задан", domain.DataSourceConfig{}, &staticToken{token: "t"}, "keycloak"},
		{"явный apikey сильнее токена",
			domain.DataSourceConfig{AuthMode: "apikey", AuthSecretKey: "ASU_TOKEN", AuthHeader: "X-API-Key"},
			&staticToken{token: "t"}, "apikey:X-API-Key"},
		{"без сервис-аккаунта — запасной ключ",
			domain.DataSourceConfig{AuthSecretKey: "ASU_TOKEN", AuthHeader: "X-API-Key"}, nil, "apikey:X-API-Key"},
		{"ключ без заголовка — статический Bearer, не JWT",
			domain.DataSourceConfig{AuthSecretKey: "ASU_TOKEN"}, nil, "apikey:bearer"},
		{"авторизации не просили вовсе", domain.DataSourceConfig{}, nil, "none"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var cl *HTTPClient
			if c.tokens == nil {
				cl = NewHTTPClient(c.cfg, staticSecrets{}, nil)
			} else {
				cl = NewHTTPClient(c.cfg, staticSecrets{}, *c.tokens)
			}
			if got := cl.AuthMode(); got != c.want {
				t.Errorf("AuthMode() = %q, ждали %q", got, c.want)
			}
		})
	}
}
