package service

// «История движения вагона» для интерфейса: сохранённый трейл рейса
// (vagon_operation, запрос 601) + матч справочников (станция → stations,
// операция → cargo_operations) и нормализация индекса поезда в XXXX-XXX-XXXX.
//
// Свёртка (решение владельца): визит = НЕПРЕРЫВНАЯ серия операций на одной
// станции; показываем первую и последнюю операцию визита, остальные — под
// разворотом. Возврат вагона на ту же станцию позже даёт отдельный визит,
// поэтому хронология не ломается.
//
// Рейс определяется строкой vagon_history (id): оттуда вагон и дата погрузки —
// всё, что нужно для запроса 601 (from = date_nach_d−1, to = сегодня), а клиент
// провайдера — по терминалу gruzpol_s через реестр портов. Снимок дислокации не
// участвует: вагон мог уже выбыть, а история остаётся.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/parser"
	"github.com/Gtport/DPmodule/internal/port"
)

var (
	// ErrTripNotFound — строки истории с таким id нет (или у неё нет даты погрузки).
	ErrTripNotFound = errors.New("рейс не найден в истории")
	// ErrProviderClient — по терминалу рейса не определить клиента провайдера АСУ.
	ErrProviderClient = errors.New("не определить клиента провайдера для запроса истории")
)

// TrailOp — одна операция продвижения в человеческом виде.
type TrailOp struct {
	DateOp domain.LocalTime
	KopVmd string // код операции (как пришёл)
	Oper   string // полное имя операции (cargo_operations.oper)
	OperS  string // краткое имя операции (cargo_operations.oper_s)
	Index  string // индекс поезда, нормализованный (XXXX-XXX-XXXX / «Б/И»)
}

// TrailVisit — непрерывная серия операций на одной станции.
type TrailVisit struct {
	StanOp  string // код станции (как пришёл, с ведущими нулями)
	Station string // имя станции (stations.name), пусто — станции нет в справочнике
	Road    string // дорога (stations.road)
	First   TrailOp
	Last    TrailOp
	Count   int
	Ops     []TrailOp   // все операции визита (для разворота и Excel)
	Delay   *TrailDelay // визит был задержкой (простой/бросание); nil — обычный
}

// TrailDelay — эпизод задержки рейса (vagon_delay) в человеческом виде.
type TrailDelay struct {
	Kind        int    // 4 — долгий простой, 5 — брошен
	StationCode string // код станции стоянки (из снимка)
	StationName string // имя станции стоянки (из снимка)
	DateFrom    domain.LocalTime
	DateTo      *domain.LocalTime // nil — стоит до сих пор
	Hours       float64           // длительность; для открытого — до «сейчас»
}

// TrailView — весь трейл рейса плюс период фактически полученной истории:
// оператор сначала смотрит, что уже есть в базе, и решает, обновлять ли из АСУ.
// Delays — эпизоды задержек рейса из vagon_delay (независимы от трейла 601:
// показываются и когда истории продвижения нет); сводка «ехал N, из них X
// стоял» — TripHours/DelayHours.
type TrailView struct {
	ID       string
	Vagon    string
	DateNach *domain.LocalTime // дата погрузки (начало рейса)
	Terminal string            // gruzpol_s — терминал, он же ключ клиента провайдера
	From     *domain.LocalTime // время первой операции
	To       *domain.LocalTime // время последней операции
	Count    int               // всего операций
	Visits   []TrailVisit

	Delays     []TrailDelay // эпизоды задержек рейса, по времени
	TripHours  float64      // длительность рейса: погрузка → прибытие (или «сейчас»)
	DelayHours float64      // суммарные задержки рейса, часы
}

// TrailByHistoryID — сохранённый трейл рейса из БД, без обращения к провайдеру.
// Пустой Count означает «истории нет» — вызывающий решает, идти ли в АСУ.
func (s *VagonOpService) TrailByHistoryID(ctx context.Context, id string) (TrailView, error) {
	row, err := s.tripRow(ctx, id)
	if err != nil {
		return TrailView{}, err
	}
	key, ok := domain.TripKeyOf(row.Vagon, row.DateNachD)
	if !ok {
		return TrailView{}, ErrTripNotFound
	}
	ops, err := s.repo.OperationsByTrip(ctx, key)
	if err != nil {
		return TrailView{}, err
	}
	v := buildTrailView(row, ops, s.dir)
	s.attachDelays(ctx, row, &v)
	return v, nil
}

// PullTrailByHistoryID — запрос 601 у провайдера «сейчас» (кнопка «Обновить из
// АСУ»): интервал date_nach−1 … сегодня, полная перезапись трейла рейса.
func (s *VagonOpService) PullTrailByHistoryID(ctx context.Context, id string) (TrailView, error) {
	row, err := s.tripRow(ctx, id)
	if err != nil {
		return TrailView{}, err
	}
	key, ok := domain.TripKeyOf(row.Vagon, row.DateNachD)
	if !ok {
		return TrailView{}, ErrTripNotFound
	}
	client := s.clientForTerminal(row.GruzpolS)
	if client == "" {
		return TrailView{}, fmt.Errorf("%w: терминал %q", ErrProviderClient, row.GruzpolS)
	}
	q := domain.VagonOpRequest{
		TripKey: key, Vagon: row.Vagon, DateNachD: *row.DateNachD,
		Client: client, Reason: VagonOpReasonManual, Priority: 10,
		CreatedAt: clock.Now(), UpdatedAt: clock.Now(),
	}
	if err := s.fetchStore(ctx, q); err != nil {
		return TrailView{}, err
	}
	ops, err := s.repo.OperationsByTrip(ctx, key)
	if err != nil {
		return TrailView{}, err
	}
	v := buildTrailView(row, ops, s.dir)
	s.attachDelays(ctx, row, &v)
	return v, nil
}

// attachDelays — best-effort наложение эпизодов задержек (vagon_delay) на трейл:
// задержки — вторичная информация, их отказ экран истории не валит.
func (s *VagonOpService) attachDelays(ctx context.Context, row domain.VagonHistory, v *TrailView) {
	if s.delays == nil || row.DateNachD == nil {
		return
	}
	eps, err := s.delays.ByTrip(ctx, row.Vagon, *row.DateNachD)
	if err != nil {
		s.log.Warn("vagon_delay по рейсу не прочитан", zap.String("vagon", row.Vagon), zap.Error(err))
		return
	}
	attachTrailDelays(v, eps, row.DatePrib, clock.Now())
}

// tripRow — строка рейса из vagon_history по id (id = вагон/станция/дата, см.
// parser.generateDeterministicID). Без даты погрузки рейс не адресуем.
func (s *VagonOpService) tripRow(ctx context.Context, id string) (domain.VagonHistory, error) {
	if s.hist == nil {
		return domain.VagonHistory{}, ErrTripNotFound
	}
	rows, err := s.hist.RowsByIDs(ctx, []string{strings.TrimSpace(id)})
	if err != nil {
		return domain.VagonHistory{}, err
	}
	if len(rows) == 0 || rows[0].Vagon == "" || rows[0].DateNachD == nil {
		return domain.VagonHistory{}, ErrTripNotFound
	}
	return rows[0], nil
}

// clientForTerminal — клиент провайдера по краткому имени терминала
// (vagon_history.gruzpol_s → ports.name_s → ports.provider_client).
func (s *VagonOpService) clientForTerminal(nameS string) string {
	if s.dir == nil || strings.TrimSpace(nameS) == "" {
		return ""
	}
	p, ok := s.dir.PortByNameS(nameS)
	if !ok {
		return ""
	}
	return p.ProviderClient
}

// buildTrailView — матч справочников + свёртка по визитам. Операции приходят из
// репозитория по возрастанию времени; порядок сохраняем как есть.
func buildTrailView(row domain.VagonHistory, ops []domain.VagonOperation, dir *DirectoryCache) TrailView {
	v := TrailView{
		ID: row.ID, Vagon: row.Vagon, DateNach: row.DateNachD,
		Terminal: row.GruzpolS, Count: len(ops),
	}
	if len(ops) == 0 {
		return v
	}
	from, to := ops[0].DateOp, ops[len(ops)-1].DateOp
	v.From, v.To = &from, &to

	for _, o := range ops {
		t := TrailOp{
			DateOp: o.DateOp,
			KopVmd: o.KopVmd,
			Index:  parser.FormatTrainIndex(o.IndexPoezd),
		}
		if kod, err := strconv.Atoi(strings.TrimSpace(o.KopVmd)); err == nil && dir != nil {
			if op, ok := dir.GetCargoOperation(kod); ok {
				t.Oper, t.OperS = op.Oper, op.OperS
			}
		}
		last := len(v.Visits) - 1
		if last >= 0 && v.Visits[last].StanOp == o.StanOp {
			v.Visits[last].Ops = append(v.Visits[last].Ops, t)
			v.Visits[last].Last = t
			v.Visits[last].Count++
			continue
		}
		visit := TrailVisit{StanOp: o.StanOp, First: t, Last: t, Count: 1, Ops: []TrailOp{t}}
		if kod, err := strconv.Atoi(strings.TrimSpace(o.StanOp)); err == nil && dir != nil {
			if st, ok := dir.GetStationByKod(kod); ok {
				visit.Station, visit.Road = st.Name, st.Road
			}
		}
		v.Visits = append(v.Visits, visit)
	}
	return v
}

// attachTrailDelays накладывает эпизоды задержек на трейл (чистая функция):
// сводка рейса (TripHours: погрузка → прибытие или now; DelayHours: сумма
// эпизодов, открытый считается до now) + маркировка визитов — по коду станции
// и пересечению интервалов времени. Эпизод без визита (истории 601 нет или она
// не покрывает стоянку) остаётся только в списке Delays.
func attachTrailDelays(v *TrailView, eps []domain.VagonDelay, datePrib *domain.LocalTime, now domain.LocalTime) {
	if v.DateNach != nil && !time.Time(*v.DateNach).IsZero() {
		end := time.Time(now)
		if datePrib != nil && !time.Time(*datePrib).IsZero() {
			end = time.Time(*datePrib)
		}
		if h := end.Sub(time.Time(*v.DateNach)).Hours(); h > 0 {
			v.TripHours = round1(h)
		}
	}

	var total float64
	for _, e := range eps {
		if e.DateFrom == nil || time.Time(*e.DateFrom).IsZero() {
			continue
		}
		d := TrailDelay{
			Kind: e.Kind, StationCode: e.StationCode, StationName: e.StationName,
			DateFrom: *e.DateFrom, DateTo: e.DateTo,
		}
		switch {
		case e.DateTo == nil: // стоит сейчас — длительность до «сейчас»
			if h := time.Time(now).Sub(time.Time(*e.DateFrom)).Hours(); h > 0 {
				d.Hours = round1(h)
			}
		case e.Hours != nil:
			d.Hours = *e.Hours
		default:
			if h := time.Time(*e.DateTo).Sub(time.Time(*e.DateFrom)).Hours(); h > 0 {
				d.Hours = round1(h)
			}
		}
		total += d.Hours
		v.Delays = append(v.Delays, d)
	}
	v.DelayHours = round1(total)

	// Маркировка визитов: код станции + пересечение интервалов (эпизод мог
	// начаться раньше первой операции визита в сохранённом окне истории).
	for i := range v.Delays {
		d := &v.Delays[i]
		to := time.Time(now)
		if d.DateTo != nil {
			to = time.Time(*d.DateTo)
		}
		for j := range v.Visits {
			vis := &v.Visits[j]
			if vis.Delay != nil || !sameStationCode(vis.StanOp, d.StationCode) {
				continue
			}
			if !time.Time(d.DateFrom).After(time.Time(vis.Last.DateOp)) &&
				!to.Before(time.Time(vis.First.DateOp)) {
				vis.Delay = d
				break
			}
		}
	}
}

// sameStationCode — сравнение кодов станций с поправкой на ведущие нули
// (601 отдаёт код с ведущими нулями, снимок — как в справочнике).
func sameStationCode(a, b string) bool {
	na, errA := strconv.Atoi(strings.TrimSpace(a))
	nb, errB := strconv.Atoi(strings.TrimSpace(b))
	if errA == nil && errB == nil {
		return na == nb
	}
	return strings.TrimSpace(a) == strings.TrimSpace(b) && strings.TrimSpace(a) != ""
}

// round1 — округление до 0.1 (часы задержек).
func round1(h float64) float64 { return math.Round(h*10) / 10 }

// SetHistory подключает репозиторий бизнес-истории: нужен для «Истории движения
// вагона» из интерфейса (рейс адресуется строкой vagon_history, а не снимком).
func (s *VagonOpService) SetHistory(h port.HistoryRepository) { s.hist = h }

// SetDelays подключает эпизоды задержек (vagon_delay) для трейла и сводки
// «ехал N, из них X стоял»; nil — блок задержек на экране пуст.
func (s *VagonOpService) SetDelays(d port.VagonDelayRepository) { s.delays = d }
