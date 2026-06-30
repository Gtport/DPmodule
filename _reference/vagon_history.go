// server/internal/models/vagon_history.go
package models

import (
	"database/sql"
	"time"
)

// VagonHistory — curated-снимок рейса для отчётов. Поля упорядочены по смыслу:
// идентификация → маршрут → груз → собственность → план/статус →
// прибытие → подача → выгрузка → уборка → метки → служебное.
type VagonHistory struct {
	// ── Идентификация / ключи ───────────────────────────────────────────────
	ID    string `json:"id" db:"id"`
	Vagon string `json:"vagon" db:"vagon"`
	// TripKey — детерминированный ключ рейса (vagon*100000 + дней_от_эпохи(date_nach_d)).
	// Вычисляется БД: GENERATED ALWAYS ... STORED. Связь с vagon_operation.
	TripKey     int64  `json:"trip_key" db:"trip_key"`
	InvoiceMain string `json:"invoice_main" db:"invoice_main"`
	Invoice     string `json:"invoice" db:"invoice"`
	IndexMain   string `json:"index_main" db:"index_main"`
	IndexPp     string `json:"index_pp" db:"index_pp"`

	// ── Отправление / маршрут ───────────────────────────────────────────────
	DateNachD   *time.Time `json:"date_nach_d" db:"date_nach_d"` // дата погрузки (ЖД-сутки)
	StationNach string     `json:"station_nach" db:"station_nach"`
	Gruzotpr    string     `json:"gruzotpr" db:"gruzotpr"` // имя (из обогащения)
	Zayavka     string     `json:"zayavka" db:"zayavka"`
	StanNazn    string     `json:"stan_nazn" db:"stan_nazn"`
	GruzpolS    string     `json:"gruzpol_s" db:"gruzpol_s"`
	Naznach     string     `json:"naznach" db:"naznach"`

	// ── Груз ────────────────────────────────────────────────────────────────
	CargoS           string         `json:"cargo_s" db:"cargo_s"`
	CargoGroup       sql.NullString `json:"cargo_group" db:"cargo_group"`
	FreightExactName string         `json:"freight_exact_name" db:"freight_exact_name"` // точное наименование
	GtdNumber        string         `json:"gtd_number" db:"gtd_number"`                 // номер ГТД
	Ves              *float64       `json:"ves" db:"ves"`
	Client           string         `json:"client" db:"client"`

	// ── Собственность вагона ────────────────────────────────────────────────
	RodVagUch     string `json:"rod_vag_uch" db:"rod_vag_uch"`         // код рода вагона (НЕ собственник)
	CarOwnerName  string `json:"car_owner_name" db:"car_owner_name"`   // собственник (имя)
	CarOwnerOkpo  string `json:"car_owner_okpo" db:"car_owner_okpo"`   // собственник (ОКПО)
	CarTenantName string `json:"car_tenant_name" db:"car_tenant_name"` // оператор (имя)
	CarTenantOkpo string `json:"car_tenant_okpo" db:"car_tenant_okpo"` // оператор (ОКПО)

	// ── План / статус / доставка ────────────────────────────────────────────
	Status     *int       `json:"status" db:"status"`
	DateDostav *time.Time `json:"date_dostav" db:"date_dostav"`
	PlanMsk    *time.Time `json:"plan_msk" db:"plan_msk"`
	PlanJd     *time.Time `json:"plan_jd" db:"plan_jd"`
	Otkl       string     `json:"otkl" db:"otkl"`   // отклонение факт/план
	Delay      *int       `json:"delay" db:"delay"` // просрочка доставки, сутки

	// ── ПРИБЫТИЕ ────────────────────────────────────────────────────────────
	DatePrib   *time.Time `json:"date_prib" db:"date_prib"`       // дата прибытия (ст.10 — расчётный DateKon)
	DatePribD  *time.Time `json:"date_prib_d" db:"date_prib_d"`   // дата прибытия (только дата)
	DateUvPrib *time.Time `json:"date_uv_prib" db:"date_uv_prib"` // дата уведомления о прибытии
	NomUvPrib  string     `json:"nom_uv_prib" db:"nom_uv_prib"`   // номер уведомления о прибытии

	// ── ПОДАЧА ──────────────────────────────────────────────────────────────
	DatePod     *time.Time `json:"date_pod" db:"date_pod"`           // дата подачи на фронт
	DateUvPod   *time.Time `json:"date_uv_pod" db:"date_uv_pod"`     // дата уведомления о подаче
	NomUvPod    string     `json:"nom_uv_pod" db:"nom_uv_pod"`       // номер уведомления о подаче
	DateGu45Pod *time.Time `json:"date_gu45_pod" db:"date_gu45_pod"` // дата ГУ-45 (памятка) на подачу — уточнить
	NomGu45Pod  string     `json:"nom_gu45_pod" db:"nom_gu45_pod"`   // номер ГУ-45 на подачу
	DatePodGu45 *time.Time `json:"date_pod_gu45" db:"date_pod_gu45"` // дата подачи по ГУ-45 — уточнить
	PlacePod    string     `json:"place_pod" db:"place_pod"`         // место/фронт подачи

	// ── ВЫГРУЗКА ────────────────────────────────────────────────────────────
	DateVigr     *time.Time `json:"date_vigr" db:"date_vigr"`           // дата-время выгрузки (статус 12)
	DateVigrD    *time.Time `json:"date_vigr_d" db:"date_vigr_d"`       // дата выгрузки (ЖД-сутки)
	DateVigrGu45 *time.Time `json:"date_vigr_gu45" db:"date_vigr_gu45"` // дата ГУ-45 при выгрузке
	PlaceVigr    string     `json:"place_vigr" db:"place_vigr"`         // порт выгрузки (статус 12)

	// ── УБОРКА ──────────────────────────────────────────────────────────────
	DateUbor     *time.Time `json:"date_ubor" db:"date_ubor"`           // дата уборки с фронта
	DateGu45Ubor *time.Time `json:"date_gu45_ubor" db:"date_gu45_ubor"` // дата ГУ-45 на уборку — уточнить
	NomGu45Ubor  string     `json:"nom_gu45_ubor" db:"nom_gu45_ubor"`   // номер ГУ-45 на уборку
	DateUborGu45 *time.Time `json:"date_ubor_gu45" db:"date_ubor_gu45"` // дата уборки по ГУ-45 — уточнить

	// ── Метки / прочее ──────────────────────────────────────────────────────
	Frost     *int   `json:"frost" db:"frost"` // признак заморозки
	Shipments string `json:"shipments" db:"shipments"`
	Info1     string `json:"info_1" db:"info_1"`
	Info2     string `json:"info_2" db:"info_2"`
	Info3     string `json:"info_3" db:"info_3"`
	Sms1      string `json:"sms_1" db:"sms_1"`
	Sms2      string `json:"sms_2" db:"sms_2"`
	Sms3      string `json:"sms_3" db:"sms_3"`
	Color     string `json:"color" db:"color"`

	// ── Служебное ───────────────────────────────────────────────────────────
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// LoadingReportRequest запрос на получение отчета по погрузке
type LoadingReportRequest struct {
	DateNachD time.Time `json:"date_nach_d"`
}

// LoadingReportItem элемент отчета по погрузке
type LoadingReportItem struct {
	GruzpolS    string `json:"gruzpol_s" db:"gruzpol_s"`
	Sms1        string `json:"sms_1" db:"sms_1"`
	StationNach string `json:"station_nach" db:"station_nach"`
	Client      string `json:"client" db:"client"`
	VagonCount  int    `json:"vagon_count" db:"vagon_count"`
}

// GruzpolTotal суммарные данные по грузополучателю
type GruzpolTotal struct {
	GruzpolS    string `json:"gruzpol_s" db:"gruzpol_s"`
	TotalVagons int    `json:"total_vagons" db:"total_vagons"`
	EvrazVagons int    `json:"evraz_vagons" db:"evraz_vagons"`
	OtherVagons int    `json:"other_vagons" db:"other_vagons"`
}

// DailyAverageStats статистика среднесуточной погрузки
type DailyAverageStats struct {
	PeriodStart  time.Time `json:"period_start"`
	PeriodEnd    time.Time `json:"period_end"`
	TotalDays    int       `json:"total_days"`
	TotalVagons  int       `json:"total_vagons" db:"total_vagons"`
	EvrazVagons  int       `json:"evraz_vagons" db:"evraz_vagons"`
	OtherVagons  int       `json:"other_vagons" db:"other_vagons"`
	DailyAverage float64   `json:"daily_average"`
	EvrazAverage float64   `json:"evraz_average"`
	OtherAverage float64   `json:"other_average"`
}

// LoadingReportResponse ответ с отчетом по погрузке
type LoadingReportResponse struct {
	DateNachD          time.Time                               `json:"date_nach_d"`
	PeriodType         string                                  `json:"period_type"`
	PeriodStart        time.Time                               `json:"period_start"`
	PeriodEnd          time.Time                               `json:"period_end"`
	Items              []LoadingReportItem                     `json:"items"`
	TotalByGruzpol     []GruzpolTotal                          `json:"total_by_gruzpol"`
	DailyAverageByPort map[string]DailyAverageStats            `json:"daily_average_by_port"`
	DailyAverageBySms1 map[string]map[string]DailyAverageStats `json:"daily_average_by_sms1"`
}

// PribStatsRequest запрос для статистики прибытия
type PribStatsRequest struct {
	DatePribD time.Time `json:"date_prib_d"`
	Naznach   string    `json:"naznach"`
}

// PribStatsResponse ответ со статистикой прибытия
type PribStatsResponse struct {
	DatePribD time.Time `json:"date_prib_d"`
	Naznach   string    `json:"naznach"`
	Total     int       `json:"total"`
	Prib      int       `json:"prib"`
	MPrib     int       `json:"m_prib"`
	ChPrib    int       `json:"ch_prib"`
}

// JournalExportRequest запрос на выгрузку из журнала мастеров АЭ
type JournalExportRequest struct {
	FileName string              `json:"file_name"`
	Items    []JournalExportItem `json:"items"`
	Report   ReportData          `json:"report"`
}

// JournalExportItem элемент выгрузки из журнала
type JournalExportItem struct {
	Vagon     string  `json:"vagon"`
	DateA     *string `json:"date_a"`
	DateO     *string `json:"date_o"`
	DateC     *string `json:"date_c"`
	Frost     *int    `json:"frost"`
	PlaceVigr string  `json:"place_vigr"`
}

// ReportData отчет из фронтенда
type ReportData struct {
	Date0            string `json:"date0"`
	IncomingBalance  int    `json:"incomingBalance"`
	UnloadedToday    int    `json:"unloadedToday"`
	RemainingBalance int    `json:"remainingBalance"`
}

// VagonOperation — одна операция продвижения вагона в пределах рейса (запрос 601).
// Связь N:1 с VagonHistory по trip_key. При повторном запросе истории (например,
// при смене статуса на 10) весь набор операций рейса ПЕРЕЗАПИСЫВАЕТСЯ
// (DELETE по trip_key + INSERT) в одной транзакции — см. VagonOperationRepository.
type VagonOperation struct {
	TripKey    int64      `json:"trip_key" db:"trip_key"`       // FK → vagon_history.trip_key, ON DELETE CASCADE
	DateOp     *time.Time `json:"date_op" db:"date_op"`         // дата-время операции (без таймзоны)
	KopVmd     string     `json:"kop_vmd" db:"kop_vmd"`         // код операции
	StanOp     string     `json:"stan_op" db:"stan_op"`         // код станции (char(6), ведущие нули сохранены)
	IndexPoezd *string    `json:"index_poezd" db:"index_poezd"` // NULL, если поезда нет («000…0»)
}

// TripKey — детерминированный ключ рейса: vagon*100000 + дней_от_эпохи(date_nach_d).
// ОБЯЗАН совпадать с GENERATED-колонкой vagon_history.trip_key
// (vagon::bigint*100000 + (date_nach_d::date - DATE '1970-01-01')).
func TripKey(vagon int64, dateNachD time.Time) int64 {
	y, m, d := dateNachD.Date()
	days := time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400
	return vagon*100000 + days
}
