package parser

import (
	"encoding/json"
	"os"
	"testing"
)

// tableResponse — форма ответа кабинета на опрос готовности отчёта (сокращённая
// до того, что читает переходник).
type tableResponse struct {
	Data struct {
		CreatedAt string `json:"created_at"`
		Data      struct {
			Head []string            `json:"head"`
			Body [][]json.RawMessage `json:"body"`
		} `json:"data"`
	} `json:"data"`
}

func loadTableGolden(t *testing.T) tableResponse {
	t.Helper()
	raw, err := os.ReadFile("testdata/lk_report_rows.json")
	if err != nil {
		t.Fatalf("чтение образца: %v", err)
	}
	var resp tableResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("разбор образца: %v", err)
	}
	return resp
}

// TestParseRows_Golden — разбор боевого ответа личного кабинета (4 строки, снятые
// с живого забора 03.08.2026). Проверяем именно те поля, на которых стоит
// конвейер: коды станций и операции, индекс поезда, вес (кг → тонны), даты.
func TestParseRows_Golden(t *testing.T) {
	resp := loadTableGolden(t)
	recs, err := NewJSONParser().ParseRows(resp.Data.Data.Head, resp.Data.Data.Body)
	if err != nil {
		t.Fatalf("ParseRows: %v", err)
	}
	if len(recs) != 4 {
		t.Fatalf("записей: получили %d, ждали 4", len(recs))
	}

	r := recs[0]
	if r.Vagon != "63499578" {
		t.Errorf("вагон: %q", r.Vagon)
	}
	if r.Invoice != "ЭЦ228978" {
		t.Errorf("накладная: %q", r.Invoice)
	}
	// 15 цифр 872504178985702 → 4-3-4 (станция начала ≠ станции операции не мешает:
	// здесь они равны, но первые 6 цифр совпадают со станцией — индекс валиден).
	if r.Index != "8725-178-9857" {
		t.Errorf("индекс: %q, ждали 8725-178-9857", r.Index)
	}
	if r.CodeStationNach != "872504" || r.CodeStationOper != "872504" || r.CodeStanNazn != "985702" {
		t.Errorf("станции: нач=%q оп=%q назн=%q", r.CodeStationNach, r.CodeStationOper, r.CodeStanNazn)
	}
	if r.CodeOper != "40" {
		t.Errorf("код операции: %q", r.CodeOper)
	}
	if r.GruzpolOkpo != "10230304" || r.GruzotprOkpo != "72448060" {
		t.Errorf("ОКПО: получатель=%q отправитель=%q", r.GruzpolOkpo, r.GruzotprOkpo)
	}
	if r.CodeCargo != "161128" {
		t.Errorf("код груза: %q", r.CodeCargo)
	}
	// Источник даёт килограммы, домен хранит тонны.
	if r.Ves == nil || *r.Ves != 74.9 {
		t.Errorf("вес: %v, ждали 74.9", r.Ves)
	}
	if r.PorozhPriznak != "1" {
		t.Errorf("признак порожнего: %q", r.PorozhPriznak)
	}
	if r.TimeOp == nil || r.TimeOp.String() != "2026-07-13T09:52:00" {
		t.Errorf("время операции: %v", r.TimeOp)
	}
	// Правило «час ≥ 18 → +1 сутки» (бизнес-правило ЖД-суток, не таймзона):
	// 12.07 17:01 → 12.07, час < 18.
	if r.DateNach == nil || r.DateNach.String() != "2026-07-12T00:00:00" {
		t.Errorf("дата начала рейса: %v", r.DateNach)
	}
	if r.DateDostav == nil || r.DateDostav.String() != "2026-07-26T00:00:00" {
		t.Errorf("срок доставки: %v", r.DateDostav)
	}
	if r.Uno != "001764122273" {
		t.Errorf("идентификатор накладной: %q", r.Uno)
	}
	// Расстояния приходят с ведущими нулями («0000006094») — должны стать числом.
	if r.RasstStanNazn == nil || *r.RasstStanNazn != 6094 {
		t.Errorf("расстояние оставшееся: %v, ждали 6094", r.RasstStanNazn)
	}
	if r.ProstDn == nil || *r.ProstDn != 20 {
		t.Errorf("простой (сутки): %v", r.ProstDn)
	}
	if r.ProstCh == nil || *r.ProstCh != 16 {
		t.Errorf("простой (часы): %v", r.ProstCh)
	}

	// Прибытие есть только у третьей строки — пустое поле приходит null и не должно
	// давать нулевую дату.
	if recs[0].DatePrib != nil {
		t.Errorf("строка без прибытия: %v", recs[0].DatePrib)
	}
	if recs[2].DatePrib == nil || recs[2].DatePrib.String() != "2026-08-01T06:30:00" {
		t.Errorf("прибытие третьей строки: %v", recs[2].DatePrib)
	}

	// Гружёный вагон (PPV_POR=0) и его собственные коды.
	if recs[3].PorozhPriznak != "0" {
		t.Errorf("четвёртая строка должна быть гружёной: %q", recs[3].PorozhPriznak)
	}

	// ID детерминированный: те же вагон + станция + дата дают тот же ключ.
	again, _ := NewJSONParser().ParseRows(resp.Data.Data.Head, resp.Data.Data.Body)
	if again[0].ID != r.ID {
		t.Errorf("ID не детерминирован: %q != %q", again[0].ID, r.ID)
	}
}

// TestParseRows_Edge — вырожденные входы: пустой заголовок, заголовок без единого
// известного поля, строка короче заголовка, строка без номера вагона.
func TestParseRows_Edge(t *testing.T) {
	p := NewJSONParser()

	if _, err := p.ParseRows(nil, nil); err == nil {
		t.Error("пустой заголовок должен быть ошибкой")
	}
	if _, err := p.ParseRows([]string{"COORDS", "TARIF"}, [][]json.RawMessage{{[]byte("1"), []byte("2")}}); err == nil {
		t.Error("заголовок без известных полей должен быть ошибкой")
	}

	head := []string{"NOM_VAG", "STAN_OP", "DATE_OP"}
	body := [][]json.RawMessage{
		{[]byte(`"12345678"`)},               // строка короче заголовка — остальное пусто
		{[]byte(`""`), []byte(`"872504"`)},   // пустой номер вагона — запись отбрасывается
		{[]byte(`null`), []byte(`"872504"`)}, // null в номере — то же самое
	}
	recs, err := p.ParseRows(head, body)
	if err != nil {
		t.Fatalf("ParseRows: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("записей: %d, ждали 1 (годна только первая)", len(recs))
	}
	if recs[0].Vagon != "12345678" {
		t.Errorf("вагон: %q", recs[0].Vagon)
	}
}
