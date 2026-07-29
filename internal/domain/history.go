package domain

import (
	"strconv"
	"strings"
	"time"
)

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
	CarTrustedName   string
	CarTrustedOkpo   string
	Owner            string // чей вагон: оператор → арендатор → собственник (см. Dislocation)

	PereadrType string // переадресация: "" / "own" / "ext" (см. Dislocation)
	PereadrPort string // имя внешнего порта при "ext"

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

// ── Запрос 601 «История продвижения вагона» ─────────────────────────────────

// VagonOperation — одна операция продвижения вагона в пределах рейса (таблица
// vagon_operation, запрос 601). Связь N:1 с VagonHistory по trip_key; повторный
// запрос истории ПЕРЕЗАПИСЫВАЕТ весь трейл рейса (DELETE + INSERT одной
// транзакцией, см. VagonOperationRepository.ReplaceForTrip).
type VagonOperation struct {
	TripKey    int64
	DateOp     LocalTime
	KopVmd     string // код операции
	StanOp     string // код станции (шестизначный, ведущие нули сохранены)
	IndexPoezd string // "" — операция вне поезда (индекс «000…0»)
}

// VagonOpRequest — заявка очереди запросов 601 (таблица vagon_op_request).
// PK trip_key: повторный триггер по тому же рейсу обновляет заявку — групповая
// смена статусов (~200 вагонов) не плодит дублей, воркер разгребает с паузой.
type VagonOpRequest struct {
	TripKey   int64
	Vagon     string
	DateNachD LocalTime
	Client    string // клиент провайдера (ports.provider_client по ОКПО грузополучателя)
	Reason    string // arrival / missing / departed / manual
	Priority  int    // ручные заявки выше автоматических
	Attempts  int
	LastError string
	CreatedAt LocalTime
	UpdatedAt LocalTime
}

// LoadingDailyRow — агрегат погрузки в адрес терминала за ЖД-сутки (отчёт
// «Погрузка», перенос gtport loading-report): строка = дата погрузки ×
// терминал × метка отправителя (sms_1) × станция × клиент × группа груза.
// Разбивка «уголь/металл» ведётся по cargo_group из словаря грузов, а не по
// клиенту «ЕВРАЗ ТК», как было захардкожено в gtport (решение владельца
// 29.07.2026, HARDCODE_INVENTORY §6.2).
type LoadingDailyRow struct {
	Day         LocalTime `json:"day"` // date_nach_d (сутки)
	GruzpolS    string    `json:"gruzpol_s"`
	Sms1        string    `json:"sms_1"`
	StationNach string    `json:"station_nach"`
	Client      string    `json:"client"`
	CargoGroup  string    `json:"cargo_group"`
	VagonCount  int       `json:"vagon_count"`
	TotalWeight float64   `json:"total_weight"`
}

// TripKeyOf — детерминированный ключ рейса: vagon*100000 + дней_от_эпохи(date_nach_d).
// ОБЯЗАН совпадать с GENERATED-колонкой vagon_history.trip_key
// (vagon::bigint*100000 + (date_nach_d::date - DATE '1970-01-01')).
// false — номер вагона не числовой или дата пустая.
func TripKeyOf(vagon string, dateNachD *LocalTime) (int64, bool) {
	if dateNachD == nil {
		return 0, false
	}
	num, err := strconv.ParseInt(strings.TrimSpace(vagon), 10, 64)
	if err != nil {
		return 0, false
	}
	t := dateNachD.Time()
	y, m, d := t.Date()
	days := time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400
	return num*100000 + days, true
}
