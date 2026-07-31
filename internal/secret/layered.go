package secret

import (
	"context"
	"fmt"

	"github.com/Gtport/DPmodule/internal/port"
)

// LayeredSource — секреты из конфига поверх запасного источника (обычно env).
//
// Канон после перехода на Vault: секрет объявляется в config.yaml шаблоном
// ${vault:<движок>/<путь>:<ключ>}, значение подставляет CI/CD перед стартом, и
// приложение читает его из конфига как обычное поле. Адаптеры (АСУ, памятки,
// MAX) просят секрет ПО ИМЕНИ КЛЮЧА через port.SecretSource — этот слой и
// сводит два мира: сперва смотрит, что пришло из конфига, иначе идёт в env.
//
// Запасной путь нужен не для красоты: имя ключа к провайдеру АСУ хранится в БД
// (data_source.auth_secret_key) и может отличаться от того, под которым значение
// положено из конфига. Тогда сработает чтение окружения по имени из БД — ровно
// как было до Vault.
type LayeredSource struct {
	static   map[string]string
	fallback port.SecretSource
}

// NewLayered собирает источник: static — значения из конфига (пустые
// отбрасываются, чтобы не перекрывать окружение пустотой), fallback — куда идти,
// когда ключа нет; nil-fallback допустим (тогда ненайденный ключ = ошибка).
func NewLayered(static map[string]string, fallback port.SecretSource) *LayeredSource {
	m := make(map[string]string, len(static))
	for k, v := range static {
		if k == "" || v == "" {
			continue
		}
		m[k] = v
	}
	return &LayeredSource{static: m, fallback: fallback}
}

func (s *LayeredSource) Get(ctx context.Context, key string) (string, error) {
	if v, ok := s.static[key]; ok {
		return v, nil
	}
	if s.fallback == nil {
		return "", fmt.Errorf("secret %q not found in config", key)
	}
	return s.fallback.Get(ctx, key)
}
