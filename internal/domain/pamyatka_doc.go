package domain

// PamyatkaDoc — памятка ГУ-45 в «сыром» виде: документ, как он лежит у
// провайдера (ручка <base>/<client>/reference?number=<n>). Это ДРУГОЙ контракт,
// не тот, что у инкремента (см. [Pamyatka]): инкремент отдаёт выжимку —
// номер, дату, место, времена вагонов; сырой документ несёт весь бланк ГУ-45,
// включая род и код груза по каждому вагону, код станции, собственника вагона
// и реквизиты владельца пути.
//
// Соотношение с [Pamyatka] на одном и том же документе (сверено на 11013/attis):
// состав вагонов совпадает вагон-в-вагон, времена подачи те же. Сверх выжимки
// сырой документ даёт: CargoCode/CargoName, StationCode, OwnerCode, AdmCode,
// NumberMemo, реквизиты сторон, состояние документа (DocState) и точное время
// его регистрации (DocDate — с часами, тогда как DATE_CREATE инкремента только
// дата, и это РАЗНЫЕ даты: у 11013 DATE_CREATE 19.07, DocDate 20.07 01:22:37).
//
// В vagon_history этот документ не раскладывается — вехи ведёт инкремент.
type PamyatkaDoc struct {
	// Client — код клиента провайдера (attis/nmtp) из пути запроса. В теле
	// ответа его нет, а номер уникален лишь в пределах клиента.
	Client string

	// --- реквизиты обработки документа (блок _summary) ---
	DocID       string     // DocId — идентификатор документа у провайдера
	Number      string     // DocNum, он же number внутри бланка (сверены при разборе)
	DocDate     *LocalTime // DocDate — момент регистрации документа («20.07.2026 01:22:37»)
	DocLastOper *LocalTime // DocLastOper — последняя операция над документом
	DocState    string     // DocState: «Подписан Клиентом» / «Подписан ОАО «РЖД»» / «Автоматически согласован»
	DocStateID  string     // DocStateId — код состояния (469/548/696)

	// --- реквизиты составления (Metadatatab внутри getPPSReply) ---
	// Кто оформил документ и когда — этого нет ни в _summary, ни в бланке.
	// В боевой выборке 28.07 (194 документа) блок есть у всех и заполнен.
	Creator     string     // doc_creator — составитель («Бранд С. А. Приемосдатчик груза и багажа»)
	Signatories string     // signatories — подписанты строкой как пришла («/Фамилия И. О.-дд.мм.гггг-чч:мм:сс»)
	ComposedAt  *LocalTime // doc_date + doc_time — когда памятку составили (раньше DocDate — момента регистрации)
	RailwayCode string     // railway_code — код дороги (96 — Дальневосточная)

	// --- бланк ГУ-45 (блок _decoded_doc.data) ---
	OperType   string // oper_type: «подачу» | «уборку»
	OperCode   string // oper_code (в наблюдённых данных всегда «0»)
	GetPlace   string // get_place — место подачи/уборки, свободный текст
	GetBy      string // get_by: «ОАО "РЖД"» | «Владельца (Пользователя)»
	GetByID    string // get_by_id — код того же признака (1/0)
	OrgID      string // orgid — организация у провайдера
	TextMark   string // text_mark — отметка свободным текстом (обычно пусто)
	ClientMark string // client_mark: «принял» | «сдал»
	PersonMark string // person_mark — встречная отметка

	RailwayName string // railway_name — дорога («Дальневосточная»)
	StationCode string // railway_station_code — код станции ЕСР (985609)
	StationName string // railway_station_name — станция («МЫС АСТАФЬЕВА», в верхнем регистре)

	PathOwner  PamyatkaParty // владелец пути (path_owner_*), есть всегда
	Contragent PamyatkaParty // контрагент (contragent_*), опционален — только у части документов

	Vagons []PamyatkaDocVagon
}

// PamyatkaParty — сторона документа (владелец пути / контрагент). Состав
// реквизитов плавает от документа к документу: где-то у владельца пути есть
// ИНН/КПП, где-то нет; блок контрагента встречается у обоих клиентов, но не
// у всех документов и не всегда полностью (бывает name+okpo без ИНН/КПП).
// Пустое поле означает «источник его не прислал».
type PamyatkaParty struct {
	Name string // *_name
	OKPO string // *_okpo
	INN  string // *_inn
	KPP  string // *_kpp
}

// IsZero — сторона не заполнена вовсе (источник не прислал ни одного реквизита).
func (p PamyatkaParty) IsZero() bool {
	return p.Name == "" && p.OKPO == "" && p.INN == "" && p.KPP == ""
}

// PamyatkaDocVagon — строка вагона в бланке. Времена собраны из пар источника
// «дата дд.мм» + «время чч:мм» — год в них не передаётся и восстановлен по дате
// документа (та же логика, что в инкременте), поэтому корректен на стыке лет.
type PamyatkaDocVagon struct {
	Order     string // order_number — порядковый номер строки в бланке
	Vagon     string // wagon_number (8 цифр)
	AdmCode   string // adm_code — код администрации-собственника (у части вагонов пусто)
	OwnerCode string // wagon_owner_code: «СОБ» | «АРС» — признак принадлежности

	CargoCode string // cargo_code — код груза ЕТСНГ (6 цифр), в выжимке инкремента его нет
	CargoName string // cargo_name — наименование груза

	GrOperationType string // gr_oper_type: «вгр» | «пгр» | «боп»
	NumberMemo      string // number_memo — ссылка на встречный документ строкой («Памятка подачи №10817»), у части строк пусто

	GetIn  *LocalTime // get_in_date + get_in_time — подача
	Report *LocalTime // report_date + report_time — уведомление об окончании грузовой операции
	GetOut *LocalTime // get_out_date + get_out_time — уборка

	// Containers — блок контейнеров как пришёл (сырой JSON). Во всей боевой
	// выборке 28.07.2026 (231 вагон, оба клиента) поле присутствует и всегда
	// null, поэтому его структура неизвестна. Держим сырым, чтобы непустое
	// значение не потерялось молча; разбирать — когда появится образец.
	Containers string
}
