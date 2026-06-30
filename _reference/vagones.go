// server/internal/models/vagones.go
package models

import (
	"database/sql/driver"
	"fmt"
	"strings"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
//  LocalTime — время без метки часового пояса (без Z)
// ─────────────────────────────────────────────────────────────────────────────

// localTimeLayout — единый формат времени GTport: без суффикса Z и без смещения.
const localTimeLayout = "2006-01-02T15:04:05"

// LocalTime — обёртка над time.Time, которая в JSON и БД представляется БЕЗ метки
// часового пояса. В JSON сериализуется как "2006-01-02T15:04:05" (nil-указатель →
// null, нулевое время → null). В PostgreSQL пишется в колонку timestamp (without
// time zone) через driver.Valuer. Для арифметики/сравнений используйте .Time().
type LocalTime time.Time

// Time возвращает обычный time.Time (для .Add/.Sub/.Before/.Format и т.п.).
func (lt LocalTime) Time() time.Time { return time.Time(lt) }

// IsZero — обёртка над time.Time.IsZero.
func (lt LocalTime) IsZero() bool { return time.Time(lt).IsZero() }

// String — представление в локальном формате без Z.
func (lt LocalTime) String() string { return time.Time(lt).Format(localTimeLayout) }

// NewLocalTime создаёт *LocalTime из time.Time.
func NewLocalTime(t time.Time) *LocalTime { lt := LocalTime(t); return &lt }

// FromTimePtr конвертирует *time.Time → *LocalTime (nil → nil).
func FromTimePtr(t *time.Time) *LocalTime {
	if t == nil {
		return nil
	}
	lt := LocalTime(*t)
	return &lt
}

// TimePtr конвертирует *LocalTime → *time.Time (nil → nil).
func (lt *LocalTime) TimePtr() *time.Time {
	if lt == nil {
		return nil
	}
	t := time.Time(*lt)
	return &t
}

// MarshalJSON — формат без Z; нулевое время → null.
func (lt LocalTime) MarshalJSON() ([]byte, error) {
	t := time.Time(lt)
	if t.IsZero() {
		return []byte("null"), nil
	}
	return []byte(`"` + t.Format(localTimeLayout) + `"`), nil
}

// UnmarshalJSON принимает и с Z, и без Z, и с миллисекундами, и дату без времени.
func (lt *LocalTime) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	if s == "" || s == "null" {
		*lt = LocalTime(time.Time{})
		return nil
	}
	for _, layout := range []string{
		localTimeLayout,
		"2006-01-02T15:04:05.000",
		"2006-01-02T15:04:05.999999999",
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02 15:04:05",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			*lt = LocalTime(t)
			return nil
		}
	}
	return fmt.Errorf("LocalTime: не удалось разобрать время %q", s)
}

// Value — запись в БД как time.Time (колонка timestamp без таймзоны).
func (lt LocalTime) Value() (driver.Value, error) {
	t := time.Time(lt)
	if t.IsZero() {
		return nil, nil
	}
	return t, nil
}

// Scan — чтение из БД. NULL → нулевое значение (для *LocalTime database/sql
// сам выставит nil-указатель при NULL).
func (lt *LocalTime) Scan(v interface{}) error {
	if v == nil {
		*lt = LocalTime(time.Time{})
		return nil
	}
	switch x := v.(type) {
	case time.Time:
		*lt = LocalTime(x)
		return nil
	case []byte:
		return lt.UnmarshalJSON(append(append([]byte(`"`), x...), '"'))
	case string:
		return lt.UnmarshalJSON([]byte(`"` + x + `"`))
	default:
		return fmt.Errorf("LocalTime: неподдерживаемый тип %T", v)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
//  Dislocation
// ─────────────────────────────────────────────────────────────────────────────

// Dislocation — одна запись о текущем местонахождении и состоянии вагона
// (таблица disl_actual). Данные приходят из выгрузки АСУ РЖД (SPV4664, JSON
// или xlsx из ЛК) и проходят конвейер:
//
//	parse_json.go / parse_lk.go  — сырой разбор источника в эту структуру;
//	Stage 1 (enrich_stage1.go)   — справочники станций/операций, Status, IdDisl, DateKon;
//	Stage 2 (enrich_stage2.go)   — словари marka/ports (имена клиента/груза/SMS),
//	                               перенос полей из предыдущей записи, IndexMain/IndexLast,
//	                               ToGo, RaschMsk;
//	Stage 3 (enrich_stage3.go)   — Perestanovka;
//	Stage 4 (enrich_stage4.go)   — распределение по ниткам расписания портов: ProgMsk,
//	                               DelayProg, Mistake.
//
// Источник отдаёт в основном КОДЫ (станций, грузов, дорог). Названия станций,
// ИМЕНА грузоотправителя/грузополучателя, клиента, группы грузов и координаты
// подставляются на этапах обогащения из справочников stations / marka / ports.
//
// Все поля даты/времени — LocalTime (в JSON и БД без метки часового пояса, без Z).
type Dislocation struct {

	// ── Основные идентификаторы ──────────────────────────────────────────────

	// ID — внутренний детерминированный первичный ключ (generateDeterministicID
	// от Vagon + CodeStationNach + DateNach). Стабилен между загрузками одного рейса.
	ID string `json:"id" db:"id"`

	// Vagon — номер вагона (NOM_VAG). Основной бизнес-ключ, индекс VagonMap.
	Vagon string `json:"vagon" db:"vagon"`

	// Invoice — номер накладной (NOM_NAK), нормализован normalizeCyrillic.
	Invoice string `json:"invoice" db:"invoice"`

	// InvoiceMain — основной (сводный) номер накладной. Может отличаться от Invoice
	// при перегруппировке состава.
	InvoiceMain string `json:"invoice_main" db:"invoice_main"`

	// ── Индексы поезда ───────────────────────────────────────────────────────

	// Index — текущий индекс поезда. Источник: INDEX_POEZD, отформатированный
	// parseJSONIndex в вид XXXX-XXX-XXXX. «Б/И» = без индекса (вагон не в поезде).
	Index string `json:"index" db:"index"`

	// IndexMain — «родительский» индекс, замороженный при ПЕРВОМ появлении вагона.
	// При парсинге временно пишется из NOM_POEZD, но в Stage 2 ВСЕГДА перезаписывается
	// (determineIndexMain / enrichFromDictionaries): берётся Index при первом появлении
	// и далее тянется неизменным. Ключ сопоставления с планом (ByIndexMain = IndexMain|IdDisl).
	// Фактический источник — INDEX_POEZD, а НЕ NOM_POEZD.
	IndexMain string `json:"index_main" db:"index_main"`

	// IndexLast — предыдущий Index, сохраняемый при смене текущего (determineIndexLast).
	IndexLast string `json:"index_last" db:"index_last"`

	// IndexPp — плановая (портовая) нитка прибытия, напр. «УТ-1 05:38». Проставляется
	// при загрузке плана (MA/NK), очищается и пересопоставляется при каждом новом плане.
	IndexPp string `json:"index_pp" db:"index_pp"`

	// ── Данные погрузки / отправления ────────────────────────────────────────

	// DateNach — дата начала рейса (DATE_NACH). NULL, если не погружен.
	DateNach *LocalTime `json:"date_nach" db:"date_nach"`

	// DateOtpr — дата отправления (DATE_OTPR). В АСУ-словаре описания нет; смысл
	// выведен из имени/значения, подтвердить. Отлична от DateNach (начала рейса).
	DateOtpr *LocalTime `json:"date_otpr" db:"date_otpr"`

	// CodeStationNach — код (ЕСР) станции отправления (STAN_NACH). Ключ для справочника
	// stations и словаря marka.
	CodeStationNach string `json:"code_station_nach" db:"code_station_nach"`

	// StationNach — название станции отправления. Stage 1 из stations по CodeStationNach.
	// Влияет на скорость доставки в calculateToGo (УЛАК, ЧЕГДОМЫН и пр.).
	StationNach string `json:"station_nach" db:"station_nach"`

	// DorogaNach — дорога станции отправления. Источник: DOR_NACH.
	DorogaNach string `json:"doroga_nach" db:"doroga_nach"`

	// StrNach — страна начала рейса (STR_NACH, код, напр. 643 = РФ).
	StrNach string `json:"str_nach" db:"str_nach"`

	// Zayavka — номер заявки на перевозку.
	// ИСТОЧНИК (новое): INV_CLAIM_NUMBER — номер заявки ГУ-12 (напр. «0045308868-ИЗМ-8»).
	// Ранее поле пустовало; задействуем под номер заявки ГУ-12, отдельную колонку не плодим.
	Zayavka string `json:"zayavka" db:"zayavka"`

	// ── Грузоотправитель ─────────────────────────────────────────────────────

	// GruzotprOkpo — ОКПО грузоотправителя (GRUZOTPR_OKPO). Часть составного ключа marka
	// (GruzotprOkpo + CodeStationNach + CodeCargo).
	GruzotprOkpo string `json:"gruzotpr_okpo" db:"gruzotpr_okpo"`

	// Gruzotpr — НАИМЕНОВАНИЕ грузоотправителя. Заполняется ОБОГАЩЕНИЕМ (Stage 2,
	// applyMarkaData): Gruzotpr ← marka.Shipper. При парсинге туда временно кладётся
	// 4-значный код предприятия GRUZOTPR (напр. «4212»), который затирается именем
	// при совпадении в словаре marka. Итог — имя, а не код и не ТГНЛ.
	Gruzotpr string `json:"gruzotpr" db:"gruzotpr"`

	// ── Назначение и грузополучатель ─────────────────────────────────────────

	// CodeStanNazn — код (ЕСР) станции назначения по накладной (STAN_NAZN).
	CodeStanNazn string `json:"code_stan_nazn" db:"code_stan_nazn"`

	// Code4StanNazn — четырёхзначный код станции назначения (stations.Kod4, Stage 1).
	Code4StanNazn string `json:"code4_stan_nazn" db:"code4_stan_nazn"`

	// StanNazn — название станции назначения. Stage 1 из stations. Значение
	// «МЫС АСТАФЬЕВА» включает логику выбора порта через naznach_station.
	StanNazn string `json:"stan_nazn" db:"stan_nazn"`

	// DorogaNazn — код дороги назначения (DOR_NAZN, 2 знака). В отличие от DorogaNach/
	// DorogaOper (имена из stations) хранит КОД; обогащение до имени — при необходимости.
	DorogaNazn string `json:"doroga_nazn" db:"doroga_nazn"`

	// StrNazn — страна назначения (STR_NAZN, код).
	StrNazn string `json:"str_nazn" db:"str_nazn"`

	// GruzpolOkpo — ОКПО грузополучателя (GRUZPOL_OKPO).
	GruzpolOkpo string `json:"gruzpol_okpo" db:"gruzpol_okpo"`

	// Gruzpol — НАИМЕНОВАНИЕ организации-грузополучателя. Заполняется ОБОГАЩЕНИЕМ
	// (Stage 2): Gruzpol ← port.Organisation (словарь ports по ключу OKPO + StanNazn).
	// При парсинге туда временно кладётся 4-значный код GRUZPOL (напр. «9862»),
	// затираемый именем. Итог — имя, а не код.
	Gruzpol string `json:"gruzpol" db:"gruzpol"`

	// GruzpolS — краткое имя грузополучателя/причала (УТ-1, АЭ, ГУТ-2) ← port.NameS.
	// База для Naznach, ключ группировки в агрегации и отчётах.
	GruzpolS string `json:"gruzpol_s" db:"gruzpol_s"`

	// Naznach — фактическое назначение в логике диспетчера. По умолчанию = GruzpolS;
	// для StanNazn = «МЫС АСТАФЬЕВА» может быть переопределено через naznach_station
	// или вручную. Маршрутизирует вагон в расписание Stage 4.
	Naznach string `json:"naznach" db:"naznach"`

	// Perestanovka — признак перестановки (Stage 3). Если Naznach ≠ GruzpolS →
	// «GruzpolS/Naznach» (напр. «УТ-1/АЭ»), иначе пусто.
	Perestanovka string `json:"perestanovka" db:"perestanovka"`

	// ── Данные груза ─────────────────────────────────────────────────────────

	// CodeCargo — код груза ЕТСНГ (KOD_GRZ_UCH). Часть ключа marka.
	CodeCargo string `json:"code_cargo" db:"code_cargo"`

	// CodeCargoGng — код груза по ГНГ (KOD_GRZ_GNG). Гармонизированная номенклатура
	// грузов; отлична от ЕТСНГ-кода CodeCargo.
	CodeCargoGng string `json:"code_cargo_gng" db:"code_cargo_gng"`

	// CodeCargoVygr — код груза выгрузки (KOD_GRZ_VYGR). В АСУ-словаре описания нет,
	// смысл выведен из имени — подтвердить.
	CodeCargoVygr string `json:"code_cargo_vygr" db:"code_cargo_vygr"`

	// CargoS — наименование груза из словаря marka (Stage 2).
	// Отличать от FreightExactName (точное наименование из источника).
	CargoS string `json:"cargo_s" db:"cargo_s"`

	// CargoSms — краткое имя груза для SMS/уведомлений (marka.Sms1, = Sms1).
	CargoSms string `json:"cargo_sms" db:"cargo_sms"`

	// CargoGroup — группа груза (УГОЛЬ, МЕТАЛЛ) из marka. Раздельное расписание Stage 4.
	CargoGroup string `json:"cargo_group" db:"cargo_group"`

	// Ves — масса груза в ТОННАХ. Источник VES_GRZ в килограммах, делится на 1000.
	Ves *float64 `json:"ves" db:"ves"`

	// PorozhPriznak — признак «порожний» (PPV_POR, 1 знак, «1»/«0»).
	// ВНИМАНИЕ: в JSONVagon сейчас НЕ объявлено — добавить и в парсер.
	PorozhPriznak string `json:"porozh_priznak" db:"porozh_priznak"`

	// FreightExactName — точное наименование груза из источника (FREIGHT_EXACT_NAME,
	// напр. «УГОЛЬ КАМЕННЫЙ МАРКИ Г(0-50) обогащенный»). В отличие от CargoS (из marka).
	FreightExactName string `json:"freight_exact_name" db:"freight_exact_name"`

	// GtdNumber — номер ГТД (FREIGHT_GTD_NUMBER, напр. «10006060/290525/5045423»).
	GtdNumber string `json:"gtd_number" db:"gtd_number"`

	// ── Операции ─────────────────────────────────────────────────────────────

	// TimeOp — дата-время последней грузовой операции (DATE_OP). Опора для RaschMsk/ToGo.
	TimeOp *LocalTime `json:"time_op" db:"time_op"`

	// DateOp — дата последней операции (= TimeOp). Входит в IdDisl.
	DateOp *LocalTime `json:"date_op" db:"date_op"`

	// DateOpJd — «железнодорожная» дата операции: = TimeOp, но если час ≥ 18, +1 сутки.
	// Используется в DateKon для статуса 10.
	DateOpJd *LocalTime `json:"date_op_jd" db:"date_op_jd"`

	// CodeOper — код грузовой операции (KOP_VMD). Код «92» = бросание поезда → Status 5.
	CodeOper string `json:"code_oper" db:"code_oper"`

	// Oper — полное наименование операции (Stage 1 из cargo_operations по CodeOper).
	Oper string `json:"oper" db:"oper"`

	// OperS — краткое наименование операции. Входит в составной ключ IdDisl.
	OperS string `json:"oper_s" db:"oper_s"`

	// CodeStationOper — код (ЕСР) станции последней операции (STAN_OP). Сравнивается
	// с CodeStationNach (→Status 0/1) и кодом StanNazn (→Status 10).
	CodeStationOper string `json:"code_station_oper" db:"code_station_oper"`

	// StationOper — название станции последней операции (Stage 1). Координаты на карте.
	StationOper string `json:"station_oper" db:"station_oper"`

	// DorogaOper — дорога станции операции. ИСТОЧНИК: DOR_RASCH (расчётная дорога).
	DorogaOper string `json:"doroga_oper" db:"doroga_oper"`

	// ── Идентификаторы отправки ──────────────────────────────────────────────

	// IdOtprk — номер отправки (ID_OTPRK, напр. «2083ЭФ162509»).
	IdOtprk string `json:"id_otprk" db:"id_otprk"`

	// Uno — код документа (UNO, напр. «001751722294»).
	Uno string `json:"uno" db:"uno"`

	// ── Географические данные ────────────────────────────────────────────────

	// Latitude — широта станции операции (Stage 1 из stations).
	Latitude string `json:"latitude" db:"latitude"`

	// Longitude — долгота станции операции (Stage 1 из stations).
	Longitude string `json:"longitude" db:"longitude"`

	// Temper — температура, °C (термочувствительные грузы). NULL если не задана.
	Temper *float64 `json:"temper" db:"temper"`

	// ── Расстояния ───────────────────────────────────────────────────────────

	// RasstStanNazn — расстояние до станции назначения, км (RASST_STAN_NAZN). Разбивает
	// calculateToGo на участки (>1364 / 1364–911 / <911 км). 0/NULL → ToGo = 72 ч.
	RasstStanNazn *int `json:"rasst_stan_nazn" db:"rasst_stan_nazn"`

	// RasstOb — расстояние общее (RASST_OB): от станции приёма груза до назначения, км.
	RasstOb *int `json:"rasst_ob" db:"rasst_ob"`

	// RasstStanOp — расстояние пройденное (RASST_STAN_OP), км.
	RasstStanOp *int `json:"rasst_stan_op" db:"rasst_stan_op"`

	// ── Простои ──────────────────────────────────────────────────────────────

	// ProstDn — простой, сутки (PROST_DN). Участвует в RaschMsk и в условии Status 4.
	ProstDn *int `json:"prost_dn" db:"prost_dn"`

	// ProstCh — простой, часы (PROST_CH). Участвует в RaschMsk и в условии Status 4 (≥12).
	ProstCh *int `json:"prost_ch" db:"prost_ch"`

	// ProstMin — простой под последней операцией, минуты (PROST_MIN).
	ProstMin *int `json:"prost_min" db:"prost_min"`

	// ── Идентификаторы и статусы ─────────────────────────────────────────────

	// IdDisl — составной ключ поезда (Stage 1): Index|CodeStationOper|OperS|DateOp.
	// С IndexMain образует ключ сопоставления с планом.
	IdDisl string `json:"id_disl" db:"id_disl"`

	// NppVag — порядковый номер вагона в составе (NPP_VAG). NULL если не указан.
	NppVag *int `json:"npp_vag" db:"npp_vag"`

	// Status — расчётный статус (calculateStatus, Stage 1):
	//   0  — Б/И на станции отправления;
	//   1  — на станции отправления, включён в поезд;
	//   2  — в пути (по умолчанию);
	//   4  — застрял на промежуточной станции (StationOper ≠ назн./отпр., CodeOper ≠ 92,
	//        ProstDn ≥ 1 или ProstCh ≥ 12) → ключ агрегации в IdStatus4;
	//   5  — поезд брошен (CodeOper = 92) → BrosQueue, ключ агрегации в IdStatus5;
	//   10 — прибыл на станцию назначения (StationOper = StanNazn).
	Status *int `json:"status" db:"status"`

	// IdStatus5 — ключ агрегации БРОШЕННЫХ вагонов (статус 5).
	// Формат createBrosKey: «Index|CodeStationOper|TimeOp». Вагоны одного броса делят
	// ключ → группируются в BrosQueue. ЗАМЕНЯЕТ прежнее использование Param1 под эту роль.
	IdStatus5 string `json:"id_status5" db:"id_status5"`

	// IdStatus4 — ключ агрегации вагонов со статусом 4 (длительный простой на
	// промежуточной станции), по аналогии с IdStatus5. Заполняется для Status = 4;
	// состав ключа определить при реализации (аналог createBrosKey).
	IdStatus4 string `json:"id_status4" db:"id_status4"`

	// DateDostav — нормативный срок доставки (DATE_DOSTAV). База для Delay и DelayProg.
	DateDostav *LocalTime `json:"date_dostav" db:"date_dostav"`

	// Delay — фактическая просрочка, сутки: (сегодня − DateDostav), если срок прошёл. Иначе NULL.
	Delay *int `json:"delay" db:"delay"`

	// DelayProg — прогнозная задержка, сутки: (ProgMsk − DateDostav). Stage 4.
	DelayProg *int `json:"delay_prog" db:"delay_prog"`

	// ── Планы и прогнозы ─────────────────────────────────────────────────────

	// PlanJd — плановая дата прибытия (JD-формат) из плана MA/NK. Очищается при новом плане.
	PlanJd *LocalTime `json:"plan_jd" db:"plan_jd"`

	// PlanMsk — плановая дата прибытия (МСК) — конкретный слот нитки порта.
	PlanMsk *LocalTime `json:"plan_msk" db:"plan_msk"`

	// ToGo — расчётное время хода до порта, часы (Stage 2 по RasstStanNazn; 0/NULL → 72.0).
	ToGo *float64 `json:"to_go" db:"to_go"`

	// RaschMsk — расчётное прибытие (МСК): TimeOp + ToGo + простой; для Status 0 +12 ч буфер.
	RaschMsk *LocalTime `json:"rasch_msk" db:"rasch_msk"`

	// ProgMsk — прогнозное прибытие (МСК), нитка из расписания порта (Stage 4). Для
	// поездов с PlanMsk = PlanMsk; иначе ближайший свободный слот ≥ RaschMsk − 6 ч.
	// При Status 10 сбрасывается в nil.
	ProgMsk *LocalTime `json:"prog_msk" db:"prog_msk"`

	// Mistake — «необъяснённый простой», дни: ProgMsk − (RaschMsk + штраф броса). Stage 4.
	Mistake *float64 `json:"mistake" db:"mistake"`

	// RaschJd — RaschMsk в JD-формате (если час ≥ 18, +1 сутки).
	RaschJd *LocalTime `json:"rasch_jd" db:"rasch_jd"`

	// ProgJd — ProgMsk в JD-формате (если час ≥ 18, +1 сутки).
	ProgJd *LocalTime `json:"prog_jd" db:"prog_jd"`

	// DateKon — РАСЧЁТНАЯ дата закрытия рейса (Stage 1): Status 10 → DateOpJd;
	// Status 2 → nil; иначе TimeOp. НЕ из источника (расчётное).
	DateKon *LocalTime `json:"date_kon" db:"date_kon"`

	// DatePrib — фактическая дата прибытия на станцию назначения (источник DATE_PRIB /
	// ЛК «прибытие АСОУП»). Достоверная дата; используется для сопоставления.
	// (DATE_KON «окончание рейса» оказался ненадёжным — не парсится.)
	DatePrib *LocalTime `json:"date_prib" db:"date_prib"`

	// ── Маршрут ──────────────────────────────────────────────────────────────

	// IsBam — маркер движения вагона по БАМ (Байкало-Амурская магистраль). Новое поле.
	// В источнике прямого признака нет; заполняется отдельной логикой по маршруту/дорогам.
	// По умолчанию false.
	IsBam bool `json:"is_bam" db:"is_bam"`

	// ── Собственник и оператор вагона (новые поля) ───────────────────────────

	// CarOwnerName — наименование собственника вагона (CAR_OWNER_NAME, напр. АО «КФС-Транс»).
	// Под него переезжает экспортная колонка «Собственник» (ранее ошибочно бравшая RodVagUch).
	CarOwnerName string `json:"car_owner_name" db:"car_owner_name"`

	// CarOwnerOkpo — ОКПО собственника вагона (CAR_OWNER_OKPO).
	CarOwnerOkpo string `json:"car_owner_okpo" db:"car_owner_okpo"`

	// CarTenantName — наименование оператора/арендатора вагона (CAR_TENANT_NAME, напр. АО «НТК»).
	CarTenantName string `json:"car_tenant_name" db:"car_tenant_name"`

	// CarTenantOkpo — ОКПО оператора/арендатора вагона (CAR_TENANT_OKPO).
	CarTenantOkpo string `json:"car_tenant_okpo" db:"car_tenant_okpo"`

	// ── Клиент и пользовательские поля из словаря marka ─────────────────────

	// Client — имя клиента (marka, Stage 2).
	Client string `json:"client" db:"client"`

	// Sms1..3 — метки для SMS/уведомлений (marka.Sms1..3). Sms1 = CargoSms.
	Sms1 string `json:"sms_1" db:"sms_1"`
	Sms2 string `json:"sms_2" db:"sms_2"`
	Sms3 string `json:"sms_3" db:"sms_3"`

	// Sprav1..3 — справочные строки из marka (расширенная агрегация).
	Sprav1 string `json:"sprav_1" db:"sprav_1"`
	Sprav2 string `json:"sprav_2" db:"sprav_2"`
	Sprav3 string `json:"sprav_3" db:"sprav_3"`

	// Param1 — общий текстовый параметр из справочника (dislocation_directory, bulk-update,
	// расширенная агрегация). ВАЖНО: ключ бросков сюда БОЛЬШЕ НЕ пишется — он в IdStatus5.
	// Экспорт «Данные броса» переключить с Param1 на IdStatus5.
	Param1 string `json:"param_1" db:"param_1"`
	Param2 string `json:"param_2" db:"param_2"`
	Param3 string `json:"param_3" db:"param_3"`

	// NParam1..3 — числовые параметры, хранятся как строки (совместимость с БД/API).
	NParam1 string `json:"n_param_1" db:"n_param_1"`
	NParam2 string `json:"n_param_2" db:"n_param_2"`
	NParam3 string `json:"n_param_3" db:"n_param_3"`

	// DateVigr — дата выгрузки. NULL если не выгружен.
	DateVigr *LocalTime `json:"date_vigr" db:"date_vigr"`

	// PlaceVigr — место выгрузки (склад/стивидор/причал).
	PlaceVigr string `json:"place_vigr" db:"place_vigr"`

	// Frost — признак заморозки/температурный класс. NULL если не применимо.
	Frost *int `json:"frost" db:"frost"`

	// Info1..3 — свободные информационные строки диспетчера.
	Info1 string `json:"info_1" db:"info_1"`
	Info2 string `json:"info_2" db:"info_2"`
	Info3 string `json:"info_3" db:"info_3"`

	// Color — цветовая метка вагона для фронта (marka, расширенная агрегация).
	Color string `json:"color" db:"color"`

	// ── Дополнительные технические поля ──────────────────────────────────────

	// RodVagUch — КОД РОДА ВАГОНА (ROD_VAG_UCH, АСУ: «Код рода вагона», 2 знака).
	// Теперь реально парсится. Это НЕ собственник — собственник в CarOwnerName/CarOwnerOkpo.
	RodVagUch string `json:"rod_vag_uch" db:"rod_vag_uch"`

	// Shipments — связанные отгрузки/партии (ExtendedCollectiveSubGroup).
	Shipments string `json:"shipments" db:"shipments"`

	// History — флаг запроса истории продвижения вагона по API (601):
	// 0 — не запрашивалась, иначе — признак того, что историю нужно/запросили.
	History int `json:"history" db:"history"`

	// ── Служебные временные метки ────────────────────────────────────────────

	// CreatedAt — время создания записи.
	CreatedAt LocalTime `json:"created_at" db:"created_at"`

	// UpdatedAt — время последнего обновления записи.
	UpdatedAt LocalTime `json:"updated_at" db:"updated_at"`
}
