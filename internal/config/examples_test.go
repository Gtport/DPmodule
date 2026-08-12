package config

import (
	"path/filepath"
	"testing"
)

// Конфиги из репозитория должны разбираться тем же кодом, что и на стенде:
// опечатка в YAML или переименованное поле иначе всплывают только при деплое,
// когда сервис уже не поднимается. config.yaml забирает CI/CD, поэтому его
// разбор проверяем в первую очередь.
func TestRepoExampleConfigsParse(t *testing.T) {
	// postgres.enabled: true в шаблоне разработчика требует пароль — на машине
	// разработчика его даёт .env, здесь подставляем сами.
	t.Setenv("PG_PASSWORD", "test")

	cases := []struct {
		file string
		want func(*testing.T, *Config)
	}{
		{
			file: "config.yaml",
			want: func(t *testing.T, cfg *Config) {
				if cfg.Keycloak.Issuer == "" || cfg.Keycloak.JWKSURL == "" {
					t.Error("keycloak: issuer и jwks_url обязаны быть заполнены")
				}
				// Секреты объявляются шаблоном Vault — значение подставляет CI/CD.
				// Пустое поле здесь означало бы, что стенд молча уедет на env.
				if cfg.Postgres.Password == "" {
					t.Error("postgres.password: ждали шаблон Vault (или значение)")
				}
				// Общий realm стенда: без strict-режима легаси-роль чужого
				// пользователя контура превратилась бы в наши *_dpport (дыра,
				// закрытая в dc0a811). Тест — от случайной потери при слияниях
				// с ветками тимлида, где строки нет.
				if !cfg.Keycloak.StrictRoles {
					t.Error("keycloak.strict_roles обязан быть true в конфиге стенда")
				}
				// Тайлы карты на стенде — в общей базе, своя схема (MAP_TILES.md).
				// Пока DevOps не выдал имена базы и схемы, кэш выключен: карта
				// живёт без подложки OSM (тайлы — картинка, не данные), свой слой
				// ж/д сети виден. Сторожим не факт включения, а связку: включили —
				// значит schema заполнена, иначе search_path уедет в public и
				// тайлы молча не найдутся.
				if cfg.Tiles.Enabled && cfg.Tiles.Schema == "" {
					t.Error("tiles: при enabled: true обязана быть заполнена schema")
				}
			},
		},
		{
			file: "config.vps.yaml",
			want: func(t *testing.T, cfg *Config) {
				// Боевой VPS: свой realm, секреты приходят из env (systemd
				// EnvironmentFile), Vault-шаблонов в файле нет.
				if cfg.Keycloak.Issuer == "" || cfg.Keycloak.JWKSURL == "" {
					t.Error("keycloak: issuer и jwks_url обязаны быть заполнены")
				}
				// Кэш тайлов живёт на VPS отдельной базой tiles (MAP_TILES.md).
				if !cfg.Tiles.Enabled || cfg.Tiles.DBName != "tiles" {
					t.Error("tiles: на VPS ждали enabled: true и dbname: tiles")
				}
			},
		},
		{
			file: "config.river.yaml",
			want: func(t *testing.T, cfg *Config) {
				// Второй инстанс VPS (клиент river): Keycloak общий с боевым —
				// issuer обязан совпадать с боевым (сверяется дословно).
				if cfg.Keycloak.Issuer != "https://95850.koara.live/realms/iqport" {
					t.Error("keycloak.issuer обязан совпадать с боевым (общий realm)")
				}
				// Все внешние интеграции выключены: у клиента нет АСУ/MAX/памяток,
				// а их включение без своих записей data_source/max_chat слало бы
				// данные боевого провайдера.
				if cfg.ASU.Enabled || cfg.Reference.Enabled || cfg.MAX.Enabled ||
					cfg.WagonOps.Enabled || cfg.Bros.Enabled || cfg.LKRobot.Enabled {
					t.Error("у инстанса river внешние интеграции обязаны быть выключены")
				}
				// Порты не должны столкнуться с боевым инстансом (8080/9090).
				if cfg.HTTP.Port == 8080 || cfg.Metrics.Port == 9090 {
					t.Error("порты river обязаны отличаться от боевых 8080/9090")
				}
				// Своя база — защита от случайного захода в боевую dpport.
				if cfg.Postgres.DBName != "dpport_river" {
					t.Error("postgres.dbname обязан быть dpport_river")
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
