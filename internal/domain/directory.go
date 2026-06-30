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

// Ports — справочник причалов/грузополучателей (Stage 2).
// Ключ: Okpo+Location (НЕ уникален).
type Ports struct {
	Okpo         int64
	Location     string
	Organisation string // → Gruzpol
	NameS        string // → GruzpolS (краткое имя причала: УТ-1 / АЭ / ГУТ-2)
	Name         string
	Code         string
}
