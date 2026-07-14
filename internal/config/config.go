package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	App      App      `yaml:"app"`
	HTTP     HTTP     `yaml:"http"`
	Metrics  Metrics  `yaml:"metrics"`
	Postgres Postgres `yaml:"postgres"`
	Keycloak Keycloak `yaml:"keycloak"`
	Log      Log      `yaml:"log"`
	Storage  Storage  `yaml:"storage"`
	ASU      ASU      `yaml:"asu"`
}

// ASU — фоновый забор дислокации из АСУ-АСУ (внутренний крон). Сами источники
// (base_url/clients/auth) живут в настроечной таблице data_source; здесь только
// расписание тикера. Enabled=false → воркер не запускается (забор только вручную
// через POST /dislocation/asu/pull).
type ASU struct {
	Enabled      bool          `yaml:"enabled"`       // включить фоновый забор по тикеру
	PullInterval time.Duration `yaml:"pull_interval"` // период забора; дефолт 10m
}

// Storage — локальное файловое хранилище на сервере (вне git). Загруженные
// файлы ЛК кладутся в <BaseDir>/lk/. По умолчанию "_data".
type Storage struct {
	BaseDir string `yaml:"base_dir"`
}

type App struct {
	Name string `yaml:"name"`
	Env  string `yaml:"env"` // dev | uat | prod
}

type HTTP struct {
	Host            string        `yaml:"host"` // пусто = все интерфейсы (docker); 127.0.0.1 = только loopback (VPS за nginx)
	Port            int           `yaml:"port"`
	ReadTimeout     time.Duration `yaml:"read_timeout"`
	WriteTimeout    time.Duration `yaml:"write_timeout"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
}

// Metrics controls the Prometheus /metrics endpoint.
// Port is a dedicated port so metrics aren't exposed alongside the public API.
// Set it equal to http.port to serve /metrics on the main server instead.
type Metrics struct {
	Port int `yaml:"port"`
}

type Postgres struct {
	Enabled         bool          `yaml:"enabled"` // false → skip connection, app boots without DB
	Host            string        `yaml:"host"`
	Port            int           `yaml:"port"`
	DBName          string        `yaml:"dbname"`
	User            string        `yaml:"user"`
	SSLMode         string        `yaml:"sslmode"`
	MaxOpenConns    int           `yaml:"max_open_conns"`
	MaxIdleConns    int           `yaml:"max_idle_conns"`
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime"`

	// Assembled after load — not in yaml.
	DSN      string
	Password string // secret — injected by CI/CD from Vault as PG_PASSWORD
}

type Keycloak struct {
	Enabled  bool   `yaml:"enabled"` // false → API routes are served WITHOUT JWT auth (dev/template only)
	JWKSURL  string `yaml:"jwks_url"`
	Issuer   string `yaml:"issuer"`
	Audience string `yaml:"audience"`

	ClientSecret string // secret — injected by CI/CD from Vault as KEYCLOAK_CLIENT_SECRET
}

type Log struct {
	Level      string `yaml:"level"`        // debug | info | warn | error
	File       string `yaml:"file"`         // path to log file; empty = stdout only
	MaxSizeMB  int    `yaml:"max_size_mb"`  // rotate after N MB (default 100)
	MaxBackups int    `yaml:"max_backups"`  // keep N rotated files (default 5)
	MaxAgeDays int    `yaml:"max_age_days"` // delete files older than N days (default 30)
}

// Load reads config from a YAML file and overlays secrets from environment variables.
func Load(path string) (*Config, error) {
	cfg, err := loadFile(path)
	if err != nil {
		return nil, err
	}

	if err := loadSecrets(cfg); err != nil {
		return nil, err
	}

	setDefaults(cfg)

	if cfg.Postgres.Enabled {
		cfg.Postgres.DSN = fmt.Sprintf(
			"host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
			cfg.Postgres.Host,
			cfg.Postgres.Port,
			cfg.Postgres.DBName,
			cfg.Postgres.User,
			cfg.Postgres.Password,
			cfg.Postgres.SSLMode,
		)
	}

	return cfg, nil
}

func loadFile(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("config: open %q: %w", path, err)
	}
	defer f.Close()

	var cfg Config
	if err := yaml.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("config: parse %q: %w", path, err)
	}
	return &cfg, nil
}

// loadSecrets reads sensitive values from environment variables.
// These are never stored in the config file.
func loadSecrets(cfg *Config) error {
	if cfg.Postgres.Enabled {
		pg, ok := requireEnv("PG_PASSWORD")
		if !ok {
			return fmt.Errorf("config: secret PG_PASSWORD is required when postgres.enabled is true")
		}
		cfg.Postgres.Password = pg
	}

	cfg.Keycloak.ClientSecret = os.Getenv("KEYCLOAK_CLIENT_SECRET")

	return nil
}

// setDefaults fills in zero values with sensible fallbacks.
func setDefaults(cfg *Config) {
	if cfg.App.Name == "" {
		cfg.App.Name = "iqport-service"
	}
	if cfg.App.Env == "" {
		cfg.App.Env = "dev"
	}
	if cfg.HTTP.Port == 0 {
		cfg.HTTP.Port = 8080
	}
	if cfg.HTTP.ReadTimeout == 0 {
		cfg.HTTP.ReadTimeout = 10 * time.Second
	}
	if cfg.HTTP.WriteTimeout == 0 {
		cfg.HTTP.WriteTimeout = 30 * time.Second
	}
	if cfg.HTTP.ShutdownTimeout == 0 {
		cfg.HTTP.ShutdownTimeout = 15 * time.Second
	}
	if cfg.Metrics.Port == 0 {
		cfg.Metrics.Port = 9090
	}
	if cfg.Postgres.SSLMode == "" {
		cfg.Postgres.SSLMode = "disable"
	}
	if cfg.Postgres.MaxOpenConns == 0 {
		cfg.Postgres.MaxOpenConns = 25
	}
	if cfg.Postgres.MaxIdleConns == 0 {
		cfg.Postgres.MaxIdleConns = 5
	}
	if cfg.Postgres.ConnMaxLifetime == 0 {
		cfg.Postgres.ConnMaxLifetime = 5 * time.Minute
	}
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
	if cfg.Storage.BaseDir == "" {
		cfg.Storage.BaseDir = "_data"
	}
	if cfg.ASU.PullInterval == 0 {
		cfg.ASU.PullInterval = 10 * time.Minute
	}
}

func requireEnv(key string) (string, bool) {
	v := os.Getenv(key)
	return v, v != ""
}
