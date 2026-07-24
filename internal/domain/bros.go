package domain

// BrosReasonCode — строка справочника официальных кодов бросания РЖД
// (классификатор причин отстановки поезда от движения). code — двузначный
// («01».."95"), description — расшифровка. Наполняется миграцией, правится
// в Админе. Используется журналом бросков (подсказки кодов) — следующие ветки.
type BrosReasonCode struct {
	Code        string `json:"code"`
	Description string `json:"description"`
}

// Bros — снимок одного брошенного поезда (агрегат вагонов статуса 5). ID = ключ
// агрегации id_status5 (`index|code_station_oper|time_op`). Жизненный цикл:
// StatusBr=true (активен) → false (поднят). Заполняется reconcile-слоем
// конвейера (`applyBros`) после Stage 4. Reason/Comment/DatePod правит оператор
// в журнале (ветка 3). Даты (DateBr/DatePod/DatePodFact/Plan) — без времени;
// Prog0/Prog1/Created/Updated — timestamp. Всё время Московское naive.
type Bros struct {
	ID          string     `json:"id"`
	IDIndex     string     `json:"id_index"`
	Index0      string     `json:"index_0"`
	Index1      string     `json:"index_1"`
	StationBr   string     `json:"station_br"`
	DorogaBr    string     `json:"doroga_br"`
	DateBr      *LocalTime `json:"date_br"`
	GruzpolS    string     `json:"gruzpol_s"`
	DatePod     *LocalTime `json:"date_pod"`
	DatePodFact *LocalTime `json:"date_pod_fact"`
	Prog0       *LocalTime `json:"prog_0"`
	Prog1       *LocalTime `json:"prog_1"`
	ToGo        *float64   `json:"to_go"`
	Plan        *LocalTime `json:"plan"`
	PlanHistory string     `json:"plan_history"`
	StatusBr    bool       `json:"status_br"`
	Reason      string     `json:"reason"`
	Comment     string     `json:"comment"`
	Sostav      string     `json:"sostav"`
	VagonCount  int        `json:"vagon_count"`
	CreatedAt   *LocalTime `json:"created_at"`
	UpdatedAt   *LocalTime `json:"updated_at"`
}

// BrosFilter — фильтр отчёта по броскам (перенос gtport): терминалы, период,
// статус активности. Пустые поля — без ограничения.
type BrosFilter struct {
	GruzpolS  []string   // терминалы (краткие имена причала); пусто — все
	StartDate *LocalTime // начало периода (по date_br / date_pod_fact)
	EndDate   *LocalTime // конец периода
	Status    *bool      // true — активные, false — завершённые, nil — все
	Limit     int
	Offset    int
}
