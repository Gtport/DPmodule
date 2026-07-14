package asu

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Gtport/DPmodule/internal/domain"
)

// staticSecrets — SecretSource-заглушка: отдаёт значение по ключу.
type staticSecrets map[string]string

func (s staticSecrets) Get(_ context.Context, key string) (string, error) { return s[key], nil }

// captureServer поднимает httptest-сервер, запоминает заголовки и путь последнего
// запроса и отдаёт валидный envelope дислокации.
func captureServer(t *testing.T, tls bool) (*httptest.Server, *http.Header, *string) {
	t.Helper()
	var gotHdr http.Header
	var gotPath string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHdr = r.Header.Clone()
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"timestamp":"2026-07-14T10:00:00","count":0,"wagons":[]}`))
	})
	if tls {
		return httptest.NewTLSServer(h), &gotHdr, &gotPath
	}
	return httptest.NewServer(h), &gotHdr, &gotPath
}

func TestPull_APIKeyHeader(t *testing.T) {
	srv, hdr, path := captureServer(t, false)
	defer srv.Close()

	cfg := domain.DataSourceConfig{
		BaseURL:       srv.URL,
		Clients:       []string{"attis", "nmtp"},
		AuthSecretKey: "ASU_TOKEN",
		AuthHeader:    "X-API-Key",
	}
	cl := NewHTTPClient(cfg, staticSecrets{"ASU_TOKEN": "SEKRET"})

	if _, err := cl.Pull(context.Background(), "attis"); err != nil {
		t.Fatalf("Pull attis: %v", err)
	}
	if got := hdr.Get("X-API-Key"); got != "SEKRET" {
		t.Fatalf("X-API-Key: ждали SEKRET, получили %q", got)
	}
	if got := hdr.Get("Authorization"); got != "" {
		t.Fatalf("при auth_header=X-API-Key Authorization не должен ставиться, получили %q", got)
	}
	if *path != "/attis/dislocation" {
		t.Fatalf("путь: ждали /attis/dislocation, получили %q", *path)
	}
}

func TestPull_DefaultBearer(t *testing.T) {
	srv, hdr, _ := captureServer(t, false)
	defer srv.Close()

	cfg := domain.DataSourceConfig{BaseURL: srv.URL, AuthSecretKey: "ASU_TOKEN"} // auth_header пуст
	cl := NewHTTPClient(cfg, staticSecrets{"ASU_TOKEN": "TOK"})

	if _, err := cl.Pull(context.Background(), "nmtp"); err != nil {
		t.Fatalf("Pull nmtp: %v", err)
	}
	if got := hdr.Get("Authorization"); got != "Bearer TOK" {
		t.Fatalf("Authorization: ждали 'Bearer TOK', получили %q", got)
	}
	if got := hdr.Get("X-API-Key"); got != "" {
		t.Fatalf("X-API-Key не должен ставиться в дефолтном режиме, получили %q", got)
	}
}

func TestPull_InsecureTLS(t *testing.T) {
	srv, _, _ := captureServer(t, true) // самоподписанный серт
	defer srv.Close()

	// Без insecure_tls → проверка серта проваливается.
	strict := NewHTTPClient(domain.DataSourceConfig{BaseURL: srv.URL}, staticSecrets{})
	if _, err := strict.Pull(context.Background(), "attis"); err == nil {
		t.Fatal("ждали ошибку TLS-проверки для самоподписанного серта, её нет")
	}

	// С insecure_tls → проходит (эквивалент curl -k).
	lax := NewHTTPClient(domain.DataSourceConfig{BaseURL: srv.URL, InsecureTLS: true}, staticSecrets{})
	if _, err := lax.Pull(context.Background(), "attis"); err != nil {
		t.Fatalf("с insecure_tls запрос должен пройти: %v", err)
	}
}
