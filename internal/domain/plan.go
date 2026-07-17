package domain

// Plan — заголовок одной загрузки плана подвода. История: на одну станцию (PlanCode)
// может быть несколько загрузок; фронт показывает список и по умолчанию свежую.
// Время — МСК naive (без таймзоны).
type Plan struct {
	ID         int64      `json:"id"`
	PlanCode   string     `json:"plan_code"`
	SourceFile string     `json:"source_file"`
	LoadedAt   *LocalTime `json:"loaded_at"`
	PlanDate   *LocalTime `json:"plan_date"` // «на какую дату план»: самая ранняя ЖД-дата ниток
	Nitki      int        `json:"nitki"`     // всего ниток (без служебных строк)
	Matched    int        `json:"matched"`   // сопоставлено с вагонами
	Stamped    int        `json:"stamped"`   // вагонов застолблено
}

// PlanSummary — краткая карточка загрузки для списка выбора плана на фронте.
type PlanSummary struct {
	ID         int64      `json:"id"`
	PlanCode   string     `json:"plan_code"`
	SourceFile string     `json:"source_file"`
	LoadedAt   *LocalTime `json:"loaded_at"`
	PlanDate   *LocalTime `json:"plan_date"` // «на какую дату план» (для списка/фильтра)
	Nitki      int        `json:"nitki"`
	Matched    int        `json:"matched"`
	Stamped    int        `json:"stamped"`
}

// PortCell — обобщённая ячейка порта в строке плана: метка столбца (терминал/груз
// из файла) и число вагонов. Набор столбцов — из данных файла (без хардкода портов),
// фронт строит колонки из объединения меток по всем ниткам.
type PortCell struct {
	Label string `json:"label"`
	Count int    `json:"count"`
	IsOur bool   `json:"is_our"` // причал «наш» (входит в Activ) — фронт показывает только такие столбцы
}

// PlanNitka — одна строка плана (нитка или служебная строка «Остаток на 18:00»)
// для таблицы плана подвода. Хранит разобранные из файла поля, результат
// сопоставления с дислокацией и данные под столбцы таблицы оригинала.
type PlanNitka struct {
	PlanCode      string     `json:"plan_code"`
	Ord           int        `json:"ord"` // порядок следования в файле
	Index         string     `json:"index"`
	IndexPp       string     `json:"index_pp"`
	StationOper   string     `json:"station_oper"` // станция текущей операции («Дислокация»)
	PlanMsk       *LocalTime `json:"plan_msk"`     // плановое прибытие (МСК, правило ≥18)
	PlanJd        *LocalTime `json:"plan_jd"`      // плановое время как в плане (без сдвига)
	FactMsk       *LocalTime `json:"fact_msk"`
	Otkl          string     `json:"otkl"`
	PlanRaw       string     `json:"plan_raw"` // сырой текст «Плана», когда он не время («не подводить»)
	Wagons        int        `json:"wagons"`   // всего вагонов поезда
	Activ         int        `json:"activ"`    // вагонов «наших» причалов
	Ports         []PortCell `json:"ports"`    // ячейки портов (обобщённо, из файла)
	Sostav        string     `json:"sostav"`   // сматченные группы вагонов («Состав»)
	Comment       string     `json:"comment"`  // «Примечание» (столбец «Комментарий»)
	Matched       bool       `json:"matched"`
	MatchedWagons int        `json:"matched_wagons"` // вагонов застолблено этой ниткой
	IsOstatok     bool       `json:"is_ostatok"`     // служебная строка «Остаток на 18:00»
	IsSf          bool       `json:"is_sf"`          // строка сборного формирования (с.ф.); синоним — в IndexPp
}

// SFRecord — строка справочника sf (dpport.sf): вариант написания синонима станции
// формирования → каноническая станция + потолок вагонов. Для подбора групп с.ф.
type SFRecord struct {
	Sinonim  string
	Station  string
	Quantity int
}
