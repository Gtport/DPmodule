package domain

// Переадресация (pereadr_type) и маркер назначения внешнего порта.
const (
	PereadrOwn = "own" // переадресован на свой терминал
	PereadrExt = "ext" // переадресован на внешний порт (имя — в pereadr_port)

	// NaznachExternalPort — значение naznach у вагона, уведённого на внешний
	// порт (перенос маркера «ВП» gtport; сам порт — в pereadr_port).
	NaznachExternalPort = "ВП"
)

// Dislocation — одна запись о текущем местонахождении и состоянии вагона
// (оперативная дислокация). Данные приходят из выгрузки АСУ РЖД (JSON или xlsx из
// ЛК) и проходят конвейер обогащения Stage 1–4 (справочники станций/операций,
// marka/ports, статусы, расписание портов).
//
// Это доменная сущность (она же DTO для API): JSON-теги задают контракт,
// поля даты/времени — LocalTime (без метки часового пояса, без Z). ORM-модель для
// БД — отдельно, в repository/gorm (маппинг колонок там). Полные описания каждого
// поля и источники обогащения — в _reference/vagones.go.
//
// Порядок и имена полей соответствуют таблице dpport.dislocation
// (migrations/000001_init_dpport.up.sql).
type Dislocation struct {
	// ── Идентификаторы ───────────────────────────────────────────────────────
	ID          string `json:"id"`           // детерминированный PK (Vagon+CodeStationNach+DateNach)
	Vagon       string `json:"vagon"`        // номер вагона, основной бизнес-ключ
	Invoice     string `json:"invoice"`      // номер накладной
	InvoiceMain string `json:"invoice_main"` // сводный номер накладной

	// ── Индексы поезда ───────────────────────────────────────────────────────
	Index     string `json:"index"`      // текущий индекс поезда
	IndexMain string `json:"index_main"` // «родительский» индекс (Stage 2), ключ сопоставления с планом
	IndexLast string `json:"index_last"` // предыдущий индекс
	IndexPp   string `json:"index_pp"`   // плановая (портовая) нитка прибытия

	// ── Погрузка / отправление ───────────────────────────────────────────────
	DateNach        *LocalTime `json:"date_nach"`         // дата начала рейса
	DateOtpr        *LocalTime `json:"date_otpr"`         // дата отправления
	CodeStationNach string     `json:"code_station_nach"` // код ЕСР станции отправления
	StationNach     string     `json:"station_nach"`      // имя станции отправления (Stage 1)
	DorogaNach      string     `json:"doroga_nach"`       // дорога отправления
	StrNach         string     `json:"str_nach"`          // страна начала рейса (код)
	Zayavka         string     `json:"zayavka"`           // номер заявки ГУ-12

	// ── Грузоотправитель ─────────────────────────────────────────────────────
	GruzotprOkpo string `json:"gruzotpr_okpo"` // ОКПО грузоотправителя (ключ marka)
	Gruzotpr     string `json:"gruzotpr"`      // имя грузоотправителя (Stage 2 ← marka.Shipper)

	// ── Назначение и грузополучатель ─────────────────────────────────────────
	CodeStanNazn  string `json:"code_stan_nazn"`  // код ЕСР станции назначения
	Code4StanNazn string `json:"code4_stan_nazn"` // 4-значный код станции назначения (Stage 1)
	StanNazn      string `json:"stan_nazn"`       // имя станции назначения (Stage 1)
	DorogaNazn    string `json:"doroga_nazn"`     // код дороги назначения
	StrNazn       string `json:"str_nazn"`        // страна назначения (код)
	GruzpolOkpo   string `json:"gruzpol_okpo"`    // ОКПО грузополучателя
	Gruzpol       string `json:"gruzpol"`         // имя грузополучателя (Stage 2 ← port.Organisation)
	GruzpolS      string `json:"gruzpol_s"`       // краткое имя причала (УТ-1/АЭ/ГУТ-2) ← port.NameS
	Naznach       string `json:"naznach"`         // фактическое назначение (по умолчанию = GruzpolS)
	Owner         string `json:"owner"`           // чей вагон: оператор → арендатор → собственник (S2-2, carry-over)

	// ── Груз ─────────────────────────────────────────────────────────────────
	CodeCargo        string   `json:"code_cargo"`         // код груза ЕТСНГ (ключ marka)
	CodeCargoGng     string   `json:"code_cargo_gng"`     // код груза ГНГ
	CodeCargoVygr    string   `json:"code_cargo_vygr"`    // код груза выгрузки
	CargoS           string   `json:"cargo_s"`            // имя груза (Stage 2 ← marka)
	CargoSms         string   `json:"cargo_sms"`          // краткое имя груза для SMS (= Sms1)
	CargoGroup       string   `json:"cargo_group"`        // группа груза (УГОЛЬ/МЕТАЛЛ) ← marka
	Ves              *float64 `json:"ves"`                // масса груза, тонны
	PorozhPriznak    string   `json:"porozh_priznak"`     // признак «порожний»
	FreightExactName string   `json:"freight_exact_name"` // точное наименование груза из источника
	GtdNumber        string   `json:"gtd_number"`         // номер ГТД

	// ── Операции ─────────────────────────────────────────────────────────────
	TimeOp          *LocalTime `json:"time_op"`           // дата-время последней операции
	DateOp          *LocalTime `json:"date_op"`           // дата последней операции (= TimeOp)
	DateOpJd        *LocalTime `json:"date_op_jd"`        // «ЖД»-дата операции (час ≥ 18 → +1 сутки)
	CodeOper        string     `json:"code_oper"`         // код грузовой операции (92 = бросание)
	Oper            string     `json:"oper"`              // имя операции (Stage 1 ← cargo_operations)
	OperS           string     `json:"oper_s"`            // краткое имя операции (в IdDisl)
	CodeStationOper string     `json:"code_station_oper"` // код ЕСР станции операции
	StationOper     string     `json:"station_oper"`      // имя станции операции (Stage 1)
	DorogaOper      string     `json:"doroga_oper"`       // дорога станции операции

	// ── Идентификаторы отправки ──────────────────────────────────────────────
	IdOtprk string `json:"id_otprk"` // номер отправки
	Uno     string `json:"uno"`      // код документа

	// ── География ────────────────────────────────────────────────────────────
	Latitude  string   `json:"latitude"`  // широта станции операции (Stage 1)
	Longitude string   `json:"longitude"` // долгота станции операции (Stage 1)
	Temper    *float64 `json:"temper"`    // температура, °C

	// ── Расстояния ───────────────────────────────────────────────────────────
	RasstStanNazn *int `json:"rasst_stan_nazn"` // до станции назначения, км
	RasstOb       *int `json:"rasst_ob"`        // общее, км
	RasstStanOp   *int `json:"rasst_stan_op"`   // пройденное, км

	// ── Простои ──────────────────────────────────────────────────────────────
	ProstDn  *int `json:"prost_dn"`  // простой, сутки
	ProstCh  *int `json:"prost_ch"`  // простой, часы
	ProstMin *int `json:"prost_min"` // простой, минуты

	// ── Идентификаторы и статусы ─────────────────────────────────────────────
	IdDisl    string `json:"id_disl"`    // составной ключ поезда (Stage 1)
	NppVag    *int   `json:"npp_vag"`    // порядковый номер вагона в составе
	Status    *int   `json:"status"`     // расчётный статус (0/1/2/4/5/10)
	IdStatus5 string `json:"id_status5"` // ключ агрегации брошенных (статус 5)
	IdStatus4 string `json:"id_status4"` // ключ агрегации долгого простоя (статус 4)

	// ── Сроки, планы, прогнозы ───────────────────────────────────────────────
	DateDostav *LocalTime `json:"date_dostav"` // нормативный срок доставки
	Delay      *int       `json:"delay"`       // фактическая просрочка, сутки
	DelayProg  *int       `json:"delay_prog"`  // прогнозная задержка, сутки (Stage 4)
	PlanJd     *LocalTime `json:"plan_jd"`     // плановая дата прибытия (JD)
	PlanMsk    *LocalTime `json:"plan_msk"`    // плановая дата прибытия (МСК)
	ToGo       *float64   `json:"to_go"`       // расчётное время хода до порта, часы (Stage 2)
	RaschMsk   *LocalTime `json:"rasch_msk"`   // расчётное прибытие (МСК)
	ProgMsk    *LocalTime `json:"prog_msk"`    // прогнозное прибытие (МСК), нитка порта (Stage 4)
	Mistake    *float64   `json:"mistake"`     // «необъяснённый простой», дни (Stage 4)
	RaschJd    *LocalTime `json:"rasch_jd"`    // RaschMsk в JD-формате
	ProgJd     *LocalTime `json:"prog_jd"`     // ProgMsk в JD-формате
	DateKon    *LocalTime `json:"date_kon"`    // расчётная дата закрытия рейса (Stage 1)
	DatePrib   *LocalTime `json:"date_prib"`   // фактическая дата прибытия

	// ── Маршрут ──────────────────────────────────────────────────────────────
	// AlternativeMove — код альтернативного продвижения (ранее булев IsBam «БАМ»).
	// В источнике прямого признака нет; заполняется отдельной логикой по маршруту
	// (в т.ч. по stations.is_bam). По умолчанию 0.
	AlternativeMove int `json:"alternative_move"`

	// ── Собственник и оператор вагона ────────────────────────────────────────
	CarOwnerName   string `json:"car_owner_name"`   // собственник (имя)
	CarOwnerOkpo   string `json:"car_owner_okpo"`   // собственник (ОКПО)
	CarTenantName  string `json:"car_tenant_name"`  // оператор/арендатор (имя)
	CarTenantOkpo  string `json:"car_tenant_okpo"`  // оператор/арендатор (ОКПО)
	CarTrustedName string `json:"car_trusted_name"` // доверенное лицо (имя)
	CarTrustedOkpo string `json:"car_trusted_okpo"` // доверенное лицо (ОКПО)

	// ── Переадресация (операторская, взамен info_1/info_2 gtport) ────────────
	// Решение оператора увести поезд на другой порт; поток РЖД этих полей не
	// знает — переносятся carry-over'ом всегда, снимаются только явной отменой.
	PereadrType string `json:"pereadr_type"` // "" — нет; PereadrOwn; PereadrExt
	PereadrPort string `json:"pereadr_port"` // имя внешнего порта (только при PereadrExt)

	// ── Клиент и пользовательские поля (marka) ───────────────────────────────
	Client  string `json:"client"` // имя клиента (Stage 2 ← marka)
	Sms1    string `json:"sms_1"`  // метки для SMS/уведомлений
	Sms2    string `json:"sms_2"`
	Sms3    string `json:"sms_3"`
	Sprav1  string `json:"sprav_1"` // справочные строки (marka)
	Sprav2  string `json:"sprav_2"`
	Sprav3  string `json:"sprav_3"`
	Param1  string `json:"param_1"` // общие текстовые параметры
	Param2  string `json:"param_2"`
	Param3  string `json:"param_3"`
	NParam1 string `json:"n_param_1"` // числовые параметры (как строки)
	NParam2 string `json:"n_param_2"`
	NParam3 string `json:"n_param_3"`

	// ── Выгрузка и прочее ────────────────────────────────────────────────────
	DateVigr  *LocalTime `json:"date_vigr"`  // дата выгрузки
	PlaceVigr string     `json:"place_vigr"` // место выгрузки
	Frost     *int       `json:"frost"`      // признак заморозки
	Info1     string     `json:"info_1"`     // свободные строки диспетчера
	Info2     string     `json:"info_2"`
	Peregruz  string     `json:"peregruz"`    // номер вагона-донора при перегрузе (S2-3, §3.17); пусто = обычная погрузка
	Color     string     `json:"color"`       // цветовая метка для фронта
	RodVagUch string     `json:"rod_vag_uch"` // код рода вагона (НЕ собственник)
	Shipments string     `json:"shipments"`   // связанные отгрузки/партии
	History   int        `json:"history"`     // флаг запроса истории продвижения (601)

	// ── Служебные метки ──────────────────────────────────────────────────────
	CreatedAt LocalTime `json:"created_at"`
	UpdatedAt LocalTime `json:"updated_at"`
}
