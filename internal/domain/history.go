package domain

// VagonHistory — строка бизнес-истории рейса (таблица vagon_history, §3.19). Ключ
// id = dislocation.id (Vagon+CodeStationNach+DateNach). Вехи рейса: погрузка,
// прибытие (статус 10), выгрузка (статус 12). Трейл продвижения (vagon_operation) —
// отдельно. Заполняется точечно из снимка дислокации; invoice_main фиксируется при
// первом появлении и далее не перезаписывается.
type VagonHistory struct {
	ID          string
	Vagon       string
	InvoiceMain string
	Invoice     string
	IndexMain   string
	IndexPp     string
	DateNachD   *LocalTime // дата погрузки (только дата)
	StationNach string
	Gruzotpr    string
	Zayavka     string
	StanNazn    string
	GruzpolS    string
	Naznach     string
	CargoS      string
	CargoGroup  string

	FreightExactName string
	GtdNumber        string
	Ves              *float64
	Client           string
	RodVagUch        string
	CarOwnerName     string
	CarOwnerOkpo     string
	CarTenantName    string
	CarTenantOkpo    string

	Status     *int
	DateDostav *LocalTime
	PlanMsk    *LocalTime
	PlanJd     *LocalTime
	Otkl       string // отклонение факт/план "±hh:mm" (пусто без плана)
	Delay      *int   // просрочка доставки, сутки

	DatePrib  *LocalTime // прибытие (статус 10 — расчётный date_kon)
	DatePribD *LocalTime // прибытие, только дата
	DateVigr  *LocalTime // выгрузка (статус 12 — time_op)
	DateVigrD *LocalTime // выгрузка, ЖД-дата
	PlaceVigr string     // место выгрузки (= naznach)

	Frost     *int
	Shipments string
	Peregruz  string
	Info1     string
	Info2     string
	Sms1      string
	Sms2      string
	Sms3      string
	Color     string

	CreatedAt *LocalTime
	UpdatedAt *LocalTime
}
