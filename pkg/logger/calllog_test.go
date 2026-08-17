package logger

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func observedCallLog(t *testing.T, component, name, baseURL string) (CallLog, *observer.ObservedLogs) {
	t.Helper()
	core, logs := observer.New(zap.DebugLevel)
	return NewCallLog(zap.New(core), component, name, baseURL), logs
}

// Строка исходящего вызова должна называть и предметную область, и контур:
// один провайдер отвечает нам с разных адресов на бою, стенде и у разработчика.
func TestCallLogFieldsNameAreaAndTarget(t *testing.T) {
	cl, logs := observedCallLog(t, CompDislocation, "АСУ", "https://10.0.0.5:8443/api")
	cl.Success("срез получен", time.Now().Add(-340*time.Millisecond), 1258291,
		zap.String("client", "attis"))

	rec := logs.All()[0]
	fields := rec.ContextMap()
	if fields[FieldComponent] != CompDislocation {
		t.Errorf("component: %v", fields[FieldComponent])
	}
	if fields[FieldDir] != DirOut {
		t.Errorf("dir: %v", fields[FieldDir])
	}
	if fields[FieldTarget] != "АСУ 10.0.0.5:8443" {
		t.Errorf("target должен нести имя и host:port, получено %v", fields[FieldTarget])
	}
	if fields["bytes"] != int64(1258291) {
		t.Errorf("объём должен оставаться числом: %v", fields["bytes"])
	}
	if _, ok := fields["took"]; !ok {
		t.Error("нет длительности вызова")
	}
}

// Повторяющийся отказ пишется один раз в окно. Без этого лежащий сутками
// провайдер дал бы ~290 одинаковых строк в сутки на клиента (крон АСУ — 5 мин).
func TestFailureRepeatsSuppressed(t *testing.T) {
	cl, logs := observedCallLog(t, CompDislocation, "АСУ", "https://10.0.0.5:8443")
	err := errors.New("dial tcp 10.0.0.5:8443: connect: connection refused")

	for i := 0; i < 10; i++ {
		cl.Failure(time.Now(), err, zap.String("client", "attis"))
	}

	if got := logs.Len(); got != 1 {
		t.Fatalf("ожидалась 1 строка на серию одинаковых отказов, получено %d", got)
	}
	rec := logs.All()[0]
	if rec.Message != "соединение отклонено" {
		t.Errorf("причина не переведена на человеческий: %q", rec.Message)
	}
	// Сырьё не теряется.
	if !strings.Contains(fmt.Sprint(rec.ContextMap()["error"]), "connection refused") {
		t.Errorf("полный текст ошибки потерян: %v", rec.ContextMap()["error"])
	}
}

// Отказы РАЗНЫХ клиентов гасить друг другом нельзя: адаптер один на всех, и
// поломка по второму кабинету не должна прятаться за поломкой по первому.
func TestFailureNotSuppressedAcrossClients(t *testing.T) {
	cl, logs := observedCallLog(t, CompDislocation, "АСУ", "https://10.0.0.5:8443")
	err := errors.New("connection refused")

	cl.Failure(time.Now(), err, zap.String("client", "attis"))
	cl.Failure(time.Now(), err, zap.String("client", "nmtp"))

	if got := logs.Len(); got != 2 {
		t.Errorf("отказы разных клиентов должны писаться оба, получено %d строк", got)
	}
}

// После успеха серия начинается заново — следующий сбой снова заметен сразу,
// а не через час молчания.
func TestSuccessResetsGate(t *testing.T) {
	cl, logs := observedCallLog(t, CompDislocation, "АСУ", "https://10.0.0.5:8443")
	err := errors.New("connection refused")

	cl.Failure(time.Now(), err)
	cl.Failure(time.Now(), err) // подавлен
	cl.Success("срез получен", time.Now(), 100)
	cl.Failure(time.Now(), err) // снова заметен

	var warns int
	for _, rec := range logs.All() {
		if rec.Level == zap.WarnLevel {
			warns++
		}
	}
	if warns != 2 {
		t.Errorf("после успеха отказ должен писаться заново: строк WARN %d, ожидалось 2", warns)
	}
}

// Подавленные повторы не пропадают бесследно: когда окно истекает, их число
// уходит полем — иначе по логу не видно, сколько длился простой.
func TestSuppressedCountReported(t *testing.T) {
	cl, logs := observedCallLog(t, CompDislocation, "АСУ", "https://10.0.0.5:8443")
	err := errors.New("connection refused")

	cl.Failure(time.Now(), err)
	for i := 0; i < 5; i++ {
		cl.Failure(time.Now(), err)
	}
	// Сдвигаем окно назад, как будто прошёл час.
	cl.gate.mu.Lock()
	for _, run := range cl.gate.seen {
		run.lastLogged = time.Now().Add(-2 * repeatWindow)
	}
	cl.gate.mu.Unlock()

	cl.Failure(time.Now(), err)

	last := logs.All()[logs.Len()-1]
	if last.ContextMap()["повторов"] != int64(5) {
		t.Errorf("число подавленных повторов не сообщено: %v", last.ContextMap())
	}
}

// Веерные вызовы уходят в DEBUG: очередь 601 ходит пачками по 50 вагонов, на
// INFO один проход забил бы файл.
func TestSuccessQuietIsDebug(t *testing.T) {
	cl, logs := observedCallLog(t, CompVagonops, "АСУ", "https://10.0.0.5:8443")
	cl.SuccessQuiet("история получена", time.Now(), 100, zap.String("vagon", "63499578"))

	if lvl := logs.All()[0].Level; lvl != zap.DebugLevel {
		t.Errorf("веерный успех должен быть DEBUG, получено %v", lvl)
	}
}

func TestReason(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{context.Canceled, "вызов отменён"},
		{context.DeadlineExceeded, "таймаут"},
		{errors.New(`Get "https://x": net/http: request canceled (Client.Timeout exceeded)`), "таймаут"},
		{errors.New("dial tcp: connect: connection refused"), "соединение отклонено"},
		{errors.New("read tcp: connection reset by peer"), "соединение разорвано"},
		{errors.New(`dial tcp: lookup foo: no such host`), "хост не найден"},
		{errors.New("x509: certificate signed by unknown authority"), "сертификат не принят"},
		{errors.New("что-то своё"), "вызов не удался"},
		{nil, "вызов не удался"},
	}
	for _, c := range cases {
		if got := Reason(c.err); got != c.want {
			t.Errorf("Reason(%v) = %q, ожидалось %q", c.err, got, c.want)
		}
	}
}

// Логгер не подключён (тесты, утилиты) — вызовы не должны падать.
func TestCallLogNilLoggerSafe(t *testing.T) {
	cl := NewCallLog(nil, CompDislocation, "АСУ", "")
	cl.Success("ок", time.Now(), 1)
	cl.Failure(time.Now(), errors.New("х"))
	if cl.Enabled() {
		t.Error("без логгера Enabled должен быть false")
	}
}

// Адрес без схемы (в конфигах встречается «host:port») тоже должен давать цель.
func TestHostOf(t *testing.T) {
	cases := map[string]string{
		"https://cargolk.rzd.ru":       "cargolk.rzd.ru",
		"https://10.0.0.5:8443/api/v1": "10.0.0.5:8443",
		"10.0.0.5:8443":                "10.0.0.5:8443",
		"":                             "",
	}
	for in, want := range cases {
		if got := hostOf(in); got != want {
			t.Errorf("hostOf(%q) = %q, ожидалось %q", in, got, want)
		}
	}
}
