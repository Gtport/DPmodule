package ha

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// Повторы в тестах мгновенные: проверяем ветки, а не умение ждать.
func TestMain(m *testing.M) {
	observerRetryIn = time.Millisecond
	os.Exit(m.Run())
}

func observer(t *testing.T, code int, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/role"
}

// Одиночный узел обязан работать полностью — так модуль жил до кластера, и
// наблюдателя на такой площадке нет вовсе. Этим путём идут оба боевых VPS и
// машина разработчика: их конфиги ключа mode не содержат.
func TestStandaloneВсегдаActive(t *testing.T) {
	for _, mode := range []string{"standalone", "", "STANDALONE", " standalone "} {
		got, err := Resolve(context.Background(), mode, "")
		if err != nil || !got.IsActive() {
			t.Errorf("режим %q: получили %q, ошибка %v", mode, got, err)
		}
	}
}

// Форму ответа нам не назвали, поэтому принимаем обе.
func TestРазборОтветаНаблюдателя(t *testing.T) {
	cases := []struct {
		body string
		want Role
	}{
		{"active", RoleActive},
		{"active\n", RoleActive},
		{"ACTIVE", RoleActive},
		{"standby", RoleStandby},
		{`{"role":"active"}`, RoleActive},
		{`{"role":"standby"}`, RoleStandby},
		{`"active"`, RoleActive},
	}
	for _, c := range cases {
		got, err := Resolve(context.Background(), ModeCluster, observer(t, 200, c.body))
		if err != nil {
			t.Errorf("ответ %q: неожиданная ошибка %v", c.body, err)
		}
		if got != c.want {
			t.Errorf("ответ %q: получили %q, ожидали %q", c.body, got, c.want)
		}
	}
}

// Всё, чего мы не поняли, — standby. Два забора АСУ в один снимок и гонка за
// курсором памяток чинятся руками: лучше не сделает никто.
func TestНепонятныйОтветЭтоStandby(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"пустой ответ", observer(t, 200, "")},
		{"мусор", observer(t, 200, "мастер")},
		{"битый json", observer(t, 200, `{"role":`)},
		{"500", observer(t, 500, "active")},
		{"наблюдатель не отвечает", "http://127.0.0.1:1/role"},
		{"адрес не задан", ""},
	}
	for _, c := range cases {
		got, err := Resolve(context.Background(), ModeCluster, c.url)
		if got.IsActive() {
			t.Errorf("%s: узел решил, что он active", c.name)
		}
		if err == nil {
			t.Errorf("%s: ошибку надо вернуть, иначе о ней не напишут в лог", c.name)
		}
	}
}

// В конфиге адрес записан без схемы — так его прислал DevOps.
func TestАдресБезСхемы(t *testing.T) {
	full := observer(t, 200, "active")
	bare := full[len("http://"):]

	got, err := Resolve(context.Background(), ModeCluster, bare)
	if err != nil || !got.IsActive() {
		t.Errorf("адрес без схемы: получили %q, ошибка %v", got, err)
	}
}

// Живой ответ наблюдателя с Angara, снятый 20.08.2026 (перенесён из ветки
// тимлида вместе с пакетом). Формат нам не называли — закрепляем тот, что
// видели своими глазами, чтобы его смена не прошла молча.
func TestОтветНаблюдателяКакНаПроде(t *testing.T) {
	got, err := Resolve(context.Background(), ModeCluster,
		observer(t, 200, `{"role": "active", "server_id": "angara"}`))
	if err != nil || !got.IsActive() {
		t.Errorf("получили %q, ошибка %v", got, err)
	}
}

// Наблюдатель и наш контейнер поднимаются независимо: попасть в секунду, когда
// он ещё не готов, при выкатке легко. Одна попытка стоила бы смены без забора
// дислокации.
func TestПовторныеПопытки(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&n, 1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"role":"active"}`))
	}))
	defer srv.Close()

	got, err := Resolve(context.Background(), ModeCluster, srv.URL+"/role")
	if err != nil || !got.IsActive() {
		t.Errorf("наблюдатель ответил с третьей попытки, а мы сдались: %q, %v", got, err)
	}
	if n != 3 {
		t.Errorf("попыток было %d", n)
	}
}
