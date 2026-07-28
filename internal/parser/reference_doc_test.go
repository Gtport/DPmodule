package parser

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Golden-файлы — боевые ответы <client>/reference?number=<n> от 2026-07-28,
// урезанные ради размера: оставлена часть документов пачки (count пересчитан
// под них, message остался от полной пачки — «Получено 5/18», это текст
// провайдера) и усечён base64 Doccontent внутри getPPSReply, который парсер
// не читает. Сами блоки _summary и _decoded_doc — дословные.
const (
	goldenAttis = "testdata/pamyatka_raw_attis.json"
	goldenNmtp  = "testdata/pamyatka_raw_nmtp.json"
)

// Ответ по номеру отдаёт пачку целиком, а не один документ, — проверяем и
// разбор конверта, и выбор нужного документа из пачки.
func TestParseReferenceDoc_GoldenAttis(t *testing.T) {
	raw, err := os.ReadFile(goldenAttis)
	require.NoError(t, err)

	batch, err := ParseReferenceDoc(raw, "attis")
	require.NoError(t, err)

	assert.Equal(t, "attis", batch.Client)
	assert.Equal(t, "Получено 5 памяток ГУ-45 (ATTIS)", batch.Message)
	assert.Equal(t, "2026-07-20T02:48:39.673Z", batch.ReceivedAt, "момент забора — дословно, без конверсии")
	assert.Equal(t, "13025673368", batch.NewOperID)
	require.Len(t, batch.Docs, 2)

	doc, ok := batch.ByNumber("11013")
	require.True(t, ok, "запрошенный номер должен найтись в пачке")

	assert.Equal(t, "1766398397", doc.DocID)
	assert.Equal(t, "2026-07-20T01:22:37", doc.DocDate.String())
	assert.Equal(t, "2026-07-20T04:22:12", doc.DocLastOper.String())
	assert.Equal(t, "Подписан Клиентом", doc.DocState)
	assert.Equal(t, "469", doc.DocStateID)

	assert.Equal(t, "подачу", doc.OperType)
	assert.Equal(t, "Аттис -1 путь", doc.GetPlace)
	assert.Equal(t, `ОАО "РЖД"`, doc.GetBy)
	assert.Equal(t, "переведена 1 стрелка", doc.TextMark)
	assert.Equal(t, "принял", doc.ClientMark)
	assert.Equal(t, "сдал", doc.PersonMark)
	assert.Equal(t, "985609", doc.StationCode, "код станции — чего в выжимке инкремента нет")
	assert.Equal(t, "МЫС АСТАФЬЕВА", doc.StationName)
	assert.Equal(t, "Дальневосточная", doc.RailwayName)

	assert.Equal(t, "10230304", doc.PathOwner.OKPO)
	assert.Equal(t, "2508033063", doc.PathOwner.INN)
	assert.Equal(t, "250801001", doc.PathOwner.KPP)
	assert.True(t, doc.Contragent.IsZero(), "в этом документе блока контрагента нет")

	require.Len(t, doc.Vagons, 19)
	first := doc.Vagons[0]
	assert.Equal(t, "1", first.Order)
	assert.Equal(t, "63498000", first.Vagon)
	assert.Equal(t, "20", first.AdmCode)
	assert.Equal(t, "СОБ", first.OwnerCode)
	assert.Equal(t, "161128", first.CargoCode, "код груза ЕТСНГ — только в сыром документе")
	assert.Equal(t, "УГОЛЬ КАМЕННЫЙ МАРКИ Д", first.CargoName)
	assert.Equal(t, "вгр", first.GrOperationType)
	// Год в «19.07» не передан — восстановлен по дате документа (20.07.2026).
	assert.Equal(t, "2026-07-19T23:25:00", first.GetIn.String())
	assert.Nil(t, first.Report, "у подачи уведомления ещё нет")
	assert.Nil(t, first.GetOut, "у подачи уборки ещё нет")
	assert.Empty(t, first.Containers, "containers приходит null → пусто")

	assert.Equal(t, "64970130", doc.Vagons[18].Vagon, "порядок строк бланка сохранён")
}

// nmtp отличается от attis формой данных: памятка с одним вагоном приходит
// объектом вместо массива, реквизиты владельца пути беднее, зато есть блок
// контрагента.
func TestParseReferenceDoc_GoldenNmtp(t *testing.T) {
	raw, err := os.ReadFile(goldenNmtp)
	require.NoError(t, err)

	batch, err := ParseReferenceDoc(raw, "nmtp")
	require.NoError(t, err)
	require.Len(t, batch.Docs, 3)

	single, ok := batch.ByNumber("11010")
	require.True(t, ok)
	require.Len(t, single.Vagons, 1, "wagons.wagon объектом — это один вагон, а не ошибка")

	w := single.Vagons[0]
	assert.Equal(t, "65119620", w.Vagon)
	assert.Equal(t, "АРС", w.OwnerCode)
	assert.Equal(t, "Памятка подачи №10817", w.NumberMemo)
	assert.Equal(t, "2026-07-15T08:14:00", w.GetIn.String())
	assert.Equal(t, "2026-07-16T02:40:00", w.Report.String())
	assert.Equal(t, "2026-07-19T20:10:00", w.GetOut.String())
	assert.Equal(t, "уборку", single.OperType)
	assert.True(t, single.Contragent.IsZero())

	withContragent, ok := batch.ByNumber("11007")
	require.True(t, ok)
	assert.Equal(t, "31170931", withContragent.Contragent.OKPO)
	assert.NotEmpty(t, withContragent.Contragent.Name)
	assert.Len(t, withContragent.Vagons, 11)

	_, ok = batch.ByNumber("99999")
	assert.False(t, ok, "чужого номера в пачке нет")
}

// Основной путь: из пачки берём ровно запрошенный документ.
func TestParseReferenceDocByNumber(t *testing.T) {
	raw, err := os.ReadFile(goldenAttis)
	require.NoError(t, err)

	doc, err := ParseReferenceDocByNumber(raw, "attis", "11013")
	require.NoError(t, err)
	assert.Equal(t, "11013", doc.Number)
	assert.Equal(t, "подачу", doc.OperType)
	require.Len(t, doc.Vagons, 19)

	// Тот же документ, что даёт разбор всей пачки, — пути не расходятся.
	batch, err := ParseReferenceDoc(raw, "attis")
	require.NoError(t, err)
	fromBatch, ok := batch.ByNumber("11013")
	require.True(t, ok)
	assert.Equal(t, fromBatch, doc)

	_, err = ParseReferenceDocByNumber(raw, "attis", "99999")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "пришли номера [11013 11017]", "в ошибке видно, что реально приехало")
}

// Ради чего всё и затевалось: битый попутчик не должен отнимать у нас
// исправную памятку. Первый документ в пачке заведомо сломан (нет вагонов),
// запрашиваем второй.
func TestParseReferenceDocByNumber_BrokenNeighbourIgnored(t *testing.T) {
	raw := []byte(`{"code":0,"status":"success","data":{"count":2,"documents":[
	  {"_summary":{"DocNum":"11017","DocDate":"мусор"},
	   "_decoded_doc":{"data":{"number":"11017","wagons":{"wagon":[]}}}},
	  {"_summary":{"DocId":"7","DocNum":"11013","DocDate":"20.07.2026 01:22:37"},
	   "_decoded_doc":{"data":{"number":"11013","oper_type":"подачу","wagons":{"wagon":
	     {"wagon_number":"63498000","gr_oper_type":"вгр","get_in_date":"19.07","get_in_time":"23:25"}}}}}]}}`)

	doc, err := ParseReferenceDocByNumber(raw, "attis", "11013")
	require.NoError(t, err, "соседний битый документ разбору не мешает")
	assert.Equal(t, "11013", doc.Number)
	assert.Equal(t, "2026-07-19T23:25:00", doc.Vagons[0].GetIn.String())

	_, err = ParseReferenceDoc(raw, "attis")
	require.Error(t, err, "а разбор всей пачки на нём спотыкается — потому он и не основной путь")
}

// Пачка бывает и из одного документа (count=1) — тогда ответ по номеру отдаёт
// ровно запрошенную памятку. Здесь же: attis С блоком контрагента (name+okpo,
// без ИНН/КПП) и уборочная памятка, чьи вагоны ссылаются сразу на три разные
// памятки подачи.
func TestParseReferenceDoc_SingleDocumentBatch(t *testing.T) {
	raw, err := os.ReadFile("testdata/pamyatka_raw_single.json")
	require.NoError(t, err)

	batch, err := ParseReferenceDoc(raw, "attis")
	require.NoError(t, err)
	require.Len(t, batch.Docs, 1, "count=1 — в пачке ровно один документ")

	doc, ok := batch.ByNumber("10807")
	require.True(t, ok)
	assert.Equal(t, "уборку", doc.OperType)
	assert.Equal(t, "2026-07-14T21:51:02", doc.DocDate.String())
	require.Len(t, doc.Vagons, 33)

	assert.Equal(t, "АО  Находкинский Морской Торговый Порт", doc.Contragent.Name)
	assert.Equal(t, "01126022", doc.Contragent.OKPO)
	assert.Empty(t, doc.Contragent.INN, "у контрагента приходят не все реквизиты")

	// Реквизиты составления — из Metadatatab, их нет ни в _summary, ни в бланке.
	assert.Equal(t, "Бранд С. А. Приемосдатчик груза и багажа", doc.Creator)
	assert.Equal(t, "/Бранд Светлана Александровна-14.07.2026-21:50:14", doc.Signatories)
	assert.Equal(t, "96", doc.RailwayCode)
	assert.Equal(t, "2026-07-14T21:45:00", doc.ComposedAt.String(),
		"составлен в 21:45, зарегистрирован (DocDate) в 21:51:02 — это разные времена")
	assert.True(t, doc.ComposedAt.Time().Before(doc.DocDate.Time()))

	memos := map[string]bool{}
	for _, w := range doc.Vagons {
		memos[w.NumberMemo] = true
		require.NotNil(t, w.GetOut, "в уборочной памятке уборка проставлена у всех вагонов")
	}
	assert.Len(t, memos, 3, "вагоны одной уборки пришли с трёх разных подач")
}

// Год вагонных времён берётся от даты документа, поэтому декабрьская подача в
// январском документе (и наоборот) не уезжает на год вперёд/назад.
func TestParseReferenceDoc_YearAcrossNewYear(t *testing.T) {
	raw := []byte(`{"code":0,"status":"success","message":"","received_at":"","data":{"count":1,"new_oper_id":"1",
	  "documents":[{"_summary":{"DocId":"1","DocNum":"77","DocDate":"02.01.2027 03:10:00","DocState":"","DocStateId":""},
	  "_decoded_doc":{"data":{"number":"77","oper_type":"уборку","wagons":{"wagon":
	    {"wagon_number":"60000001","gr_oper_type":"вгр","get_in_date":"30.12","get_in_time":"22:00",
	     "get_out_date":"02.01","get_out_time":"01:00"}}}}}]}}`)

	batch, err := ParseReferenceDoc(raw, "attis")
	require.NoError(t, err)
	require.Len(t, batch.Docs, 1)

	w := batch.Docs[0].Vagons[0]
	assert.Equal(t, "2026-12-30T22:00:00", w.GetIn.String(), "декабрь до документа — прошлый год")
	assert.Equal(t, "2027-01-02T01:00:00", w.GetOut.String(), "январь — год документа")
}

func TestParseReferenceDoc_Errors(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "тело отказа провайдера (404)",
			raw:  `{"error":"pamyatka not found"}`,
			want: "pamyatka not found",
		},
		{
			name: "не JSON",
			raw:  `не json`,
			want: "парсинг JSON",
		},
		{
			name: "код ошибки в конверте",
			raw:  `{"code":1,"status":"error","message":"внутренняя ошибка","data":{}}`,
			want: "внутренняя ошибка",
		},
		{
			name: "count не сходится с числом документов",
			raw:  `{"code":0,"status":"success","data":{"count":5,"documents":[]}}`,
			want: "count=5",
		},
		{
			name: "номер бланка не совпадает с DocNum",
			raw:  `{"code":0,"status":"success","data":{"count":1,"documents":[{"_summary":{"DocNum":"11013","DocDate":"20.07.2026 01:22:37"},"_decoded_doc":{"data":{"number":"11017","wagons":{"wagon":[{"wagon_number":"1"}]}}}}]}}`,
			want: "не совпадает с DocNum",
		},
		{
			name: "бланк без вагонов",
			raw:  `{"code":0,"status":"success","data":{"count":1,"documents":[{"_summary":{"DocNum":"11013","DocDate":"20.07.2026 01:22:37"},"_decoded_doc":{"data":{"number":"11013","wagons":{"wagon":[]}}}}]}}`,
			want: "нет вагонов",
		},
		{
			name: "битая дата документа",
			raw:  `{"code":0,"status":"success","data":{"count":1,"documents":[{"_summary":{"DocNum":"11013","DocDate":"мусор"},"_decoded_doc":{"data":{"number":"11013","wagons":{"wagon":[{"wagon_number":"1"}]}}}}]}}`,
			want: "DocDate",
		},
		{
			name: "время вагона без даты",
			raw: `{"code":0,"status":"success","data":{"count":1,"documents":[{"_summary":{"DocNum":"11013","DocDate":"20.07.2026 01:22:37"},` +
				`"_decoded_doc":{"data":{"number":"11013","wagons":{"wagon":[{"wagon_number":"1","get_in_time":"23:25"}]}}}}]}}`,
			want: "парой",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseReferenceDoc([]byte(tc.raw), "attis")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}
