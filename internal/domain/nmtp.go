package domain

// NmtpColumn — колонка формы «Подход вагонов» (НМТП) терминала: три подписи
// шапки (клиент / станции погрузки / марка — как в файле порта) и правила
// матчинга вагона. Списки правил — текст с разделителем '|' (формат gtport,
// универсальный админ-редактор правит как строку); пустое правило — любое
// значение. Справочник nmtp_column (миграция 000053), правится в Админе.
type NmtpColumn struct {
	ID            int64 // ключ привязок nmtp_vagon_column
	Terminal      string
	SortOrder     int
	GroupLabel    string
	StationLabel  string
	MarkLabel     string
	MatchClients  string
	MatchStations string
	MatchMarks    string
}

// NmtpColumnHead — шапка колонки (три уровня, как в файле порта).
type NmtpColumnHead struct {
	ID      int64  `json:"id,omitempty"` // nmtp_column.id — цель ручного переноса поезда
	Group   string `json:"group"`
	Station string `json:"station"`
	Mark    string `json:"mark"`
}

// NmtpTrainRow — строка-поезд формы.
type NmtpTrainRow struct {
	Index        string     `json:"index"`
	StationOper  string     `json:"station_oper"`
	DateNach     *LocalTime `json:"date_nach"`      // мода даты приёма к перевозке
	Note         string     `json:"note,omitempty"` // перестановка: «НА {терминал}» / «С {терминал}»
	ControlVagon string     `json:"control_vagon"`  // «вагон для контроля» — первый вагон состава
	Prog         *LocalTime `json:"prog"`           // ожидаемое прибытие (prog_jd, МСК)
	Planned      bool       `json:"planned"`        // поезд в плане подвода (есть plan_jd) — только таким печатается прибытие
	DateBros     *LocalTime `json:"date_bros,omitempty"`
	Counts       []int      `json:"counts"` // по колонкам; последний элемент — «прочее»
	Tons         []float64  `json:"-"`      // тонны по колонкам (для подвала)
	Total        int        `json:"total"`
}

// NmtpSection — секция строк (станция терминала либо дорога) с итогом.
type NmtpSection struct {
	Label     string         `json:"label"`
	Near      bool           `json:"near"`                 // участвует в «Прогнозе выгрузки по подходу»
	IsStation bool           `json:"is_station,omitempty"` // причальная станция (не дорога) — экран пустую прячет
	Rows      []NmtpTrainRow `json:"rows"`
	Total     int            `json:"total"`
}

// NmtpClientTons — строка свода «клиент → тоннаж» (подвал файла порта).
type NmtpClientTons struct {
	Client string  `json:"client"`
	Tons   float64 `json:"tons"` // тыс. тонн
}

// NmtpReport — форма целиком (активные + брошенные + подвал).
type NmtpReport struct {
	Terminal  string           `json:"terminal"`
	Columns   []NmtpColumnHead `json:"columns"`
	HasOther  bool             `json:"has_other"` // есть вагоны вне правил — колонка «прочее»
	Sections  []NmtpSection    `json:"sections"`
	Abandoned []NmtpSection    `json:"abandoned"`

	ColCounts []int     `json:"col_counts"` // итого вагонов по колонкам (актив + брошенные)
	ColTons   []float64 `json:"col_tons"`   // итого тыс. тонн по колонкам

	TotalVagons     int     `json:"total_vagons"`
	TotalTons       float64 `json:"total_tons"` // тыс. тонн
	TrainsActive    int     `json:"trains_active"`
	TrainsAbandoned int     `json:"trains_abandoned"`

	UnloadForecast float64          `json:"unload_forecast"` // вагонов/сут: ближние секции / 7
	Norm           int              `json:"norm"`            // норма вагонов на сети (0 — блока нет)
	ClientTons     []NmtpClientTons `json:"client_tons"`     // свод по клиентам (group_label)
}
