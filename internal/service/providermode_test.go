package service

import (
	"context"
	"errors"
	"testing"
)

func TestParseProviderMode(t *testing.T) {
	cases := []struct {
		name, raw, want string
		wantErr         bool
	}{
		{"JSON штатный", `{"source":"asu"}`, ProviderSourceASU, false},
		{"JSON с server_id (живой ответ rwgate)", `{"source": "lk", "server_id": "angara"}`, ProviderSourceLK, false},
		{"paused", `{"source":"paused"}`, ProviderSourcePaused, false},
		{"голый текст", "asu", ProviderSourceASU, false},
		{"текст в кавычках", `"lk"`, ProviderSourceLK, false},
		{"регистр и пробелы", "  ASU \n", ProviderSourceASU, false},
		{"неизвестное значение — не asu", `{"source":"maybe"}`, "", true},
		{"пусто", "", "", true},
		{"битый JSON", `{"source":`, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseProviderMode([]byte(c.raw))
			if c.wantErr {
				if err == nil {
					t.Fatalf("ждали ошибку, получили %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("неожиданная ошибка: %v", err)
			}
			if got != c.want {
				t.Fatalf("режим = %q, ждали %q", got, c.want)
			}
		})
	}
}

// stubModeClient — счётчик походов: проверяем, что кэш держит TTL.
type stubModeClient struct {
	raw   []byte
	err   error
	calls int
}

func (s *stubModeClient) PullProviderMode(context.Context) ([]byte, error) {
	s.calls++
	return s.raw, s.err
}

func TestProviderMode_CacheAndFallbackToUnknown(t *testing.T) {
	cl := &stubModeClient{raw: []byte(`{"source":"asu"}`)}
	svc := NewProviderModeService(cl, nil)

	m := svc.Mode(context.Background())
	if !m.IsASU() || !m.OK {
		t.Fatalf("первый ответ: %+v, ждали asu/ok", m)
	}
	// Повтор в пределах TTL — из кэша, к провайдеру не ходим.
	svc.Mode(context.Background())
	if cl.calls != 1 {
		t.Fatalf("походов %d, ждали 1 (кэш минуту)", cl.calls)
	}

	// Отказ провайдера сворачивается в unknown, не в ошибку: неизвестный режим —
	// не разрешение автоматике работать.
	cl2 := &stubModeClient{err: errors.New("connection refused")}
	svc2 := NewProviderModeService(cl2, nil)
	m2 := svc2.Mode(context.Background())
	if m2.Source != ProviderSourceUnknown || m2.OK || m2.IsASU() {
		t.Fatalf("отказ провайдера: %+v, ждали unknown/!ok", m2)
	}
}
