package reference

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type staticSecrets map[string]string

func (s staticSecrets) Get(_ context.Context, key string) (string, error) { return s[key], nil }

// captureServer запоминает заголовки, путь и raw-query последнего запроса.
func captureServer(t *testing.T, tls bool) (*httptest.Server, *http.Request) {
	t.Helper()
	got := &http.Request{}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*got = *r
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"LAST_UPDATE":"2026-07-10 12:30:03.058","PAMYATKI":{}}`))
	})
	if tls {
		return httptest.NewTLSServer(h), got
	}
	return httptest.NewServer(h), got
}

func TestByNumber(t *testing.T) {
	srv, got := captureServer(t, false)
	defer srv.Close()

	cl := NewHTTPClient(srv.URL, false, "ASU_TOKEN", staticSecrets{"ASU_TOKEN": "SEKRET"})
	if _, err := cl.ByNumber(context.Background(), "10272"); err != nil {
		t.Fatalf("ByNumber: %v", err)
	}
	if got.URL.Path != "/reference" {
		t.Fatalf("путь: ждали /reference, получили %q", got.URL.Path)
	}
	if q := got.URL.Query().Get("number"); q != "10272" {
		t.Fatalf("number: ждали 10272, получили %q", q)
	}
	if h := got.Header.Get("X-API-Key"); h != "SEKRET" {
		t.Fatalf("X-API-Key: ждали SEKRET, получили %q", h)
	}
}

func TestUpdate(t *testing.T) {
	srv, got := captureServer(t, false)
	defer srv.Close()

	cl := NewHTTPClient(srv.URL, false, "ASU_TOKEN", staticSecrets{"ASU_TOKEN": "SEKRET"})
	if _, err := cl.Update(context.Background(), "attis", "2026-07-08 00:00:00.000"); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.URL.Path != "/reference/update/attis" {
		t.Fatalf("путь: ждали /reference/update/attis, получили %q", got.URL.Path)
	}
	if q := got.URL.Query().Get("last_update"); q != "2026-07-08 00:00:00.000" {
		t.Fatalf("last_update: ждали '2026-07-08 00:00:00.000', получили %q", q)
	}
	if h := got.Header.Get("X-API-Key"); h != "SEKRET" {
		t.Fatalf("X-API-Key: ждали SEKRET, получили %q", h)
	}
}

func TestInsecureTLS(t *testing.T) {
	srv, _ := captureServer(t, true) // самоподписанный серт
	defer srv.Close()

	strict := NewHTTPClient(srv.URL, false, "", staticSecrets{})
	if _, err := strict.ByNumber(context.Background(), "1"); err == nil {
		t.Fatal("ждали ошибку TLS-проверки для самоподписанного серта")
	}

	lax := NewHTTPClient(srv.URL, true, "", staticSecrets{})
	if _, err := lax.ByNumber(context.Background(), "1"); err != nil {
		t.Fatalf("с insecure_tls запрос должен пройти: %v", err)
	}
}
