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
	ID        string
	Name      string
	Enabled   bool
	Ingest    string // IngestUpload | IngestAPIPull
	Category  string // CategoryDislocation | CategoryPlan | CategoryTechState
	Config    DataSourceConfig
	SortOrder int
}

// DataSourceConfig — разобранный config источника. Здесь поля upload-приёма ЛК;
// поля api_pull (endpoint/headers/…) добавим в JSON-слайсе.
type DataSourceConfig struct {
	Detect         []string          `json:"detect,omitempty"`         // маркеры распознавания файла («Личный кабинет»)
	SubtypeMarker  map[string]string `json:"subtype_marker,omitempty"` // «Дислокация вагонов»→lk и т.п.
	AllowedExt     []string          `json:"allowed_ext,omitempty"`    // ["xlsx","xls"]
	MaxMB          int               `json:"max_mb,omitempty"`         // лимит размера файла
	OkpoMap        map[string]string `json:"okpo_map,omitempty"`       // ОКПО грузополучателя → код порта (AT/NMTP)
	OkpoColumn     string            `json:"okpo_column,omitempty"`    // заголовок колонки ОКПО грузополучателя
	HeaderMarker   string            `json:"header_marker,omitempty"`  // текст строки заголовка таблицы
	DateCutoffHour int               `json:"date_cutoff_hour,omitempty"`
}

// PortByOkpo возвращает код порта по ОКПО грузополучателя (из okpo_map).
func (c DataSourceConfig) PortByOkpo(okpo string) (string, bool) {
	p, ok := c.OkpoMap[okpo]
	return p, ok
}

// ExpectedPorts — множество кодов портов, ожидаемых у этого источника (значения
// okpo_map). Порядок не гарантирован. Используется для контроля «сколько файлов».
func (c DataSourceConfig) ExpectedPorts() []string {
	if len(c.OkpoMap) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(c.OkpoMap))
	out := make([]string, 0, len(c.OkpoMap))
	for _, port := range c.OkpoMap {
		if _, dup := seen[port]; dup {
			continue
		}
		seen[port] = struct{}{}
		out = append(out, port)
	}
	return out
}

// ClientSettings — синглтон клиентских параметров.
type ClientSettings struct {
	ClientName   string
	IngestPolicy IngestPolicy
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
	PlanMaxLagHours        int    `json:"plan_max_lag_hours,omitempty"`        // план не позже дислокации на N ч
}
