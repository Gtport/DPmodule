package parser

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Gtport/DPmodule/internal/domain"
)

// Формат времён обработки документа: «20.07.2026 01:22:37». Вагонные даты идут
// в тех же «дд.мм» + «чч:мм», что и в инкременте, — год восстанавливаем той же
// vagonTime, только точкой отсчёта служит дата самого документа.
const (
	refDocDateLayout  = "02.01.2006 15:04:05"
	refComposedLayout = "02.01.2006 15:04" // doc_date + doc_time из Metadatatab, без секунд
)

// ReferenceDocBatch — результат разбора ответа <client>/reference?number=<n>.
//
// ⚠️ Ответ на запрос по номеру — НЕ один документ. Провайдер хранит памятки
// пачками, как они к нему приехали, и по номеру находит пачку-контейнер, отдавая
// её целиком (боевая проверка 28.07.2026: attis number=11013 и number=11017 дают
// байт в байт один ответ из 5 документов; nmtp — 18). Несуществующий номер даёт
// HTTP 404 с телом {"error":"pamyatka not found"} — до разбора не доходит, его
// отбивает HTTP-клиент. Нужный документ выбирается из пачки методом [ByNumber].
type ReferenceDocBatch struct {
	Client  string // код клиента провайдера из пути запроса (в теле его нет)
	Message string // человекочитаемый итог провайдера: «Получено 5 памяток ГУ-45 (ATTIS)»

	// ReceivedAt — момент забора пачки провайдером, ДОСЛОВНО как пришёл
	// («2026-07-20T02:48:39.673Z»). Строкой, а не временем, намеренно: суффикс Z
	// говорит, что поле не в московском времени (сверка с DocLastOper это
	// подтверждает), а конвертировать время в проекте запрещено. Поле служебное —
	// показывает, насколько свежа пачка у провайдера.
	ReceivedAt string

	NewOperID string // new_oper_id — курсор провайдера по пачке
	Docs      []domain.PamyatkaDoc
}

// ByNumber достаёт из пачки документ с нужным номером — тот, ради которого
// делался запрос. Второе значение false, если такого номера в пачке нет.
func (b ReferenceDocBatch) ByNumber(number string) (domain.PamyatkaDoc, bool) {
	for i := range b.Docs {
		if b.Docs[i].Number == number {
			return b.Docs[i], true
		}
	}
	return domain.PamyatkaDoc{}, false
}

// --- сырой контракт источника ---

type refDocEnvelope struct {
	Code       int        `json:"code"`
	Status     string     `json:"status"`
	Message    string     `json:"message"`
	ReceivedAt string     `json:"received_at"`
	Error      string     `json:"error"` // тело отказа провайдера: {"error":"pamyatka not found"}
	Data       refDocData `json:"data"`
}

type refDocData struct {
	Count     int          `json:"count"`
	NewOperID string       `json:"new_oper_id"`
	Documents []refDocItem `json:"documents"`
}

// refDocItem — один документ пачки. Из getPPSReply (ответ системы-источника)
// берём только Metadatatab — реквизиты составления, которых нет ни в _summary,
// ни в бланке. Сам бланк оттуда не читаем: он лежит там в base64 (Doccontent,
// ~19 КБ на документ), а _decoded_doc — его полный декод, сверка XML-тегов с
// ключами JSON на 23 боевых документах не выявила ни одного потерянного поля.
// Docsigntab (ЭЦП, килобайты PKCS#7) не разбираем по решению владельца.
type refDocItem struct {
	Summary  refDocSummary `json:"_summary"`
	Decoded  refDecodedDoc `json:"_decoded_doc"`
	PPSReply refPPSReply   `json:"getPPSReply"`
}

type refPPSReply struct {
	Request struct {
		Documentdata struct {
			Metadatatab refMetaTab `json:"Metadatatab"`
		} `json:"Documentdata"`
	} `json:"Request"`
}

// refMetaTab — список пар ключ-значение. Как и у вагонов, источник приезжает
// конвертацией из XML, поэтому единственный элемент может прийти объектом:
// в боевой выборке так пока не случалось (194 документа, всегда массив), но
// Docsigntab рядом ведёт себя ровно так, поэтому разбираем оба вида.
type refMetaTab struct {
	Item []refMetaItem `json:"item"`
}

type refMetaItem struct {
	Key   string `json:"Key"`
	Value string `json:"Value"`
}

func (m *refMetaTab) UnmarshalJSON(b []byte) error {
	var probe struct {
		Item json.RawMessage `json:"item"`
	}
	if err := json.Unmarshal(b, &probe); err != nil {
		return err
	}
	trimmed := strings.TrimSpace(string(probe.Item))
	if trimmed == "" || trimmed == "null" {
		m.Item = nil
		return nil
	}
	if trimmed[0] == '[' {
		return json.Unmarshal(probe.Item, &m.Item)
	}
	var one refMetaItem
	if err := json.Unmarshal(probe.Item, &one); err != nil {
		return err
	}
	m.Item = []refMetaItem{one}
	return nil
}

func (m refMetaTab) get(key string) string {
	for _, it := range m.Item {
		if it.Key == key {
			return it.Value
		}
	}
	return ""
}

type refDocSummary struct {
	DocID       string `json:"DocId"`
	DocNum      string `json:"DocNum"`
	DocDate     string `json:"DocDate"`
	DocState    string `json:"DocState"`
	DocStateID  string `json:"DocStateId"`
	DocLastOper string `json:"DocLastOper"`
}

type refDecodedDoc struct {
	Data refDocBody `json:"data"`
}

type refDocBody struct {
	Number     string `json:"number"`
	OrgID      string `json:"orgid"`
	OperCode   string `json:"oper_code"`
	OperType   string `json:"oper_type"`
	GetPlace   string `json:"get_place"`
	GetBy      string `json:"get_by"`
	GetByID    string `json:"get_by_id"`
	TextMark   string `json:"text_mark"`
	ClientMark string `json:"client_mark"`
	PersonMark string `json:"person_mark"`

	RailwayName string `json:"railway_name"`
	StationCode string `json:"railway_station_code"`
	StationName string `json:"railway_station_name"`

	PathOwnerName string `json:"path_owner_name"`
	PathOwnerOkpo string `json:"path_owner_okpo"`
	PathOwnerINN  string `json:"path_owner_inn"`
	PathOwnerKPP  string `json:"path_owner_kpp"`

	ContragentName string `json:"contragent_name"`
	ContragentOkpo string `json:"contragent_okpo"`
	ContragentINN  string `json:"contragent_inn"`
	ContragentKPP  string `json:"contragent_kpp"`

	Wagons refWagonList `json:"wagons"`
}

// refWagonList — обёртка над списком вагонов. Бланк приезжает конвертацией из
// XML, а она не различает «один элемент» и «список из одного»: в памятке с одним
// вагоном wagons.wagon приходит ОБЪЕКТОМ, а не массивом (5 из 18 документов
// nmtp в боевой выборке). Разбираем оба вида — иначе половина уборочных памяток
// роняет парсер.
type refWagonList struct {
	Wagon []refDocVagon `json:"wagon"`
}

func (l *refWagonList) UnmarshalJSON(b []byte) error {
	var probe struct {
		Wagon json.RawMessage `json:"wagon"`
	}
	if err := json.Unmarshal(b, &probe); err != nil {
		return err
	}
	trimmed := strings.TrimSpace(string(probe.Wagon))
	if trimmed == "" || trimmed == "null" {
		l.Wagon = nil
		return nil
	}
	if trimmed[0] == '[' {
		return json.Unmarshal(probe.Wagon, &l.Wagon)
	}
	var one refDocVagon
	if err := json.Unmarshal(probe.Wagon, &one); err != nil {
		return err
	}
	l.Wagon = []refDocVagon{one}
	return nil
}

type refDocVagon struct {
	OrderNumber     string          `json:"order_number"`
	WagonNumber     string          `json:"wagon_number"`
	AdmCode         string          `json:"adm_code"`
	WagonOwnerCode  string          `json:"wagon_owner_code"`
	CargoCode       string          `json:"cargo_code"`
	CargoName       string          `json:"cargo_name"`
	GrOperationType string          `json:"gr_oper_type"`
	NumberMemo      string          `json:"number_memo"`
	GetInDate       string          `json:"get_in_date"`
	GetInTime       string          `json:"get_in_time"`
	ReportDate      string          `json:"report_date"`
	ReportTime      string          `json:"report_time"`
	GetOutDate      string          `json:"get_out_date"`
	GetOutTime      string          `json:"get_out_time"`
	Containers      json.RawMessage `json:"containers"`
}

// ParseReferenceDocByNumber — ОСНОВНОЙ путь разбора: из ответа берётся ровно
// тот документ, ради которого делался запрос, остальные не трогаются.
//
// Так и надо ходить за памяткой (решение владельца 28.07). Соседи по пачке с
// запрошенным документом не связаны — их роднит только клиент и час оформления
// (станция общая у половины пачек, путь подачи всегда разный, вагоны
// пересекаются у 1 пары из 986). Разбирать их — значит без нужды зависеть от
// чужих данных: сломанный попутчик отнял бы у нас исправную памятку.
//
// Номера нет в пачке — ошибка: провайдер отдал не то, что просили.
func ParseReferenceDocByNumber(raw []byte, client, number string) (domain.PamyatkaDoc, error) {
	env, err := parseRefDocEnvelope(raw, client)
	if err != nil {
		return domain.PamyatkaDoc{}, err
	}
	for i := range env.Data.Documents {
		if env.Data.Documents[i].Summary.DocNum == number {
			return convertPamyatkaDoc(&env.Data.Documents[i], client)
		}
	}
	got := make([]string, 0, len(env.Data.Documents))
	for i := range env.Data.Documents {
		got = append(got, env.Data.Documents[i].Summary.DocNum)
	}
	return domain.PamyatkaDoc{}, fmt.Errorf("памятка %s/%s: в ответе её нет, пришли номера [%s]",
		client, number, strings.Join(got, " "))
}

// ParseReferenceDoc разбирает пачку целиком — для диагностики, когда нужно
// увидеть, что провайдер сложил в одну порцию. За конкретной памяткой ходить
// через [ParseReferenceDocByNumber]: пачка приходит «до 19 документов», и
// разбирать их все ради одного нужного незачем.
//
// Ошибка любого документа прерывает разбор всей пачки: молча отдать «часть
// разобралась» здесь хуже, чем отказать — расхождение с бланком нужно увидеть.
func ParseReferenceDoc(raw []byte, client string) (ReferenceDocBatch, error) {
	env, err := parseRefDocEnvelope(raw, client)
	if err != nil {
		return ReferenceDocBatch{}, err
	}

	out := ReferenceDocBatch{
		Client:     client,
		Message:    env.Message,
		ReceivedAt: env.ReceivedAt,
		NewOperID:  env.Data.NewOperID,
		Docs:       make([]domain.PamyatkaDoc, 0, len(env.Data.Documents)),
	}
	for i := range env.Data.Documents {
		d, err := convertPamyatkaDoc(&env.Data.Documents[i], client)
		if err != nil {
			return ReferenceDocBatch{}, err
		}
		out.Docs = append(out.Docs, d)
	}
	return out, nil
}

// parseRefDocEnvelope — общая часть обоих путей: разбор конверта и проверки
// того, что провайдер вообще ответил делом, а не отказом.
func parseRefDocEnvelope(raw []byte, client string) (refDocEnvelope, error) {
	var env refDocEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return refDocEnvelope{}, fmt.Errorf("reference (%s): парсинг JSON: %w", client, err)
	}
	if env.Error != "" {
		return refDocEnvelope{}, fmt.Errorf("reference (%s): провайдер отказал: %s", client, env.Error)
	}
	if env.Code != 0 || (env.Status != "" && env.Status != "success") {
		return refDocEnvelope{}, fmt.Errorf("reference (%s): провайдер вернул code=%d status=%q: %s",
			client, env.Code, env.Status, env.Message)
	}
	if env.Data.Count != len(env.Data.Documents) {
		return refDocEnvelope{}, fmt.Errorf("reference (%s): count=%d, а документов %d",
			client, env.Data.Count, len(env.Data.Documents))
	}
	return env, nil
}

func convertPamyatkaDoc(src *refDocItem, client string) (domain.PamyatkaDoc, error) {
	s, body := &src.Summary, &src.Decoded.Data

	if s.DocNum == "" {
		return domain.PamyatkaDoc{}, fmt.Errorf("памятка %s: в _summary нет DocNum", client)
	}
	// Номер лежит в двух местах: в реквизитах обработки и внутри самого бланка.
	// Расхождение означало бы, что сводка и бланк — от разных документов.
	if body.Number != "" && body.Number != s.DocNum {
		return domain.PamyatkaDoc{}, fmt.Errorf("памятка %s/%s: номер в бланке (%s) не совпадает с DocNum",
			client, s.DocNum, body.Number)
	}
	if len(body.Wagons.Wagon) == 0 {
		return domain.PamyatkaDoc{}, fmt.Errorf("памятка %s/%s: в бланке нет вагонов", client, s.DocNum)
	}

	docDate, err := time.Parse(refDocDateLayout, s.DocDate)
	if err != nil {
		return domain.PamyatkaDoc{}, fmt.Errorf("памятка %s/%s: DocDate %q не в формате «дд.мм.гггг чч:мм:сс»: %w",
			client, s.DocNum, s.DocDate, err)
	}
	lastOper, err := optDocTime(s.DocLastOper, client, s.DocNum, "DocLastOper")
	if err != nil {
		return domain.PamyatkaDoc{}, err
	}

	meta := src.PPSReply.Request.Documentdata.Metadatatab
	composed, err := composedAt(meta.get("doc_date"), meta.get("doc_time"), client, s.DocNum)
	if err != nil {
		return domain.PamyatkaDoc{}, err
	}

	doc := domain.PamyatkaDoc{
		Client:      client,
		DocID:       s.DocID,
		Number:      s.DocNum,
		DocDate:     domain.NewLocalTime(docDate),
		DocLastOper: lastOper,
		DocState:    s.DocState,
		DocStateID:  s.DocStateID,

		Creator:     meta.get("doc_creator"),
		Signatories: meta.get("signatories"),
		ComposedAt:  composed,
		RailwayCode: meta.get("railway_code"),

		OperType:   body.OperType,
		OperCode:   body.OperCode,
		GetPlace:   body.GetPlace,
		GetBy:      body.GetBy,
		GetByID:    body.GetByID,
		OrgID:      body.OrgID,
		TextMark:   body.TextMark,
		ClientMark: body.ClientMark,
		PersonMark: body.PersonMark,

		RailwayName: body.RailwayName,
		StationCode: body.StationCode,
		StationName: body.StationName,

		PathOwner: domain.PamyatkaParty{
			Name: body.PathOwnerName, OKPO: body.PathOwnerOkpo,
			INN: body.PathOwnerINN, KPP: body.PathOwnerKPP,
		},
		Contragent: domain.PamyatkaParty{
			Name: body.ContragentName, OKPO: body.ContragentOkpo,
			INN: body.ContragentINN, KPP: body.ContragentKPP,
		},
		Vagons: make([]domain.PamyatkaDocVagon, 0, len(body.Wagons.Wagon)),
	}

	for _, w := range body.Wagons.Wagon {
		getIn, err := vagonTime(w.GetInDate, w.GetInTime, docDate, "get_in", client, s.DocNum)
		if err != nil {
			return domain.PamyatkaDoc{}, err
		}
		report, err := vagonTime(w.ReportDate, w.ReportTime, docDate, "report", client, s.DocNum)
		if err != nil {
			return domain.PamyatkaDoc{}, err
		}
		getOut, err := vagonTime(w.GetOutDate, w.GetOutTime, docDate, "get_out", client, s.DocNum)
		if err != nil {
			return domain.PamyatkaDoc{}, err
		}
		doc.Vagons = append(doc.Vagons, domain.PamyatkaDocVagon{
			Order:           w.OrderNumber,
			Vagon:           w.WagonNumber,
			AdmCode:         w.AdmCode,
			OwnerCode:       w.WagonOwnerCode,
			CargoCode:       w.CargoCode,
			CargoName:       w.CargoName,
			GrOperationType: w.GrOperationType,
			NumberMemo:      w.NumberMemo,
			GetIn:           getIn,
			Report:          report,
			GetOut:          getOut,
			Containers:      rawOrEmpty(w.Containers),
		})
	}
	return doc, nil
}

// optDocTime — время обработки документа, которого может не быть.
func optDocTime(v, client, number, field string) (*domain.LocalTime, error) {
	if v == "" {
		return nil, nil
	}
	t, err := time.Parse(refDocDateLayout, v)
	if err != nil {
		return nil, fmt.Errorf("памятка %s/%s: %s %q не в формате «дд.мм.гггг чч:мм:сс»: %w",
			client, number, field, v, err)
	}
	return domain.NewLocalTime(t), nil
}

// composedAt собирает время составления документа из пары Metadatatab
// «doc_date» + «doc_time» («14.07.2026» + «21:45»). Год здесь передан, гадать
// не нужно. Обоих полей нет → nil; есть только одно или формат чужой — ошибка.
func composedAt(datePart, timePart, client, number string) (*domain.LocalTime, error) {
	if datePart == "" && timePart == "" {
		return nil, nil
	}
	if datePart == "" || timePart == "" {
		return nil, fmt.Errorf("памятка %s/%s: doc_date/doc_time должны идти парой (дата %q, время %q)",
			client, number, datePart, timePart)
	}
	t, err := time.Parse(refComposedLayout, datePart+" "+timePart)
	if err != nil {
		return nil, fmt.Errorf("памятка %s/%s: время составления %q %q не в формате «дд.мм.гггг чч:мм»: %w",
			client, number, datePart, timePart, err)
	}
	return domain.NewLocalTime(t), nil
}

// rawOrEmpty отдаёт сырой JSON строкой; null и отсутствие поля — пустая строка.
func rawOrEmpty(v json.RawMessage) string {
	s := strings.TrimSpace(string(v))
	if s == "null" {
		return ""
	}
	return s
}
