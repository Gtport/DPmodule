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
	"strconv"
	"strings"

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
	Ops     []TrailOp // все операции визита (для разворота и Excel)
}

// TrailView — весь трейл рейса плюс период фактически полученной истории:
// оператор сначала смотрит, что уже есть в базе, и решает, обновлять ли из АСУ.
type TrailView struct {
	ID       string
	Vagon    string
	DateNach *domain.LocalTime // дата погрузки (начало рейса)
	Terminal string            // gruzpol_s — терминал, он же ключ клиента провайдера
	From     *domain.LocalTime // время первой операции
	To       *domain.LocalTime // время последней операции
	Count    int               // всего операций
	Visits   []TrailVisit
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
	return buildTrailView(row, ops, s.dir), nil
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
	return buildTrailView(row, ops, s.dir), nil
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

// SetHistory подключает репозиторий бизнес-истории: нужен для «Истории движения
// вагона» из интерфейса (рейс адресуется строкой vagon_history, а не снимком).
func (s *VagonOpService) SetHistory(h port.HistoryRepository) { s.hist = h }
