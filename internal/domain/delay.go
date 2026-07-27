package domain

// Виды задержки вагона (vagon_delay.kind) — по расчётному статусу дислокации.
const (
	DelayKindProstoi = 4 // долгий простой в пути (статус 4)
	DelayKindBros    = 5 // брошен (статус 5, code_oper = 92)
)

// VagonDelay — эпизод задержки вагона: «стоял на станции с DateFrom по DateTo».
// Память о задержках рейса: за рейс у вагона может накопиться несколько эпизодов.
// Ведёт reconcile-слой конвейера (`applyDelays`): открытие при входе в статус 4/5,
// продолжение пока станция та же (эскалация 4 → 5 затирает Kind, DateFrom
// сохраняется — стоянка та же), закрытие при смене станции / уходе из статуса /
// пропаже из выгрузки. Открытый эпизод — DateTo == nil (не больше одного на вагон).
// GroupKey (= id_status4/id_status5) связывает с bros и вагонами той же группы;
// у вагонов без индекса пуст. Всё время Московское naive.
type VagonDelay struct {
	ID          int64      `json:"id"`
	Vagon       string     `json:"vagon"`
	DateNachD   *LocalTime `json:"date_nach_d"` // дата начала рейса (привязка к vagon_history)
	Kind        int        `json:"kind"`        // DelayKindProstoi | DelayKindBros
	GroupKey    string     `json:"group_key"`
	Index       string     `json:"index"`      // текущий индекс поезда
	IndexMain   string     `json:"index_main"` // «родительский» индекс (ключ плана)
	StationCode string     `json:"station_code"`
	StationName string     `json:"station_name"`
	Doroga      string     `json:"doroga"`
	DateFrom    *LocalTime `json:"date_from"` // начало стоянки (time_op)
	DateTo      *LocalTime `json:"date_to"`   // nil — стоит до сих пор
	Hours       *float64   `json:"hours"`     // длительность, часы (при закрытии)
	CreatedAt   *LocalTime `json:"created_at"`
	UpdatedAt   *LocalTime `json:"updated_at"`
}

// VagonDelayRow — эпизод в отчёте: сам эпизод + id строки рейса в vagon_history
// (по нему открывается «История движения вагона»; пуст, если строки рейса нет)
// + часть длительности, попавшая в период отчёта (вагоно-часы за период).
type VagonDelayRow struct {
	VagonDelay
	TripID        string  `json:"trip_id"`
	HoursInPeriod float64 `json:"hours_in_period"`
}

// DelayStationAgg — строка отчёта «по станциям»: вагоно-часы простоя за период
// с разбивкой по виду, уникальные вагоны, стоящие прямо сейчас.
type DelayStationAgg struct {
	StationCode string  `json:"station_code"`
	StationName string  `json:"station_name"`
	Doroga      string  `json:"doroga"`
	Episodes    int     `json:"episodes"`
	Vagons      int     `json:"vagons"`
	OpenNow     int     `json:"open_now"`
	Hours4      float64 `json:"hours4"` // долгий простой
	Hours5      float64 `json:"hours5"` // брошен
	Hours       float64 `json:"hours"`  // всего за период
}

// DelayReport — отчёт «Простои за период»: эпизоды (пересекающиеся с периодом)
// + агрегат по станциям (по убыванию вагоно-часов) + итоги.
type DelayReport struct {
	Records       []VagonDelayRow   `json:"records"`
	Stations      []DelayStationAgg `json:"stations"`
	TotalHours    float64           `json:"total_hours"`
	TotalEpisodes int               `json:"total_episodes"`
	TotalVagons   int               `json:"total_vagons"`
	OpenNow       int               `json:"open_now"`
}
