package domain

// Pamyatka — памятка на подачу/уборку от внешнего провайдера (ручка
// <base>/<client>/reference/update, «обработанный» контракт
// {LAST_UPDATE, PAMYATKI:[…]}). Сырая полная памятка (забор по номеру) —
// отдельный формат, он не разбирается и доменной модели не имеет.
type Pamyatka struct {
	// Client — код клиента провайдера (attis/nmtp). В теле ответа его нет
	// (клиент — в пути запроса), но номер памятки уникален только в пределах
	// клиента, поэтому без него запись неадресуема.
	Client        string
	Number        string     // NUMBER_PAMYATKA
	DateCreate    *LocalTime // DATE_CREATE, только дата (в источнике — MM-DD-YYYY)
	OperationType string     // OPERATION_TYPE: «подачу» | «уборку»
	GetPlace      string     // GET_PLACE — место подачи/уборки, свободный текст
	NameStation   string     // NAME_STATION — станция
	PathOwnerOkpo string     // PATH_OWNER_OKPO — ОКПО владельца пути
	Vagons        []PamyatkaVagon
}

// PamyatkaVagon — вагон в составе памятки. Времена собраны из пар источника
// «дата дд.мм» + «время чч:мм»; год источник не передаёт — он восстановлен
// по дате создания памятки (ближайший к ней), поэтому корректен на стыке лет.
type PamyatkaVagon struct {
	Vagon           string     // NUMBER_VAGON (8 цифр)
	GrOperationType string     // GR_OPERATION_TYPE: «вгр» / «пгр» / «боп»
	GetIn           *LocalTime // подача: GET_IN_DATE + GET_IN_TIME (есть всегда)
	Report          *LocalTime // уведомление: REPORT_DATE + REPORT_TIME (может отсутствовать)
	GetOut          *LocalTime // уборка: GET_OUT_DATE + GET_OUT_TIME (может отсутствовать)
}
