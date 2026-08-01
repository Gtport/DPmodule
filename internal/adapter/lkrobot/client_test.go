package lkrobot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeLK — заглушка личного кабинета: повторяет наблюдаемое поведение боевого
// (csrf в <meta>, вход JSON-ом, заказ отчёта, таблица в опросе, xlsx по PUT).
type fakeLK struct {
	password  string
	orders    int
	pollsLeft int    // сколько раз ответить «ещё не готово»
	gotOKPO   string // ОКПО из заказа — проверяем формат
	gotCars   []string
}

func (f *fakeLK) handler() http.Handler {
	mux := http.NewServeMux()
	page := `<html><head><meta name="csrf-token" content="tok-123"></head><body></body></html>`

	mux.HandleFunc("/sign_in", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			io.WriteString(w, page)
			return
		}
		var body struct {
			User struct {
				Query    string `json:"query"`
				Password string `json:"password"`
			} `json:"user"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.User.Password != f.password {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusCreated)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, page)
	})

	mux.HandleFunc("/api/v1/services/asoup/reports", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-CSRF-Token") == "" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		var body struct {
			Params struct {
				Receivers []string `json:"receivers"`
			} `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if len(body.Params.Receivers) > 0 {
			f.gotOKPO = body.Params.Receivers[0]
		}
		f.orders++
		io.WriteString(w, `{"data":{"id":777}}`)
	})

	mux.HandleFunc("/api/v1/services/asoup/reports/777", func(w http.ResponseWriter, r *http.Request) {
		if f.pollsLeft > 0 {
			f.pollsLeft--
			io.WriteString(w, `{"data":{"status":"processing"}}`)
			return
		}
		// Номер вагона строкой, второй — числом: кабинет отдаёт и так, и так.
		io.WriteString(w, `{"data":{"status":"done","data":{
			"head":["NOM_NAK","NOM_VAG"],
			"body":[["ЭЧ1","60411014"],["ЭЧ2",68123462]]}}}`)
	})

	mux.HandleFunc("/api/v1/services/asoup/reports/777.xlsx", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Objects      []string        `json:"objects"`
			CustomFields json.RawMessage `json:"custom_fields"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		f.gotCars = body.Objects
		if len(body.CustomFields) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Write([]byte("PK\x03\x04содержимое"))
	})
	return mux
}

func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	c, err := New(Options{
		BaseURL:     srv.URL,
		ServiceID:   226,
		Timeout:     5 * time.Second,
		PollEvery:   time.Millisecond,
		PollTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("клиент не создан: %v", err)
	}
	return c
}

// Полный цикл: вход, заказ, ожидание готовности, выгрузка.
func TestFetchFullCycle(t *testing.T) {
	fake := &fakeLK{password: "secret", pollsLeft: 2}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.Login(context.Background(), "login", "secret"); err != nil {
		t.Fatalf("вход не прошёл: %v", err)
	}
	file, rows, err := c.Fetch(context.Background(), "01126022")
	if err != nil {
		t.Fatalf("выгрузка не прошла: %v", err)
	}
	if rows != 2 {
		t.Errorf("вагонов: получили %d, ждали 2", rows)
	}
	if !strings.HasPrefix(string(file), "PK") {
		t.Errorf("файл не похож на xlsx: %q", string(file[:2]))
	}
	// ОКПО уходит дословно, ведущий ноль не теряется (боевой кабинет отвергает 422).
	if fake.gotOKPO != "01126022" {
		t.Errorf("ОКПО в заказе: %q, ждали 01126022", fake.gotOKPO)
	}
	// «Отметить все»: в выгрузку уходят все вагоны таблицы, число тоже строкой.
	if len(fake.gotCars) != 2 || fake.gotCars[0] != "60411014" || fake.gotCars[1] != "68123462" {
		t.Errorf("вагоны в выгрузке: %v", fake.gotCars)
	}
}

// Неверный пароль отличается от «кабинет недоступен»: диспетчеру нужен разный текст.
func TestLoginWrongPassword(t *testing.T) {
	fake := &fakeLK{password: "secret"}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	err := newTestClient(t, srv).Login(context.Background(), "login", "мимо")
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("ждали ErrAuth, получили %v", err)
	}
}

// Отчёт не подготовился за отведённое время — не молчим и не ждём вечно.
func TestFetchNotReady(t *testing.T) {
	fake := &fakeLK{password: "secret", pollsLeft: 1 << 30}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	c := newTestClient(t, srv)
	c.opt.PollTimeout = 10 * time.Millisecond
	if err := c.Login(context.Background(), "login", "secret"); err != nil {
		t.Fatalf("вход не прошёл: %v", err)
	}
	if _, _, err := c.Fetch(context.Background(), "10230304"); !errors.Is(err, ErrNotReady) {
		t.Fatalf("ждали ErrNotReady, получили %v", err)
	}
}
