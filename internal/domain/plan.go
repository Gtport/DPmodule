package domain

// Plan — заголовок загруженного плана подвода (одна станция плана = один план).
// Сводка последней загрузки для фронта. Время — МСК naive (без таймзоны).
type Plan struct {
	PlanCode   string     `json:"plan_code"`
	SourceFile string     `json:"source_file"`
	LoadedAt   *LocalTime `json:"loaded_at"`
	Nitki      int        `json:"nitki"`   // всего ниток
	Matched    int        `json:"matched"` // сопоставлено с вагонами
	Stamped    int        `json:"stamped"` // вагонов застолблено
}

// PlanNitka — одна нитка плана (строка расписания) для отображения на фронте.
// Хранит и разобранные из файла поля, и результат сопоставления с дислокацией.
type PlanNitka struct {
	PlanCode      string     `json:"plan_code"`
	Ord           int        `json:"ord"` // порядок следования в файле
	Index         string     `json:"index"`
	IndexPp       string     `json:"index_pp"`
	PlanMsk       *LocalTime `json:"plan_msk"` // плановое прибытие (МСК, правило ≥18)
	PlanJd        *LocalTime `json:"plan_jd"`  // плановое время как в плане (без сдвига)
	FactMsk       *LocalTime `json:"fact_msk"`
	Otkl          string     `json:"otkl"`
	Wagons        int        `json:"wagons"` // всего вагонов поезда
	Activ         int        `json:"activ"`  // вагонов «наших» причалов
	Matched       bool       `json:"matched"`
	MatchedWagons int        `json:"matched_wagons"` // вагонов застолблено этой ниткой
}
