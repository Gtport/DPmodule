package reference

import (
	"context"
	"testing"
)

// staticToken — TokenProvider-заглушка.
type staticToken struct{ token string }

func (s staticToken) Token(context.Context) (string, error) { return s.token, nil }

// Памятки идут к тому же провайдеру, что дислокация, — авторизация общая:
// после перевода это токен сервис-аккаунта, а не X-API-Key.
func TestUpdate_KeycloakBearer(t *testing.T) {
	srv, got := captureServer(t, false)
	defer srv.Close()

	cl := NewHTTPClient(srv.URL, false, "", "ASU_TOKEN",
		staticSecrets{"ASU_TOKEN": "SEKRET"}, staticToken{token: "KC-TOKEN"})

	if _, err := cl.Update(context.Background(), "attis", "2026-07-08 00:00:00.000"); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if h := got.Header.Get("Authorization"); h != "Bearer KC-TOKEN" {
		t.Errorf("Authorization = %q, ждали 'Bearer KC-TOKEN'", h)
	}
	if h := got.Header.Get("X-API-Key"); h != "" {
		t.Errorf("X-API-Key не должен ставиться в режиме keycloak, получили %q", h)
	}
}

// Явный apikey — откат на прежнюю авторизацию при настроенном сервис-аккаунте.
func TestUpdate_ExplicitApiKeyMode(t *testing.T) {
	srv, got := captureServer(t, false)
	defer srv.Close()

	cl := NewHTTPClient(srv.URL, false, "apikey", "ASU_TOKEN",
		staticSecrets{"ASU_TOKEN": "SEKRET"}, staticToken{token: "KC-TOKEN"})

	if _, err := cl.Update(context.Background(), "attis", ""); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if h := got.Header.Get("X-API-Key"); h != "SEKRET" {
		t.Errorf("X-API-Key = %q, ждали SEKRET", h)
	}
	if h := got.Header.Get("Authorization"); h != "" {
		t.Errorf("Authorization не должен ставиться в режиме apikey, получили %q", h)
	}
}
