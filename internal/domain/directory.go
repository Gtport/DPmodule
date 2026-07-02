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

// Marka — словарь «наши грузы» (Stage 2).
// Ключ: Okpo+StationKod+CargoKod (НЕ уникален — на ключ может быть несколько записей).
type Marka struct {
	Okpo       int64
	StationKod int64
	CargoKod   int64
	Shipper    string // → Gruzotpr
	CargoS     string // → CargoS
	Client     string // → Client
	CargoGroup string // → CargoGroup
	Sms1       string // → CargoSms / Sms1
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
