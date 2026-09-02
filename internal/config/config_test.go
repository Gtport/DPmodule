package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// write кладёт конфиг во временный файл и отдаёт путь.
func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("подготовка конфига: %v", err)
	}
	return p
}

// Канон после перехода на Vault: значение секрета приходит В ФАЙЛЕ (шаблон
// ${vault:...} резолвит CI/CD перед стартом). Файл сильнее окружения.
func TestLoad_SecretsFromFileWinOverEnv(t *testing.T) {
	t.Setenv("PG_PASSWORD", "из-окружения")
	t.Setenv("KEYCLOAK_CLIENT_SECRET", "из-окружения")
	t.Setenv(SecretKeyServiceAccount, "из-окружения")
	t.Setenv("MAX_BOT_TOKEN", "из-окружения")

	cfg, err := Load(write(t, `
postgres:
  enabled: true
  host: localhost
  port: 5432
  dbname: dpport
  user: gtport_app
  password: из-файла
keycloak:
  client_secret: kc-из-файла
  service_account:
    client_secret: sa-из-файла
max:
  bot_token: max-из-файла
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Postgres.Password != "из-файла" {
		t.Errorf("postgres.password = %q, ждали значение из файла", cfg.Postgres.Password)
	}
	if cfg.Keycloak.ClientSecret != "kc-из-файла" {
		t.Errorf("keycloak.client_secret = %q, ждали значение из файла", cfg.Keycloak.ClientSecret)
	}
	if cfg.Keycloak.ServiceAccount.ClientSecret != "sa-из-файла" {
		t.Errorf("keycloak.service_account.client_secret = %q, ждали значение из файла",
			cfg.Keycloak.ServiceAccount.ClientSecret)
	}
	if cfg.MAX.BotToken != "max-из-файла" {
		t.Errorf("max.bot_token = %q, ждали значение из файла", cfg.MAX.BotToken)
	}
	// DSN собирается из того же значения — иначе подстановка CI/CD не доедет до базы.
	if !strings.Contains(cfg.Postgres.DSN, "password='из-файла'") {
		t.Errorf("DSN = %q, ждали пароль из файла", cfg.Postgres.DSN)
	}
}

// Запасной путь для стендов без подстановки (машина разработчика, systemd
// с EnvironmentFile): в файле пусто — берём окружение.
func TestLoad_EnvFallbackWhenFileEmpty(t *testing.T) {
	t.Setenv("PG_PASSWORD", "pg-env")
	t.Setenv("KEYCLOAK_CLIENT_SECRET", "kc-env")
	t.Setenv(SecretKeyServiceAccount, "sa-env")
	t.Setenv("MAX_BOT_TOKEN", "max-env")

	cfg, err := Load(write(t, `
postgres:
  enabled: true
  host: localhost
  port: 5432
  dbname: dpport
  user: gtport_app
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Postgres.Password != "pg-env" {
		t.Errorf("postgres.password = %q, ждали запасной путь из env", cfg.Postgres.Password)
	}
	if cfg.Keycloak.ClientSecret != "kc-env" {
		t.Errorf("keycloak.client_secret = %q, ждали запасной путь из env", cfg.Keycloak.ClientSecret)
	}
	if cfg.Keycloak.ServiceAccount.ClientSecret != "sa-env" {
		t.Errorf("keycloak.service_account.client_secret = %q, ждали запасной путь из env",
			cfg.Keycloak.ServiceAccount.ClientSecret)
	}
	if cfg.MAX.BotToken != "max-env" {
		t.Errorf("max.bot_token = %q, ждали запасной путь из env", cfg.MAX.BotToken)
	}
}

// Падаем громко: база включена, а пароля нет ни в файле, ни в окружении —
// это не «поднимемся без базы», а недонастроенный стенд.
func TestLoad_PostgresPasswordRequired(t *testing.T) {
	t.Setenv("PG_PASSWORD", "")

	_, err := Load(write(t, `
postgres:
  enabled: true
  host: localhost
  port: 5432
  dbname: dpport
  user: gtport_app
`))
	if err == nil {
		t.Fatal("ждали ошибку: пароль postgres не задан ни в файле, ни в env")
	}
	if !strings.Contains(err.Error(), "postgres.password") {
		t.Errorf("текст ошибки не подсказывает, где задать пароль: %v", err)
	}
}

// postgres.enabled: false — пароль не нужен, приложение поднимается «голым».
func TestLoad_NoPasswordNeededWhenPostgresDisabled(t *testing.T) {
	t.Setenv("PG_PASSWORD", "")

	cfg, err := Load(write(t, "postgres:\n  enabled: false\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Postgres.DSN != "" {
		t.Errorf("DSN = %q, при выключенной базе не собирается", cfg.Postgres.DSN)
	}
}

// BuildDSN: schema уходит в search_path только когда задана — один тип Postgres
// обслуживает и основную базу, и кэш тайлов в общей базе стенда.
func TestBuildDSN_SchemaOptional(t *testing.T) {
	p := Postgres{Host: "h", Port: 5432, DBName: "db", User: "u", Password: "pw", SSLMode: "disable"}

	if dsn := p.BuildDSN(); strings.Contains(dsn, "search_path") {
		t.Errorf("DSN без schema = %q, search_path быть не должно", dsn)
	}
	p.Schema = "dpport"
	if dsn := p.BuildDSN(); !strings.Contains(dsn, "search_path='dpport'") {
		t.Errorf("DSN со schema = %q, ждали search_path='dpport'", dsn)
	}
}

// Блок tiles: пароль по канону MAP_TILES.md — tiles.password → env
// TILES_PG_PASSWORD → postgres.password (роль в кластере одна).
func TestLoad_TilesPasswordFallsBackToPostgres(t *testing.T) {
	t.Setenv("PG_PASSWORD", "")
	t.Setenv("TILES_PG_PASSWORD", "")

	cfg, err := Load(write(t, `
postgres:
  enabled: true
  host: localhost
  port: 5433
  dbname: dpport
  user: gtport_app
  password: общий-пароль
tiles:
  enabled: true
  host: localhost
  dbname: tiles
  user: gtport_app
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Tiles.Password != "общий-пароль" {
		t.Errorf("tiles.password = %q, ждали пароль основной базы", cfg.Tiles.Password)
	}
	if !strings.Contains(cfg.Tiles.DSN, "dbname='tiles'") || !strings.Contains(cfg.Tiles.DSN, "port=5432") {
		t.Errorf("tiles.DSN = %q, ждали dbname=tiles и дефолтный порт 5432", cfg.Tiles.DSN)
	}
	if cfg.Tiles.MaxOpenConns != 10 {
		t.Errorf("tiles.max_open_conns = %d, ждали скромный дефолт 10", cfg.Tiles.MaxOpenConns)
	}
}
