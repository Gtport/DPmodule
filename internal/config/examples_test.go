package config

import (
	"path/filepath"
	"testing"
)

// Примеры конфигов в репозитории должны разбираться тем же кодом, что и боевой:
// опечатка в YAML или переименованное поле иначе всплывают только на стенде,
// когда сервис уже не поднимается.
func TestRepoExampleConfigsParse(t *testing.T) {
	// postgres.enabled: true в шаблоне разработчика требует пароль — на машине
	// разработчика его даёт .env, здесь подставляем сами.
	t.Setenv("PG_PASSWORD", "test")

	cases := []struct {
		file string
		want func(*testing.T, *Config)
	}{
		{
			file: "config.docker.yaml",
			want: func(t *testing.T, cfg *Config) {
				if cfg.Keycloak.Issuer == "" || cfg.Keycloak.JWKSURL == "" {
					t.Error("keycloak: issuer и jwks_url обязаны быть заполнены")
				}
				// Секреты объявляются шаблоном Vault — значение подставляет CI/CD.
				// Пустое поле здесь означало бы, что стенд молча уедет на env.
				if cfg.Postgres.Password == "" {
					t.Error("postgres.password: ждали шаблон Vault (или значение)")
				}
			},
		},
		{
			file: "config.local.example.yaml",
			want: func(t *testing.T, cfg *Config) {
				// Машина разработчика: ВСЕ исходящие интеграции выключены —
				// второй инстанс не должен слать формы в живые чаты MAX и
				// дублировать забор данных у провайдера.
				if cfg.ASU.Enabled || cfg.Reference.Enabled || cfg.MAX.Enabled ||
					cfg.WagonOps.Enabled || cfg.Bros.Enabled {
					t.Error("в шаблоне разработчика исходящие интеграции обязаны быть выключены")
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			cfg, err := Load(filepath.Join("..", "..", tc.file))
			if err != nil {
				t.Fatalf("Load(%s): %v", tc.file, err)
			}
			tc.want(t, cfg)
		})
	}
}
