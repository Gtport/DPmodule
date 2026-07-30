package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

// fakeRefClient — заглушка port.ReferenceClient: помнит опрошенных клиентов и
// курсоры, с которыми пришли запросы; возвращает ошибку для перечисленных в
// failFor и тело из bodies (по клиенту), иначе — пустую пачку.
type fakeRefClient struct {
	called     []string
	cursors    []string
	byNumberCl string // клиент, с которым пришёл последний ByNumber
	byNumberDC string // date_create последнего ByNumber
	failFor    map[string]bool
	bodies     map[string]string
}

func (f *fakeRefClient) ByNumber(_ context.Context, client, _, dateCreate string) ([]byte, error) {
	f.byNumberCl = client
	f.byNumberDC = dateCreate
	return []byte(`{"code":0,"data":{"count":1}}`), nil
}

func (f *fakeRefClient) Update(_ context.Context, client, lastUpdate string) ([]byte, error) {
	f.called = append(f.called, client)
	f.cursors = append(f.cursors, lastUpdate)
	if f.failFor[client] {
		return nil, errors.New("404")
	}
	if b, ok := f.bodies[client]; ok {
		return []byte(b), nil
	}
	return []byte(`{"LAST_UPDATE":"","PAMYATKI":[]}`), nil
}

// refHistStub — история для тестов памяток: отдаёт заданные рейсы и копит
// применённые правки.
type refHistStub struct {
	trips   []domain.PamyatkaTrip
	asked   []string
	updates map[string]map[string]any
}

func (s *refHistStub) TripsForPamyatki(_ context.Context, vagons []string) ([]domain.PamyatkaTrip, error) {
	s.asked = vagons
	return s.trips, nil
}

func (s *refHistStub) UpdateFieldsBatch(_ context.Context, updates map[string]map[string]any) error {
	if s.updates == nil {
		s.updates = map[string]map[string]any{}
	}
	for id, f := range updates {
		s.updates[id] = f
	}
	return nil
}

func (s *refHistStub) ExistingIDs(context.Context, []string) (map[string]struct{}, error) {
	return nil, nil
}
func (s *refHistStub) Insert(context.Context, []domain.VagonHistory) error        { return nil }
func (s *refHistStub) UpdateFields(context.Context, string, map[string]any) error { return nil }
func (s *refHistStub) RowsByIDs(context.Context, []string) ([]domain.VagonHistory, error) {
	return nil, nil
}
func (s *refHistStub) ArrivedRows(context.Context, domain.LocalTime, domain.LocalTime, []string) ([]domain.VagonHistory, error) {
	return nil, nil
}
func (s *refHistStub) DailyTerminalCounts(context.Context, domain.LocalTime, domain.LocalTime) (map[string]int, map[string]int, error) {
	return nil, nil, nil
}
func (s *refHistStub) DailyCargoUnloaded(context.Context, domain.LocalTime, domain.LocalTime) (map[string]int, error) {
	return nil, nil
}

func (s *refHistStub) PerestanovkaRows(context.Context, domain.LocalTime, domain.LocalTime, bool) ([]domain.VagonHistory, error) {
	return nil, nil
}

func (s *refHistStub) LoadingDaily(context.Context, domain.LocalTime, domain.LocalTime) ([]domain.LoadingDailyRow, error) {
	return nil, nil
}

// refCursorStub — курсор в памяти.
type refCursorStub struct {
	saved map[string]string
	fail  bool
}

func (c *refCursorStub) Get(_ context.Context, client string) (string, error) {
	return c.saved[client], nil
}

func (c *refCursorStub) Set(_ context.Context, client, lastUpdate string) error {
	if c.fail {
		return errors.New("нет связи с БД")
	}
	if c.saved == nil {
		c.saved = map[string]string{}
	}
	c.saved[client] = lastUpdate
	return nil
}

// refJournalStub — port.JournalRepository, копящий добавленные события.
type refJournalStub struct{ events []domain.JournalEvent }

func (s *refJournalStub) Append(_ context.Context, ev domain.JournalEvent) error {
	s.events = append(s.events, ev)
	return nil
}
func (s *refJournalStub) LatestByType(context.Context, string) (domain.JournalEvent, bool, error) {
	return domain.JournalEvent{}, false, nil
}
func (s *refJournalStub) LatestBySource(context.Context, string) (domain.JournalEvent, bool, error) {
	return domain.JournalEvent{}, false, nil
}
func (s *refJournalStub) Recent(context.Context, int) ([]domain.JournalEvent, error) {
	return nil, nil
}
func (s *refJournalStub) Range(context.Context, *domain.LocalTime, *domain.LocalTime, []string, int) ([]domain.JournalEvent, error) {
	return nil, nil
}

// refTestTiming — боевые дефолты таймингов (config подставляет те же).
func refTestTiming() ReferenceTiming {
	return ReferenceTiming{
		Interval:        time.Hour,
		InitialLookback: 48 * time.Hour,
		CursorOverlap:   30 * time.Minute,
		StaleAfter:      6 * time.Hour,
	}
}

func newRefSvc(cl *fakeRefClient, hist *refHistStub, cur *refCursorStub, clients []string) *ReferenceService {
	if hist == nil {
		hist = &refHistStub{}
	}
	if cur == nil {
		cur = &refCursorStub{}
	}
	return NewReferenceService(cl, hist, cur, nil, clients, refTestTiming(), zap.NewNop())
}

// Ошибка одного клиента не прерывает опрос остальных, но даёт сводную ошибку.
func TestPullUpdates_ResilientAcrossClients(t *testing.T) {
	fc := &fakeRefClient{failFor: map[string]bool{"nmtp": true}}
	svc := newRefSvc(fc, nil, nil, []string{"attis", "nmtp"})

	if err := svc.PullUpdates(context.Background()); err == nil {
		t.Fatal("ждали сводную ошибку из-за упавшего nmtp")
	}
	if len(fc.called) != 2 || fc.called[0] != "attis" || fc.called[1] != "nmtp" {
		t.Fatalf("оба клиента должны быть опрошены, получили %v", fc.called)
	}
}

// Все клиенты успешны → ошибки нет.
func TestPullUpdates_AllOK(t *testing.T) {
	fc := &fakeRefClient{}
	if err := newRefSvc(fc, nil, nil, []string{"attis"}).PullUpdates(context.Background()); err != nil {
		t.Fatalf("ошибок быть не должно: %v", err)
	}
}

// Курсор берётся из БД и после ответа сдвигается на LAST_UPDATE провайдера
// МИНУС нахлёст (cursor_overlap): провайдер публикует записи не в порядке
// штампов, встык запоздавшие памятки терялись бы навсегда.
func TestPullUpdates_CursorRoundTrip(t *testing.T) {
	fc := &fakeRefClient{bodies: map[string]string{
		"attis": `{"LAST_UPDATE":"2026-07-27 22:22:51.869","PAMYATKI":[]}`,
	}}
	cur := &refCursorStub{saved: map[string]string{"attis": "2026-07-27 10:00:00.000"}}
	svc := newRefSvc(fc, nil, cur, []string{"attis"})

	if err := svc.PullUpdates(context.Background()); err != nil {
		t.Fatalf("PullUpdates: %v", err)
	}
	if len(fc.cursors) != 1 || fc.cursors[0] != "2026-07-27 10:00:00.000" {
		t.Fatalf("запрос должен уйти с сохранённым курсором, ушёл с %v", fc.cursors)
	}
	if cur.saved["attis"] != "2026-07-27 21:52:51.869" {
		t.Fatalf("курсор должен встать на LAST_UPDATE минус 30m нахлёста, стало %q", cur.saved["attis"])
	}
}

// Холодный старт (курсора в БД нет) берёт окно initial_lookback, а НЕ
// pull_interval: часовое окно у этого провайдера пусто всегда (он публикует
// памятки позже их LAST_UPDATE), пустой ответ не даёт курсора — и инкремент
// зацикливался на первом заходе (бой 27–30.07.2026, 80 тиков по нулям).
func TestPullUpdates_ColdStartUsesLookback(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC))
	defer restore()

	fc := &fakeRefClient{}
	if err := newRefSvc(fc, nil, nil, []string{"attis"}).PullUpdates(context.Background()); err != nil {
		t.Fatalf("PullUpdates: %v", err)
	}
	if len(fc.cursors) != 1 || fc.cursors[0] != "2026-07-28 12:00:00.000" {
		t.Fatalf("первый заход должен уйти с окном 48h назад, ушёл с %v", fc.cursors)
	}
}

// Курсор двигается только вперёд: пачка нахлёста приходит с тем же (или более
// старым) LAST_UPDATE, откатывать позицию назад нельзя.
func TestPullUpdates_CursorNeverMovesBack(t *testing.T) {
	fc := &fakeRefClient{bodies: map[string]string{
		"attis": `{"LAST_UPDATE":"2026-07-27 22:22:51.869","PAMYATKI":[]}`,
	}}
	cur := &refCursorStub{saved: map[string]string{"attis": "2026-07-27 23:00:00.000"}}

	if err := newRefSvc(fc, nil, cur, []string{"attis"}).PullUpdates(context.Background()); err != nil {
		t.Fatalf("PullUpdates: %v", err)
	}
	if cur.saved["attis"] != "2026-07-27 23:00:00.000" {
		t.Fatalf("курсор уехал назад: %q", cur.saved["attis"])
	}
}

// Молчание длиннее stale_after попадает в журнал событием с silent_hours —
// иначе трое суток пустых тиков выглядят в интерфейсе как тишина, а не как
// поломка (ровно это и случилось 27–30.07.2026). Свежее молчание не пишем:
// памятки приходят далеко не каждый час, журнал забился бы тиками крона.
func TestPullUpdates_JournalsLongSilence(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC))
	defer restore()

	cases := []struct {
		name   string
		cursor string
		want   bool
		hours  int
	}{
		{"свежее молчание — не пишем", "2026-07-30 11:00:00.000", false, 0},
		{"молчание сутки — тревога", "2026-07-29 12:00:00.000", true, 24},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			jr := &refJournalStub{}
			cur := &refCursorStub{saved: map[string]string{"attis": c.cursor}}
			svc := NewReferenceService(&fakeRefClient{}, &refHistStub{}, cur, jr,
				[]string{"attis"}, refTestTiming(), zap.NewNop())

			if err := svc.PullUpdates(context.Background()); err != nil {
				t.Fatalf("PullUpdates: %v", err)
			}
			if got := len(jr.events) > 0; got != c.want {
				t.Fatalf("записей в журнале %d, ожидали want=%v", len(jr.events), c.want)
			}
			if !c.want {
				return
			}
			if jr.events[0].EventType != domain.EventPamyatkaPull {
				t.Fatalf("тип события: %q", jr.events[0].EventType)
			}
			var d struct {
				Pamyatki    int `json:"pamyatki"`
				SilentHours int `json:"silent_hours"`
			}
			if err := json.Unmarshal(jr.events[0].Detail, &d); err != nil {
				t.Fatalf("detail: %v", err)
			}
			if d.Pamyatki != 0 || d.SilentHours != c.hours {
				t.Fatalf("detail: pamyatki=%d silent_hours=%d (ждали 0 и %d)", d.Pamyatki, d.SilentHours, c.hours)
			}
		})
	}
}

// Пустая пачка приходит с ПУСТЫМ LAST_UPDATE — записать его в курсор нельзя,
// иначе следующий запрос уйдёт в никуда и позиция потеряется.
func TestPullUpdates_EmptyCursorNotSaved(t *testing.T) {
	fc := &fakeRefClient{bodies: map[string]string{
		"attis": `{"LAST_UPDATE":"","PAMYATKI":[]}`,
	}}
	cur := &refCursorStub{saved: map[string]string{"attis": "2026-07-27 10:00:00.000"}}

	if err := newRefSvc(fc, nil, cur, []string{"attis"}).PullUpdates(context.Background()); err != nil {
		t.Fatalf("PullUpdates: %v", err)
	}
	if cur.saved["attis"] != "2026-07-27 10:00:00.000" {
		t.Fatalf("прежний курсор должен уцелеть, стало %q", cur.saved["attis"])
	}
}

// Полный проход: памятка разбирается и ложится вехами в рейс истории.
func TestPullUpdates_AppliesToHistory(t *testing.T) {
	body := `{"LAST_UPDATE":"2026-07-27 22:22:51.869","PAMYATKI":[
	  {"NUMBER_PAMYATKA":"11255","DATE_CREATE":"07-25-2026","OPERATION_TYPE":"подачу",
	   "GET_PLACE":"Аттис -1 путь","NAME_STATION":"Мыс Астафьева","PATH_OWNER_OKPO":"10230304",
	   "VAGONS":[{"NUMBER_VAGON":"62428651","GR_OPERATION_TYPE":"вгр",
	              "GET_IN_DATE":"25.07","GET_IN_TIME":"00:10"}]}]}`
	prib := lt2("2026-07-24 08:00")
	hist := &refHistStub{trips: []domain.PamyatkaTrip{
		{ID: "t1", Vagon: "62428651", DatePrib: prib},
	}}
	fc := &fakeRefClient{bodies: map[string]string{"attis": body}}
	svc := newRefSvc(fc, hist, nil, []string{"attis"})

	res, err := svc.PullUpdatesDetailed(context.Background())
	if err != nil {
		t.Fatalf("PullUpdatesDetailed: %v", err)
	}
	if len(res) != 1 || res[0].Pamyatki != 1 || res[0].Vagons != 1 || res[0].Applied != 1 || res[0].Skipped != 0 {
		t.Fatalf("неожиданный итог прохода: %+v", res)
	}
	if len(hist.asked) != 1 || hist.asked[0] != "62428651" {
		t.Fatalf("рейсы должны запрашиваться по вагонам пачки, спросили %v", hist.asked)
	}
	f := hist.updates["t1"]
	if f == nil {
		t.Fatal("правка рейса t1 не записана")
	}
	if f["nom_gu45_pod"] != "11255" || f["place_pod"] != "Аттис -1 путь" {
		t.Fatalf("вехи подачи разложены неверно: %v", f)
	}
	if f["pamyatka_state"] != domain.PamyatkaStatePod {
		t.Fatalf("стадия должна стать 1, получили %v", f["pamyatka_state"])
	}
}

// Вагон чужого клиента (рейса в истории нет) не роняет проход, а попадает в
// счётчик пропусков с причиной.
func TestPullUpdates_UnknownVagonCounted(t *testing.T) {
	body := `{"LAST_UPDATE":"2026-07-27 22:22:51.869","PAMYATKI":[
	  {"NUMBER_PAMYATKA":"11255","DATE_CREATE":"07-25-2026","OPERATION_TYPE":"подачу",
	   "GET_PLACE":"путь","NAME_STATION":"ст","PATH_OWNER_OKPO":"1",
	   "VAGONS":[{"NUMBER_VAGON":"99999999","GR_OPERATION_TYPE":"вгр",
	              "GET_IN_DATE":"25.07","GET_IN_TIME":"00:10"}]}]}`
	fc := &fakeRefClient{bodies: map[string]string{"attis": body}}

	res, err := newRefSvc(fc, nil, nil, []string{"attis"}).PullUpdatesDetailed(context.Background())
	if err != nil {
		t.Fatalf("PullUpdatesDetailed: %v", err)
	}
	if res[0].Applied != 0 || res[0].Skipped != 1 {
		t.Fatalf("ждали 0 применённых и 1 пропуск, получили %+v", res[0])
	}
	if res[0].Reasons[domain.PamyatkaSkipNoTrip] != 1 {
		t.Fatalf("причина пропуска должна быть no_trip: %v", res[0].Reasons)
	}
}

// Битое тело провайдера не двигает курсор: пачку нельзя считать разобранной.
func TestPullUpdates_BadBodyKeepsCursor(t *testing.T) {
	fc := &fakeRefClient{bodies: map[string]string{"attis": `{"LAST_UPDATE":"x","PAMYATKI":[{"NUMBER_PAMYATKA":"1","DATE_CREATE":"кривая дата"}]}`}}
	cur := &refCursorStub{saved: map[string]string{"attis": "2026-07-27 10:00:00.000"}}

	if err := newRefSvc(fc, nil, cur, []string{"attis"}).PullUpdates(context.Background()); err == nil {
		t.Fatal("ждали ошибку разбора")
	}
	if cur.saved["attis"] != "2026-07-27 10:00:00.000" {
		t.Fatalf("курсор не должен двигаться при ошибке разбора, стало %q", cur.saved["attis"])
	}
}

func TestFetchByNumber(t *testing.T) {
	fc := &fakeRefClient{}
	body, err := newRefSvc(fc, nil, nil, []string{"attis"}).
		FetchByNumber(context.Background(), "nmtp", "10272", "07-04-2026")
	if err != nil {
		t.Fatalf("FetchByNumber: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("ждали непустое тело документа")
	}
	if fc.byNumberCl != "nmtp" {
		t.Fatalf("явный клиент должен дойти до клиента как есть, получили %q", fc.byNumberCl)
	}
	// Памятку определяет тройка клиент+номер+дата: дату отдаём провайдеру
	// дословно, без переформатирования (иначе вернётся чужой документ).
	if fc.byNumberDC != "07-04-2026" {
		t.Fatalf("date_create должен дойти дословно, получили %q", fc.byNumberDC)
	}
}

// Пустой client → первый из настроенных reference.clients.
func TestFetchByNumber_DefaultClient(t *testing.T) {
	fc := &fakeRefClient{}
	if _, err := newRefSvc(fc, nil, nil, []string{"attis", "nmtp"}).
		FetchByNumber(context.Background(), "", "10272", "07-04-2026"); err != nil {
		t.Fatalf("FetchByNumber: %v", err)
	}
	if fc.byNumberCl != "attis" {
		t.Fatalf("ждали дефолтного attis, получили %q", fc.byNumberCl)
	}
}

// Клиент не задан и список пуст → внятная ошибка, а не запрос в никуда.
func TestFetchByNumber_NoClientConfigured(t *testing.T) {
	svc := newRefSvc(&fakeRefClient{}, nil, nil, nil)
	if _, err := svc.FetchByNumber(context.Background(), "", "10272", "07-04-2026"); err == nil {
		t.Fatal("ждали ошибку: клиент не задан и reference.clients пуст")
	}
}

func lt2(s string) *domain.LocalTime {
	t, err := time.Parse("2006-01-02 15:04", s)
	if err != nil {
		panic(err)
	}
	return domain.NewLocalTime(t)
}
