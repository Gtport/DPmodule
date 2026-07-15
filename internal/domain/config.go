package domain

// Настроечная таблица: реестр каналов ввода (data_source) и клиентские параметры
// (client_settings). Чистые структуры без ORM-тегов; ORM/загрузка — gorm-репозиторий,
// кэш в RAM — internal/service.ConfigCache. См. миграцию 000003 и TARGET.md §3.10.

// Способ получения данных источника (data_source.ingest).
const (
	IngestUpload  = "upload"
	IngestAPIPull = "api_pull"
)

// Категория данных источника (data_source.category).
const (
	CategoryDislocation = "dislocation"
	CategoryPlan        = "plan"
	CategoryTechState   = "tech_state"
)

// DataSource — один канал ввода. Config — разобранный JSONB (транспорт и
// пер-файловая валидация). Пороги, общие для категории, живут в ClientSettings.
type DataSource struct {
	ID             string
	Name           string
	Enabled        bool
	Ingest         string // IngestUpload | IngestAPIPull
	Category       string // CategoryDislocation | CategoryPlan | CategoryTechState
	Config         DataSourceConfig
	CoArrivalGroup string // метка совместного среза (§3.12); пустая = вне группы
	SortOrder      int
}

// DataSourceConfig — разобранный config источника. Поля upload-приёма ЛК и
// поля api_pull (АСУ-АСУ: базовый URL, коды клиентов провайдера, авторизация).
type DataSourceConfig struct {
	Detect         []string          `json:"detect,omitempty"`         // маркеры распознавания файла («Личный кабинет»)
	SubtypeMarker  map[string]string `json:"subtype_marker,omitempty"` // «Дислокация вагонов»→lk и т.п.
	AllowedExt     []string          `json:"allowed_ext,omitempty"`    // ["xlsx","xls"]
	MaxMB          int               `json:"max_mb,omitempty"`         // лимит размера файла
	OkpoColumn     string            `json:"okpo_column,omitempty"`    // заголовок колонки ОКПО грузополучателя
	HeaderMarker   string            `json:"header_marker,omitempty"`  // текст строки заголовка таблицы
	DateCutoffHour int               `json:"date_cutoff_hour,omitempty"`

	// api_pull (АСУ-АСУ). Провайдер отдаёт снимок по маршруту <base_url>/<client>/dislocation
	// в формате {timestamp,count,wagons} (envelope, см. parser.JSONParser). Один источник
	// перечисляет всех своих клиентов; ingest тянет их за один проход и сверяет метки.
	BaseURL       string   `json:"base_url,omitempty"`        // базовый URL сервиса АСУ (без хвостового пути)
	Clients       []string `json:"clients,omitempty"`         // коды клиентов провайдера: ["attis","nmtp"]
	PathTemplate  string   `json:"path_template,omitempty"`   // шаблон пути, {client} → код; дефолт "/{client}/dislocation"
	Method        string   `json:"method,omitempty"`          // HTTP-метод, дефолт GET
	AuthSecretKey string   `json:"auth_secret_key,omitempty"` // ключ секрета в SecretSource; пусто — без авторизации
	AuthHeader    string   `json:"auth_header,omitempty"`     // заголовок для секрета (напр. "X-API-Key"); пусто — "Authorization: Bearer <секрет>"
	InsecureTLS   bool     `json:"insecure_tls,omitempty"`    // не проверять TLS-сертификат провайдера (самоподписанный серт на IP); по умолчанию проверяем
	TimeoutSecs   int      `json:"timeout_secs,omitempty"`    // таймаут одного запроса, дефолт 30
}

// Идентификация «чей файл»/терминала — НЕ здесь: ОКПО грузополучателя проверяется
// против справочника ports (окпо не уникален — один ОКПО может иметь несколько
// терминалов), см. DirectoryCache.PortsByOkpo и TARGET.md §3.12. Прежний okpo_map
// (ОКПО→код порта) упразднён как тупиковый.

// ClientSettings — синглтон клиентских параметров.
type ClientSettings struct {
	ClientName   string
	IngestPolicy IngestPolicy
	Status       StatusPolicy // пороги расчёта статусов (из client_settings.extra.status)
	Stage4       Stage4Policy // пороги прогноза прибытия (из client_settings.extra.stage4)
}

// StatusPolicy — общепрограмные пороги расчёта статусов дислокации (§3.12/§3.13).
// Живёт в client_settings.extra.status. Значения GTport: 1 сутки / 12 часов.
type StatusPolicy struct {
	ProstDnMin int `json:"prost_dn_min"` // порог простоя в сутках → статус 4
	ProstChMin int `json:"prost_ch_min"` // порог простоя в часах → статус 4
}

// IngestPolicy — пороги приёма по категориям (§3.9). Межфайловые/на загрузку
// целиком правила, не принадлежащие одному каналу.
type IngestPolicy struct {
	Dislocation CategoryPolicy `json:"dislocation"`
	Plan        CategoryPolicy `json:"plan"`
}

// CategoryPolicy — набор порогов одной категории (поля-омитемпти: для разных
// категорий заполнены разные подмножества).
type CategoryPolicy struct {
	MaxGapMinutes          int    `json:"max_gap_minutes,omitempty"`           // макс. разрыв между файлами одной загрузки
	MaxStalenessMinutes    int    `json:"max_staleness_minutes,omitempty"`     // устаревание относительно «сейчас»
	RejectOlderThanCurrent bool   `json:"reject_older_than_current,omitempty"` // запрет отката на старую дислокацию
	RejectOlderRoleExempt  string `json:"reject_older_role_exempt,omitempty"`  // роль-исключение (предупреждение вместо запрета)
	MaxDataLossPct         int    `json:"max_data_loss_pct,omitempty"`         // порог потери данных (%)
	MaxSourceSkewMinutes   int    `json:"max_source_skew_minutes,omitempty"`   // макс. расхождение меток формирования между api_pull-источниками (АСУ); 0 — гард выключен
	PlanMaxLagHours        int    `json:"plan_max_lag_hours,omitempty"`        // план не позже дислокации на N ч
	PlanMaxDislAgeMinutes  int    `json:"plan_max_disl_age_minutes,omitempty"` // не грузить план, если дислокация старше N мин (0 — гард выключен)
}

// Режим прогноза станции (plan_profile.mode).
const (
	PlanModePlanned  = "planned"  // есть расписание ниток → Stage 4 раскладывает по слотам
	PlanModeCapacity = "capacity" // плана нет → прогноз из pc_* × correction_coef
)

// PlanProfile — настроечный портрет станции плана (таблица plan_profile). Ключ —
// StationCode (= ports.station_code); терминалы одной станции делят настройки.
// Расхардкодивание builtinProfiles + задел под Stage 4 (Mode/CorrectionCoef) и
// аналитику расписания (nitka_schedule).
type PlanProfile struct {
	StationCode          string
	StationName          string
	Mode                 string   // PlanModePlanned | PlanModeCapacity
	PlanCode             string   // ma/nk у плановых; "" у бесплановых
	CorrectionCoef       float64  // поправочный коэф (один на станцию, capacity-режим)
	MatchRequiresNaznach bool
	OurTerminals         []string // ключевые слова «наших» колонок плана (вклад в Activ)
	SlotToleranceH       float64  // допуск слота Stage 4: слот может быть ≥ Rasch − N часов
	DistributionMethod   string   // метод раскладки Stage 4: "staircase" (УТ-1) | "excel" (АЭ/ГУТ-2); пусто → excel
}

// NitkaSlot — один слот расписания станции (таблица nitka_schedule). Слоты повторяются
// каждые сутки; общий пул прибытия станции для всех терминалов (Stage 4).
type NitkaSlot struct {
	StationCode string
	Hour        int
	Minute      int
	SortOrder   int
}

// Stage4Policy — пороги прогноза прибытия (client_settings.extra.stage4).
type Stage4Policy struct {
	MinVagonCount int `json:"min_vagon_count"` // минимум вагонов для беспланового прогноза (эталон 20)
	MinVagonBros  int `json:"min_vagon_bros"`  // порог для брошенных (эталон 10)
	BrosPenaltyH  int `json:"bros_penalty_h"`  // штраф бросания, часов (эталон 72)
}
