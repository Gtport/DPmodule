package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// SecretKeyServiceAccount — имя ключа секрета сервис-аккаунта Keycloak: под ним
// значение кладётся в SecretSource и под ним же читается запасной путь из env.
const SecretKeyServiceAccount = "KEYCLOAK_SA_CLIENT_SECRET"

type Config struct {
	App       App       `yaml:"app"`
	HTTP      HTTP      `yaml:"http"`
	Metrics   Metrics   `yaml:"metrics"`
	Postgres  Postgres  `yaml:"postgres"`
	Keycloak  Keycloak  `yaml:"keycloak"`
	Log       Log       `yaml:"log"`
	Storage   Storage   `yaml:"storage"`
	ASU       ASU       `yaml:"asu"`
	LKRobot   LKRobot   `yaml:"lk_robot"`
	Reference Reference `yaml:"reference"`
	WagonOps  WagonOps  `yaml:"wagonops"`
	MAX       MAX       `yaml:"max"`
	Bros      Bros      `yaml:"bros"`
}

// Bros — подсистема «Брошенные»: фоновый крон ежедневной фиксации журнала
// (bulk-save по всем активным броскам), чтобы посуточная история набивалась
// сама, не завися от отправок в MAX.
type Bros struct {
	Enabled     bool   `yaml:"enabled"`      // включить ежедневный крон bulk-save журнала
	JournalCron string `yaml:"journal_cron"` // время bulk-save по МСК «HH:MM»; дефолт 01:00
}

// MAX — исходящая рассылка форм («План подвода»/оперативка) в мессенджер MAX.
// Токен бота — bot_token (шаблон Vault, подставляет CI/CD) либо env MAX_BOT_TOKEN
// запасным путём, см. loadSecrets. Сами чаты и маршруты
// «терминал → чат» живут в БД (таблица max_chat), здесь — только транспорт.
// Крон-автоотправки для форм нет: рассылка по кнопке диспетчера (решение владельца).
type MAX struct {
	Enabled     bool          `yaml:"enabled"`      // false → ручки рассылки отвечают «отключено»
	BaseURL     string        `yaml:"base_url"`     // Bot API; дефолт https://platform-api.max.ru
	TimeoutSecs int           `yaml:"timeout_secs"` // таймаут HTTP; дефолт 120
	SendDelay   time.Duration `yaml:"send_delay"`   // пауза между чатами (лимит API 30 rps); дефолт 500ms

	// BotToken — секрет: токен бота MAX. Шаблон Vault в файле, значение подставляет
	// CI/CD; пусто → запасной путь: env MAX_BOT_TOKEN.
	BotToken string `yaml:"bot_token"`
}

// Reference — забор памяток на подачу/уборку из внешнего сервиса (тот же провайдер,
// что дислокация: base_url и ключ те же). Крон-инкремент раз в час + ручной забор по
// номеру. На этом этапе данные только логируются, в БД не кладутся.
//
// Тайминги окна запроса разведены по разным параметрам (урок боя 30.07.2026):
// pull_interval — только «как часто спрашивать», глубина окна первого захода —
// initial_lookback, запас на запоздавшие записи — cursor_overlap.
// LKRobot — автовыгрузка дислокации из личного кабинета РЖД вместо ручной
// работы диспетчера. Здесь только адрес кабинета и тайминги; кто выгружает
// (ОКПО→логин) — в настроечной таблице lk_account, а пароль вводит диспетчер
// в момент запуска и он нигде не сохраняется.
//
// Кабинет отдаёт данные обычным JSON-API, браузер не нужен: вход POST /sign_in,
// заказ отчёта, ожидание готовности, выгрузка xlsx (см. adapter/lkrobot).
type LKRobot struct {
	Enabled     bool          `yaml:"enabled"`      // разрешить ручку запуска
	BaseURL     string        `yaml:"base_url"`     // адрес кабинета; дефолт https://cargolk.rzd.ru
	ServiceID   int           `yaml:"service_id"`   // услуга «Дислокация вагонов по списку»; дефолт 226
	Timeout     time.Duration `yaml:"timeout"`      // таймаут одного HTTP-запроса к кабинету; дефолт 2m
	PollEvery   time.Duration `yaml:"poll_every"`   // период опроса готовности отчёта; дефолт 5s
	PollTimeout time.Duration `yaml:"poll_timeout"` // сколько ждём готовности; дефолт 5m
}

type Reference struct {
	Enabled         bool          `yaml:"enabled"`          // включить фоновый забор обновлений по тикеру
	BaseURL         string        `yaml:"base_url"`         // базовый URL провайдера (тот же, что у АСУ)
	InsecureTLS     bool          `yaml:"insecure_tls"`     // не проверять серт (самоподписанный на IP)
	PullInterval    time.Duration `yaml:"pull_interval"`    // период крон-инкремента; дефолт 1h
	InitialLookback time.Duration `yaml:"initial_lookback"` // глубина окна, когда курсора ещё нет; дефолт 48h
	CursorOverlap   time.Duration `yaml:"cursor_overlap"`   // нахлёст: курсор храним на столько раньше LAST_UPDATE; дефолт 30m
	StaleAfter      time.Duration `yaml:"stale_after"`      // курсор не двигается дольше — пустой проход идёт в журнал; дефолт 6h
	Clients         []string      `yaml:"clients"`          // коды клиентов провайдера: ["attis","nmtp"]
	AuthMode        string        `yaml:"auth_mode"`        // "keycloak" | "apikey"; пусто → keycloak, если сервис-аккаунт настроен
	AuthSecretKey   string        `yaml:"auth_secret_key"`  // режим apikey: имя ключа X-API-Key; дефолт ASU_TOKEN (тот же провайдер)
}

// ASU — фоновый забор дислокации из АСУ-АСУ (внутренний крон). Сами источники
// (base_url/clients/auth) живут в настроечной таблице data_source; здесь только
// расписание тикера. Enabled=false → воркер не запускается (забор только вручную
// через POST /dislocation/asu/pull).
type ASU struct {
	Enabled      bool          `yaml:"enabled"`       // включить фоновый забор по тикеру
	PullInterval time.Duration `yaml:"pull_interval"` // период забора; дефолт 10m
	PullOffset   time.Duration `yaml:"pull_offset"`   // сдвиг тиков от начала часа (5m при 10m → :05,:15,...); дефолт 0 → :00,:10,...
}

// WagonOps — фоновый воркер очереди запросов 601 «История продвижения вагона»
// (тот же провайдер, что дислокация; сам источник — data_source id=asu). Пороги
// подтверждены владельцем: пачка 50, пауза 500 мс (~2 мин на 200 вагонов).
type WagonOps struct {
	Enabled       bool          `yaml:"enabled"`        // включить фоновый разбор очереди
	DrainInterval time.Duration `yaml:"drain_interval"` // период тика воркера; дефолт 15s
	Batch         int           `yaml:"batch"`          // заявок за тик; дефолт 50
	Pause         time.Duration `yaml:"pause"`          // пауза между запросами; дефолт 500ms
	MaxAttempts   int           `yaml:"max_attempts"`   // неудач до снятия заявки; дефолт 5
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

	// Password — секрет: в файле объявляется шаблоном ${vault:<движок>/<путь>:<ключ>},
	// значение подставляет CI/CD перед стартом (своего резолвинга в приложении нет —
	// читаем как есть). Пусто → запасной путь: env PG_PASSWORD.
	Password string `yaml:"password"`

	// Assembled after load — not in yaml.
	DSN string
}

type Keycloak struct {
	Enabled  bool   `yaml:"enabled"` // false → API routes are served WITHOUT JWT auth (dev/template only)
	JWKSURL  string `yaml:"jwks_url"`
	Issuer   string `yaml:"issuer"`
	Audience string `yaml:"audience"`

	// ClientSecret — секрет (только для confidential-клиентов; пусто = не требуется).
	// Шаблон Vault в файле, значение подставляет CI/CD; пусто → env KEYCLOAK_CLIENT_SECRET.
	ClientSecret string `yaml:"client_secret"`

	// ServiceAccount — ИСХОДЯЩИЕ походы к провайдеру (дислокация, запрос 601,
	// памятки). Поля выше настраивают обратное — проверку ВХОДЯЩИХ токенов.
	ServiceAccount ServiceAccount `yaml:"service_account"`
}

// ServiceAccount — сервис-аккаунт Keycloak для потока client_credentials: сервис
// сам берёт access-токен и шлёт его провайдеру в "Authorization: Bearer".
// Человек в цикле не нужен — поэтому работает и в фоновых кронах.
//
// ⚠️ Заполненность этого блока И ЕСТЬ переключатель режима авторизации: пока
// token_url/client_id пусты, клиенты провайдера ходят по-старому (X-API-Key).
// Так стенд, где Keycloak ещё не заведён, не встаёт после обновления кода.
type ServiceAccount struct {
	TokenURL    string `yaml:"token_url"`    // <keycloak>/realms/<realm>/protocol/openid-connect/token
	ClientID    string `yaml:"client_id"`    // клиент с включённым service account
	Scope       string `yaml:"scope"`        // необязательно
	InsecureTLS bool   `yaml:"insecure_tls"` // самоподписанный серт Keycloak на стенде
	TimeoutSecs int    `yaml:"timeout_secs"` // таймаут запроса токена; дефолт 30

	// ClientSecret — секрет: шаблон Vault в файле, значение подставляет CI/CD;
	// пусто → запасной путь: env KEYCLOAK_SA_CLIENT_SECRET.
	ClientSecret string `yaml:"client_secret"`
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

// loadSecrets досыпает секреты из переменных окружения — но только те, что не
// заданы в файле. Канон: секрет объявляется в конфиге шаблоном
// ${vault:<движок>/<путь>:<ключ>} и резолвится CI/CD перед стартом; приложение
// читает уже готовое значение (своего резолвинга Vault здесь нет).
//
// env остаётся ЗАПАСНЫМ путём для стендов, где подстановки нет (машина
// разработчика, systemd с EnvironmentFile). Приоритет: файл → env.
func loadSecrets(cfg *Config) error {
	if cfg.Postgres.Password == "" {
		cfg.Postgres.Password = os.Getenv("PG_PASSWORD")
	}
	if cfg.Postgres.Enabled && cfg.Postgres.Password == "" {
		return fmt.Errorf("config: пароль postgres обязателен при postgres.enabled: true — " +
			"задать postgres.password в конфиге (шаблон Vault) либо env PG_PASSWORD")
	}

	if cfg.Keycloak.ClientSecret == "" {
		cfg.Keycloak.ClientSecret = os.Getenv("KEYCLOAK_CLIENT_SECRET")
	}
	if cfg.Keycloak.ServiceAccount.ClientSecret == "" {
		cfg.Keycloak.ServiceAccount.ClientSecret = os.Getenv(SecretKeyServiceAccount)
	}

	// Токен бота MAX: значение здесь только «взводится», читают его адаптеры
	// через port.SecretSource (см. secret.LayeredSource).
	//
	// X-API-Key провайдера сюда НЕ добавлять: к нему ходим через Keycloak
	// (keycloak.service_account), а запасной режим apikey берёт ключ прямо из
	// окружения по имени, которое объявил источник (data_source.auth_secret_key).
	if cfg.MAX.BotToken == "" {
		cfg.MAX.BotToken = os.Getenv("MAX_BOT_TOKEN")
	}

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
	if cfg.LKRobot.BaseURL == "" {
		cfg.LKRobot.BaseURL = "https://cargolk.rzd.ru"
	}
	if cfg.LKRobot.ServiceID == 0 {
		cfg.LKRobot.ServiceID = 226 // «Дислокация вагонов по списку» (SPV4664)
	}
	if cfg.LKRobot.Timeout == 0 {
		cfg.LKRobot.Timeout = 2 * time.Minute
	}
	if cfg.LKRobot.PollEvery == 0 {
		cfg.LKRobot.PollEvery = 5 * time.Second
	}
	if cfg.LKRobot.PollTimeout == 0 {
		cfg.LKRobot.PollTimeout = 5 * time.Minute
	}
	if cfg.Reference.PullInterval == 0 {
		cfg.Reference.PullInterval = time.Hour
	}
	if cfg.Reference.InitialLookback == 0 {
		cfg.Reference.InitialLookback = 48 * time.Hour
	}
	if cfg.Reference.CursorOverlap == 0 {
		cfg.Reference.CursorOverlap = 30 * time.Minute
	}
	if cfg.Reference.StaleAfter == 0 {
		cfg.Reference.StaleAfter = 6 * time.Hour
	}
	if cfg.Reference.AuthSecretKey == "" {
		cfg.Reference.AuthSecretKey = "ASU_TOKEN" // тот же провайдер/ключ, что и АСУ
	}
	if cfg.WagonOps.DrainInterval == 0 {
		cfg.WagonOps.DrainInterval = 15 * time.Second
	}
	if cfg.WagonOps.Batch == 0 {
		cfg.WagonOps.Batch = 50
	}
	if cfg.WagonOps.Pause == 0 {
		cfg.WagonOps.Pause = 500 * time.Millisecond
	}
	if cfg.WagonOps.MaxAttempts == 0 {
		cfg.WagonOps.MaxAttempts = 5
	}
	if cfg.MAX.BaseURL == "" {
		cfg.MAX.BaseURL = "https://platform-api.max.ru"
	}
	if cfg.MAX.TimeoutSecs == 0 {
		cfg.MAX.TimeoutSecs = 120
	}
	if cfg.MAX.SendDelay == 0 {
		cfg.MAX.SendDelay = 500 * time.Millisecond
	}
	if cfg.Bros.JournalCron == "" {
		cfg.Bros.JournalCron = "01:00"
	}
}
