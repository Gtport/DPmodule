package secret

import (
	"context"
	"testing"
)

func TestLayered_ConfigWinsOverFallback(t *testing.T) {
	t.Setenv("ASU_TOKEN", "из-окружения")

	s := NewLayered(map[string]string{"ASU_TOKEN": "из-конфига"}, NewEnvSource())

	got, err := s.Get(context.Background(), "ASU_TOKEN")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "из-конфига" {
		t.Errorf("Get = %q, ждали значение из конфига", got)
	}
}

// Имя ключа у источника в data_source может отличаться от того, под которым
// значение положено из конфига — тогда работает запасной путь через окружение.
func TestLayered_FallbackForUnknownKey(t *testing.T) {
	t.Setenv("PROVIDER_TOKEN_2", "из-окружения")

	s := NewLayered(map[string]string{"ASU_TOKEN": "из-конфига"}, NewEnvSource())

	got, err := s.Get(context.Background(), "PROVIDER_TOKEN_2")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "из-окружения" {
		t.Errorf("Get = %q, ждали запасной путь из окружения", got)
	}
}

// Пустое значение в конфиге не должно перекрывать окружение пустотой:
// незаполненный шаблон Vault иначе молча гасил бы рабочий env.
func TestLayered_EmptyConfigValueDoesNotShadowEnv(t *testing.T) {
	t.Setenv("MAX_BOT_TOKEN", "из-окружения")

	s := NewLayered(map[string]string{"MAX_BOT_TOKEN": ""}, NewEnvSource())

	got, err := s.Get(context.Background(), "MAX_BOT_TOKEN")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "из-окружения" {
		t.Errorf("Get = %q, пустое значение конфига не должно перекрывать env", got)
	}
}

func TestLayered_MissingEverywhereIsError(t *testing.T) {
	t.Setenv("NOPE", "")

	s := NewLayered(nil, NewEnvSource())

	if _, err := s.Get(context.Background(), "NOPE"); err == nil {
		t.Fatal("ждали ошибку: секрета нет ни в конфиге, ни в окружении")
	}
}
