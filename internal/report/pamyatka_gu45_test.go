package report_test

import (
	"bytes"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/parser"
	"github.com/Gtport/DPmodule/internal/report"
)

func lt(t *testing.T, s string) *domain.LocalTime {
	t.Helper()
	v, err := time.Parse("2006-01-02T15:04:05", s)
	if err != nil {
		t.Fatalf("время %q: %v", s, err)
	}
	return domain.NewLocalTime(v)
}

// Памятка на уборку — образец бланка 11357 (МЫС АСТАФЬЕВА): у вагона заполнены
// все три времени, есть ссылка на встречную памятку подачи.
func uborkaDoc(t *testing.T) domain.PamyatkaDoc {
	t.Helper()
	return domain.PamyatkaDoc{
		Client:      "attis",
		Number:      "11357",
		DocDate:     lt(t, "2026-07-28T02:20:11"),
		DocState:    "Подписан Клиентом",
		Creator:     "Гаген О. В. Приемосдатчик груза и багажа",
		Signatories: "/Гаген Оксана Валерьевна-28.07.2026-02:28:04",
		ComposedAt:  lt(t, "2026-07-28T02:18:00"),
		OperType:    "уборку",
		GetPlace:    "3 путь - 71, 72, 73 причал (уголь)",
		GetBy:       `ОАО "РЖД"`,
		RailwayName: "Дальневосточная",
		StationCode: "985609",
		StationName: "МЫС АСТАФЬЕВА",
		PathOwner:   domain.PamyatkaParty{Name: "АО Находкинский Морской Торговый Порт", OKPO: "01126022"},
		Vagons: []domain.PamyatkaDocVagon{{
			Order:           "1",
			Vagon:           "64413214",
			AdmCode:         "20",
			OwnerCode:       "СОБ",
			CargoCode:       "161005",
			CargoName:       "ШИХТА УГОЛЬНАЯ",
			GrOperationType: "вгр",
			NumberMemo:      "Памятка подачи №11319",
			GetIn:           lt(t, "2026-07-26T15:45:00"),
			Report:          lt(t, "2026-07-27T15:53:00"),
			GetOut:          lt(t, "2026-07-27T18:30:00"),
		}},
	}
}

func openBook(t *testing.T, raw []byte) (*excelize.File, string) {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("открытие книги: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f, f.GetSheetName(0)
}

func cell(t *testing.T, f *excelize.File, sheet, addr string) string {
	t.Helper()
	v, err := f.GetCellValue(sheet, addr)
	if err != nil {
		t.Fatalf("ячейка %s: %v", addr, err)
	}
	return v
}

// Шапка бланка: станция с кодом, заголовок с номером и видом операции,
// владелец пути, место подачи, локомотив.
func TestPamyatkaGU45XLSX_Header(t *testing.T) {
	doc := uborkaDoc(t)
	raw, err := report.PamyatkaGU45XLSX(doc)
	if err != nil {
		t.Fatalf("сборка: %v", err)
	}
	f, sheet := openBook(t, raw)

	if sheet != "ГУ-45 №11357" {
		t.Errorf("имя листа: ждали «ГУ-45 №11357», получили %q", sheet)
	}
	checks := map[string]string{
		"A3":  "Станция",
		"B3":  "МЫС АСТАФЬЕВА 985609",
		"A4":  "Дальневосточная",
		"A6":  "ПАМЯТКА ПРИЕМОСДАТЧИКА № 11357 на уборку вагонов",
		"C8":  "АО Находкинский Морской Торговый Порт",
		"I8":  "3 путь - 71, 72, 73 причал (уголь)",
		"C10": `ОАО "РЖД"`,
		"I10": "", // индекс поезда провайдер не отдаёт — линия пустая
		"L1":  "0363809",
	}
	for addr, want := range checks {
		if got := cell(t, f, sheet, addr); got != want {
			t.Errorf("%s: ждали %q, получили %q", addr, want, got)
		}
	}
}

// Строка вагона: 12 граф в порядке бланка, времена «дд.мм» + «чч:мм» двумя
// строками, незаполняемые графы (задержка, ГУ-23, взвешивания) пустые.
func TestPamyatkaGU45XLSX_VagonRow(t *testing.T) {
	doc := uborkaDoc(t)
	raw, err := report.PamyatkaGU45XLSX(doc)
	if err != nil {
		t.Fatalf("сборка: %v", err)
	}
	f, sheet := openBook(t, raw)

	want := map[string]string{
		"A16": "1",
		"B16": "64413214\nШИХТА УГОЛЬНАЯ",
		"C16": "20",
		"D16": "СОБ",
		"E16": "вгр",
		"F16": "26.07\n15:45",
		"G16": "27.07\n15:53",
		"H16": "27.07\n18:30",
		"I16": "",
		"J16": "",
		"K16": "",
		"L16": "Памятка подачи №11319",
	}
	for addr, w := range want {
		if got := cell(t, f, sheet, addr); got != w {
			t.Errorf("%s: ждали %q, получили %q", addr, w, got)
		}
	}
	// Строка нумерации граф — она в бланке над данными.
	if got := cell(t, f, sheet, "A15"); got != "1" {
		t.Errorf("нумерация граф A15: ждали «1», получили %q", got)
	}
	if got := cell(t, f, sheet, "L15"); got != "12" {
		t.Errorf("нумерация граф L15: ждали «12», получили %q", got)
	}
}

// У памятки на подачу уборки ещё нет: графы 7 и 8 пустые, а подпись
// приемосдатчика стоит со стороны «Сдал» (на уборке — со стороны «Принял»).
func TestPamyatkaGU45XLSX_PodachaBezUborki(t *testing.T) {
	doc := uborkaDoc(t)
	doc.Number = "12659"
	doc.OperType = "подачу"
	doc.Vagons[0].Report = nil
	doc.Vagons[0].GetOut = nil
	doc.Vagons[0].NumberMemo = ""

	raw, err := report.PamyatkaGU45XLSX(doc)
	if err != nil {
		t.Fatalf("сборка: %v", err)
	}
	f, sheet := openBook(t, raw)

	if got := cell(t, f, sheet, "A6"); got != "ПАМЯТКА ПРИЕМОСДАТЧИКА № 12659 на подачу вагонов" {
		t.Errorf("заголовок: %q", got)
	}
	for _, addr := range []string{"G16", "H16", "L16"} {
		if got := cell(t, f, sheet, addr); got != "" {
			t.Errorf("%s: ждали пусто, получили %q", addr, got)
		}
	}

	sdal, prinyal := findSignature(t, f, sheet)
	if !strings.Contains(sdal, "Подписано ЭП Гаген Оксана Валерьевна") {
		t.Errorf("подача: подпись ждали у «Сдал», получили sdal=%q prinyal=%q", sdal, prinyal)
	}
	if prinyal != "" {
		t.Errorf("подача: у «Принял» ждали пусто, получили %q", prinyal)
	}
}

func TestPamyatkaGU45XLSX_UborkaPodpisPrinyal(t *testing.T) {
	raw, err := report.PamyatkaGU45XLSX(uborkaDoc(t))
	if err != nil {
		t.Fatalf("сборка: %v", err)
	}
	f, sheet := openBook(t, raw)

	sdal, prinyal := findSignature(t, f, sheet)
	if !strings.Contains(prinyal, "Подписано ЭП Гаген Оксана Валерьевна") {
		t.Errorf("уборка: подпись ждали у «Принял», получили sdal=%q prinyal=%q", sdal, prinyal)
	}
	if sdal != "" {
		t.Errorf("уборка: у «Сдал» ждали пусто, получили %q", sdal)
	}
}

// findSignature ищет в подвале строку «Сдал/Принял приемосдатчик ж.д.» и
// отдаёт значения обеих линий подписи. Номер строки не фиксируем: подвал
// съезжает вниз вместе с числом вагонов.
func findSignature(t *testing.T, f *excelize.File, sheet string) (sdal, prinyal string) {
	t.Helper()
	rows, err := f.GetRows(sheet)
	if err != nil {
		t.Fatalf("чтение строк: %v", err)
	}
	for _, row := range rows {
		if len(row) > 0 && strings.HasPrefix(row[0], "Сдал приемосдатчик") {
			if len(row) > 2 {
				sdal = row[2]
			}
			if len(row) > 8 {
				prinyal = row[8]
			}
			return sdal, prinyal
		}
	}
	t.Fatal("в подвале нет строки «Сдал приемосдатчик ж.д.»")
	return "", ""
}

// Сквозная проверка на боевом ответе: разбор сырой памятки → бланк. Все вагоны
// документа должны попасть в лист, номера — совпасть построчно.
func TestPamyatkaGU45XLSX_FromLiveResponse(t *testing.T) {
	raw, err := os.ReadFile("../parser/testdata/pamyatka_raw_single.json")
	if err != nil {
		t.Fatalf("чтение фикстуры: %v", err)
	}
	doc, err := parser.ParseReferenceDocByNumber(raw, "attis", "10807")
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}

	book, err := report.PamyatkaGU45XLSX(doc)
	if err != nil {
		t.Fatalf("сборка: %v", err)
	}
	f, sheet := openBook(t, book)

	if got := cell(t, f, sheet, "A6"); got != "ПАМЯТКА ПРИЕМОСДАТЧИКА № 10807 на уборку вагонов" {
		t.Errorf("заголовок: %q", got)
	}
	for i, v := range doc.Vagons {
		addr := "B" + strconv.Itoa(16+i)
		got := cell(t, f, sheet, addr)
		if !strings.HasPrefix(got, v.Vagon) {
			t.Fatalf("%s: ждали вагон %s, получили %q", addr, v.Vagon, got)
		}
	}
	// За последним вагоном таблица кончается — подвал не должен наезжать на неё.
	after := cell(t, f, sheet, "A"+strconv.Itoa(16+len(doc.Vagons)))
	if after != "" {
		t.Errorf("строка за таблицей не пуста: %q", after)
	}
}
