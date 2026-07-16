package domain

// Справочники обогащения дислокации. Чистые структуры (без ORM-тегов):
// ORM-модели и загрузка — в internal/repository/gorm; кэш в RAM — internal/service.
// Минимальный срез колонок — см. migration 000002_init_directories.

// Station — справочник станций (Stage 1). Ключи поиска: Kod, Kod4.
type Station struct {
	Kod       int
	Kod4      int
	Name      string   // → StationNach / StanNazn / StationOper
	Road      string   // → DorogaNach / DorogaOper
	Latitude  *float64 // → Latitude (может отсутствовать)
	Longitude *float64 // → Longitude
	IsBam     bool     // признак БАМ (Байкало-Амурская магистраль)
}

// RouteSpeed — один участок скоростного профиля (Stage 2, миграция 000005,
// TARGET.md §3.12). Профиль = набор участков с общим ключом (StationNach, IsBam);
// StationNach == "*" — профиль по умолчанию. Заменяет switch по станции из gtlogic
// enrich_stage2.go. Участок для остатка расстояния — с наибольшим FromKm ≤ остаток.
type RouteSpeed struct {
	StationNach string  // станция отправления; "*" = по умолчанию
	IsBam       bool    // альтернативный маршрут (БАМ)
	FromKm      int     // нижняя граница участка (км до назначения)
	Speed       float64 // км/ч
}

// CargoOperation — справочник операций груза (Stage 1). Ключ: Kod.
type CargoOperation struct {
	Kod   int
	Oper  string // полное имя → Dislocation.Oper
	OperS string // краткое имя → Dislocation.OperS
}

// Cargo — словарь грузов ЕТСНГ (Stage 1, миграция 000027). Ключ: Kod.
// Полный перечень с наложенной ручной переработкой (бизнес-группы, краткие
// имена, метки). Заполняет груз-поля каждому вагону независимо от отправителя.
type Cargo struct {
	Kod        int64
	Name       string // полное имя груза
	CargoGroup string // → Dislocation.CargoGroup
	CargoS     string // → Dislocation.CargoS
	CargoSms   string // → Dislocation.CargoSms
}

// Marka — словарь бизнес-атрибуции «наших» грузов (Stage 2, миграция 000028).
// Ключ: Okpo+StationKod+CargoGroup (УНИКАЛЕН). Вход по ГРУППЕ груза, а не по коду:
// новый код внутри знакомой группы у известного отправителя матчится без правки
// словаря. Идентичность груза (группа/имя/метка) — словарь Cargo, не marka.
type Marka struct {
	Okpo       int64  // ОКПО грузоотправителя (ключ)
	StationKod int64  // код станции отправления (ключ)
	Station    string // имя станции погрузки (информационное, для владельца; поиск — по коду)
	CargoGroup string // группа груза (ключ; = Cargo.CargoGroup)
	Shipper    string // → Gruzotpr
	Client     string // → Client
	Sms1       string // → Sms1 (метка уровня отправитель+станция+группа)
	Sms3       string // → Sms3 (регион/направление; пусто, если неоднозначно)
}

// Ports — справочник причалов/грузополучателей (Stage 2) + слой настроек/физики
// (миграция 000004, TARGET.md §3.12). Идентичность порта — по составному ключу
// Okpo+Location (НЕ уникален: один ОКПО может относиться к нескольким причалам на
// разных станциях, напр. НМТП → ГУТ-2 (Мыс Астафьева) и УТ-1 (Находка)).
type Ports struct {
	Okpo         int64
	Location     string
	Organisation string // → Gruzpol
	NameS        string // → GruzpolS (краткое имя причала: УТ-1 / АЭ / ГУТ-2)
	Name         string
	Code         string

	// Настроечные/физические поля (000004). pc_* и Front — nil, если род груза не
	// обрабатывается терминалом. Интервал между поездами (Stage 4) считается из
	// перерабатывающей способности: interval_h = вагонов × 24 / pc_рода.
	PlanCode    string // param_s1: 'ma'/'nk'/'rb' — тип плана подвода
	StationCode string // param_s2: код причальной станции
	PcCoal      *int   // перераб. способность, ваг/сут, уголь
	PcMetal     *int   // ... металл
	PcOther     *int   // ... прочее
	PcTotal     *int   // суммарно
	Front       *int   // фронт выгрузки
	Color       string // цвет отображения
	Enabled     bool   // at_work
	SortOrder   int
}

// NaznachStation — настроечная таблица «перестановок назначения» (Stage 2, §3.17).
// Для вагона, физически идущего на станцию назначения DestStation, «фактическое
// назначение» (Naznach — площадка внутри порта) определяется по станции отправления
// OriginStation. Обобщает хардкод «МЫС АСТАФЬЕВА» из gtlogic: у каждой станции
// назначения свой список. Ключ поиска — имена станций (DestStation, OriginStation).
type NaznachStation struct {
	DestStation   string // станция назначения-триггер (= StanNazn)
	OriginStation string // станция отправления (= StationNach)
	Naznach       string // площадка назначения (результат)
	Univers       bool   // признак «универсальный»
	Enabled       bool   // включена ли перестановка
}
