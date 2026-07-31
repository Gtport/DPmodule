package oauth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Gtport/DPmodule/internal/clock"
)

// staticSecrets — SecretSource-заглушка.
type staticSecrets map[string]string

func (s staticSecrets) Get(_ context.Context, key string) (string, error) {
	v, ok := s[key]
	if !ok {
		return "", fmt.Errorf("нет секрета %q", key)
	}
	return v, nil
}

// tokenServer отдаёт токен и считает обращения; форму запроса запоминает.
func tokenServer(t *testing.T, ttl int) (*httptest.Server, *int32, *map[string][]string) {
	t.Helper()
	var calls int32
	form := map[string][]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if err := r.ParseForm(); err != nil {
			t.Errorf("разбор формы: %v", err)
		}
		for k, v := range r.PostForm {
			form[k] = v
		}
		n := atomic.LoadInt32(&calls)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"access_token":"TOK-%d","expires_in":%d}`, n, ttl)
	}))
	return srv, &calls, &form
}

func TestToken_ClientCredentialsRequest(t *testing.T) {
	srv, calls, form := tokenServer(t, 300)
	defer srv.Close()

	ts := New(srv.URL, "dpport-service", "KEYCLOAK_SA_CLIENT_SECRET", "",
		staticSecrets{"KEYCLOAK_SA_CLIENT_SECRET": "SEKRET"}, false, 0)

	got, err := ts.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if got != "TOK-1" {
		t.Errorf("токен = %q, ждали TOK-1", got)
	}
	if *calls != 1 {
		t.Errorf("обращений к Keycloak = %d, ждали 1", *calls)
	}
	f := *form
	if v := f["grant_type"]; len(v) != 1 || v[0] != "client_credentials" {
		t.Errorf("grant_type = %v, ждали client_credentials", v)
	}
	if v := f["client_id"]; len(v) != 1 || v[0] != "dpport-service" {
		t.Errorf("client_id = %v", v)
	}
	if v := f["client_secret"]; len(v) != 1 || v[0] != "SEKRET" {
		t.Errorf("client_secret не тот, что дал SecretSource: %v", v)
	}
}

// Кэш: провайдера дёргают часто (крон + очередь 601), за токеном ходим один раз.
func TestToken_CachedUntilExpiry(t *testing.T) {
	srv, calls, _ := tokenServer(t, 300)
	defer srv.Close()

	ts := New(srv.URL, "dpport-service", "K", "", staticSecrets{"K": "S"}, false, 0)

	for i := 0; i < 3; i++ {
		if _, err := ts.Token(context.Background()); err != nil {
			t.Fatalf("Token #%d: %v", i, err)
		}
	}
	if *calls != 1 {
		t.Errorf("обращений = %d, ждали 1 (остальные — из кэша)", *calls)
	}
}

// Истёк — идём за новым. Время двигаем через clock (единый источник «сейчас»).
func TestToken_RefreshedAfterExpiry(t *testing.T) {
	srv, calls, _ := tokenServer(t, 60) // ttl 60s, запас 30s → кэш живёт 30s
	defer srv.Close()

	base := time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC)
	restore := clock.SetForTest(base)
	defer restore()

	ts := New(srv.URL, "dpport-service", "K", "", staticSecrets{"K": "S"}, false, 0)

	first, err := ts.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}

	restore()
	restore = clock.SetForTest(base.Add(31 * time.Second))

	second, err := ts.Token(context.Background())
	if err != nil {
		t.Fatalf("Token после истечения: %v", err)
	}
	if first == second {
		t.Errorf("токен не обновился после истечения: %q", second)
	}
	if *calls != 2 {
		t.Errorf("обращений = %d, ждали 2", *calls)
	}
}

// Отказ Keycloak виден целиком: без тела ответа причина («unauthorized_client»)
// не восстанавливается по логу.
func TestToken_ErrorCarriesProviderReply(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":"unauthorized_client"}`)
	}))
	defer srv.Close()

	ts := New(srv.URL, "dpport-service", "K", "", staticSecrets{"K": "S"}, false, 0)

	_, err := ts.Token(context.Background())
	if err == nil {
		t.Fatal("ждали ошибку на статус 401")
	}
	if !strings.Contains(err.Error(), "unauthorized_client") {
		t.Errorf("ошибка не несёт ответ Keycloak: %v", err)
	}
}

func TestToken_NotConfigured(t *testing.T) {
	ts := New("", "", "K", "", staticSecrets{"K": "S"}, false, 0)

	if _, err := ts.Token(context.Background()); err == nil {
		t.Fatal("ждали ошибку: сервис-аккаунт не настроен")
	}
}
