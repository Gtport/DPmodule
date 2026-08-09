package service

import (
	"context"
	"sort"
	"time"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

// MissingService — списки вагонов «сбоку от снимка» для интерфейса:
//   - пропавшие: записи-8 из таблицы кандидатов (status9) с последней известной
//     позицией; появляются в reconcileCandidates (S2-1b), уходят возвратом
//     вагона в поток либо автоочисткой по TTL (S2-1c);
//   - доноры перегруза: записи статуса 6 (таблица status6), у которых забирают
//     груз/назначение приёмники (S2-3c).
//
// Только чтение. Обе таблицы — копии структуры dislocation, поэтому у строк
// есть id рейса: интерфейс адресует им «Историю движения вагона».
type MissingService struct {
	status9 *Status9Cache
	status6 *Status6Cache
}

func NewMissingService(status9 *Status9Cache, status6 *Status6Cache) *MissingService {
	return &MissingService{status9: status9, status6: status6}
}

// MissingVagonDTO — строка экрана: последняя известная позиция + давность пропажи.
// ID — id рейса (= dislocation.id = vagon_history.id): по нему интерфейс
// открывает историю движения вагона.
type MissingVagonDTO struct {
	ID           string            `json:"id"`
	Vagon        string            `json:"vagon"`
	Index        string            `json:"index"`        // последний поездной индекс
	StationOper  string            `json:"station_oper"` // где видели в последний раз
	DorogaOper   string            `json:"doroga_oper"`
	OperS        string            `json:"oper_s"`  // последняя операция
	TimeOp       *domain.LocalTime `json:"time_op"` // время последней операции
	Naznach      string            `json:"naznach"` // терминал назначения
	GruzpolS     string            `json:"gruzpol_s"`
	StanNazn     string            `json:"stan_nazn"`
	CargoS       string            `json:"cargo_s"`
	Ves          *float64          `json:"ves"`
	DateDostav   *domain.LocalTime `json:"date_dostav"`
	MissingSince domain.LocalTime  `json:"missing_since"` // когда зафиксирована пропажа
	DaysMissing  int               `json:"days_missing"`  // полных суток с пропажи (от «сейчас» МСК)
}

// MissingSubgroupDTO — подгруппа пропавшего поезда (одно назначение/получатель),
// раскрывается до вагонов. Display — тот же формат, что у подгрупп прибывших.
type MissingSubgroupDTO struct {
	Key         string            `json:"key"` // index_main|naznach|gruzpol_s
	IndexMain   string            `json:"index_main"`
	StationNach string            `json:"station_nach"`
	Naznach     string            `json:"naznach"`
	GruzpolS    string            `json:"gruzpol_s"`
	VagonCount  int               `json:"vagon_count"`
	Display     string            `json:"display"` // «(N)-783-Челутай АЭ»
	Vagons      []MissingVagonDTO `json:"vagons"`
}

// MissingGroupDTO — пропавший поезд: вагоны одного индекса и станции назначения
// (ключ — как у кандидатов прибытия). Позиция группы — самая свежая операция
// среди её вагонов (где состав видели в последний раз).
type MissingGroupDTO struct {
	Key          string               `json:"key"` // index|stan_nazn
	Index        string               `json:"index"`
	StanNazn     string               `json:"stan_nazn"`
	StationOper  string               `json:"station_oper"`
	DorogaOper   string               `json:"doroga_oper"`
	OperS        string               `json:"oper_s"`
	TimeOp       *domain.LocalTime    `json:"time_op"`       // самая свежая операция группы
	MissingSince domain.LocalTime     `json:"missing_since"` // самая свежая фиксация пропажи
	DaysMissing  int                  `json:"days_missing"`  // от самой свежей фиксации
	VagonCount   int                  `json:"vagon_count"`
	SubGroups    []MissingSubgroupDTO `json:"sub_groups"`
}

// Groups — пропавшие агрегированно: поезд → подгруппа → вагоны, с фильтром по
// терминалам naznach (пусто — все): станционные карточки «Кандидаты на
// прибытие» показывают каждой станции только её пропавших.
func (s *MissingService) Groups(ctx context.Context, naznach []string) ([]MissingGroupDTO, error) {
	rows, err := s.status9.MissingRows(ctx)
	if err != nil {
		return nil, err
	}
	return groupMissing(filterByNaznach(rows, naznach), time.Time(clock.Now())), nil
}

// filterByNaznach — только записи с назначением из списка (пусто — как есть).
func filterByNaznach(rows []domain.Dislocation, naznach []string) []domain.Dislocation {
	if len(naznach) == 0 {
		return rows
	}
	nz := make(map[string]struct{}, len(naznach))
	for _, n := range naznach {
		nz[n] = struct{}{}
	}
	out := make([]domain.Dislocation, 0, len(rows))
	for _, r := range rows {
		if _, ok := nz[r.Naznach]; ok {
			out = append(out, r)
		}
	}
	return out
}

// groupMissing — группировка записей-8 (образец — Candidates): группа =
// index|stan_nazn, подгруппа = index_main|naznach|gruzpol_s. Строки приходят
// свежепропавшими первыми (updated_at DESC) — insertion-order сохраняет это
// и для групп.
func groupMissing(rows []domain.Dislocation, now time.Time) []MissingGroupDTO {
	type subKey struct{ im, nzn, gp string }
	var order []string
	groups := map[string]*MissingGroupDTO{}
	subs := map[string]map[subKey]*MissingSubgroupDTO{}
	subOrder := map[string][]subKey{}

	for i := range rows {
		r := &rows[i]
		gk := r.Index + "|" + r.StanNazn
		g, ok := groups[gk]
		if !ok {
			g = &MissingGroupDTO{
				Key: gk, Index: r.Index, StanNazn: r.StanNazn,
				StationOper: r.StationOper, DorogaOper: r.DorogaOper, OperS: r.OperS,
				TimeOp: r.TimeOp, MissingSince: r.UpdatedAt,
			}
			groups[gk] = g
			subs[gk] = map[subKey]*MissingSubgroupDTO{}
			order = append(order, gk)
		}
		g.VagonCount++
		// Позиция группы — по вагону с самой свежей операцией.
		if r.TimeOp != nil && (g.TimeOp == nil || g.TimeOp.Time().Before(r.TimeOp.Time())) {
			g.TimeOp = r.TimeOp
			g.StationOper, g.DorogaOper, g.OperS = r.StationOper, r.DorogaOper, r.OperS
		}
		if g.MissingSince.Time().Before(r.UpdatedAt.Time()) {
			g.MissingSince = r.UpdatedAt
		}

		sk := subKey{r.IndexMain, r.Naznach, r.GruzpolS}
		sg, ok := subs[gk][sk]
		if !ok {
			sg = &MissingSubgroupDTO{
				Key:       r.IndexMain + "|" + r.Naznach + "|" + r.GruzpolS,
				IndexMain: r.IndexMain, StationNach: r.StationNach,
				Naznach: r.Naznach, GruzpolS: r.GruzpolS,
			}
			subs[gk][sk] = sg
			subOrder[gk] = append(subOrder[gk], sk)
		}
		sg.VagonCount++
		v := MissingVagonDTO{
			ID: r.ID, Vagon: r.Vagon, Index: r.Index,
			StationOper: r.StationOper, DorogaOper: r.DorogaOper,
			OperS: r.OperS, TimeOp: r.TimeOp,
			Naznach: r.Naznach, GruzpolS: r.GruzpolS, StanNazn: r.StanNazn,
			CargoS: r.CargoS, Ves: r.Ves, DateDostav: r.DateDostav,
			MissingSince: r.UpdatedAt,
		}
		if !r.UpdatedAt.IsZero() {
			v.DaysMissing = int(now.Sub(time.Time(r.UpdatedAt)).Hours() / 24)
		}
		sg.Vagons = append(sg.Vagons, v)
	}

	out := make([]MissingGroupDTO, 0, len(order))
	for _, gk := range order {
		g := groups[gk]
		for _, sk := range subOrder[gk] {
			sg := subs[gk][sk]
			sg.Display = displayLine(sg.VagonCount, sg.IndexMain, sg.StationNach, sg.Naznach, sg.GruzpolS)
			g.SubGroups = append(g.SubGroups, *sg)
		}
		sort.SliceStable(g.SubGroups, func(i, j int) bool { return g.SubGroups[i].Key < g.SubGroups[j].Key })
		if !g.MissingSince.IsZero() {
			g.DaysMissing = int(now.Sub(time.Time(g.MissingSince)).Hours() / 24)
		}
		out = append(out, *g)
	}
	return out
}

// Status6VagonDTO — строка списка доноров перегруза (статус 6): последняя
// известная позиция и груз, который у донора могут забрать приёмники.
type Status6VagonDTO struct {
	ID          string            `json:"id"`
	Vagon       string            `json:"vagon"`
	Index       string            `json:"index"`
	StationOper string            `json:"station_oper"`
	DorogaOper  string            `json:"doroga_oper"`
	OperS       string            `json:"oper_s"`
	TimeOp      *domain.LocalTime `json:"time_op"`
	Naznach     string            `json:"naznach"`
	GruzpolS    string            `json:"gruzpol_s"`
	StanNazn    string            `json:"stan_nazn"`
	CargoS      string            `json:"cargo_s"`
	Ves         *float64          `json:"ves"`
	DateDostav  *domain.LocalTime `json:"date_dostav"`
	Since       domain.LocalTime  `json:"since"` // когда запись донора обновлена
	DaysDonor   int               `json:"days_donor"`
}

// Donors — доноры перегруза (статус 6) из RAM-кэша, свежие первыми.
func (s *MissingService) Donors() []Status6VagonDTO {
	if s.status6 == nil {
		return nil
	}
	now := time.Time(clock.Now())
	rows := s.status6.Donors()
	out := make([]Status6VagonDTO, 0, len(rows))
	for _, r := range rows {
		d := Status6VagonDTO{
			ID: r.ID, Vagon: r.Vagon, Index: r.Index,
			StationOper: r.StationOper, DorogaOper: r.DorogaOper,
			OperS: r.OperS, TimeOp: r.TimeOp,
			Naznach: r.Naznach, GruzpolS: r.GruzpolS, StanNazn: r.StanNazn,
			CargoS: r.CargoS, Ves: r.Ves, DateDostav: r.DateDostav,
			Since: r.UpdatedAt,
		}
		if !r.UpdatedAt.IsZero() {
			d.DaysDonor = int(now.Sub(time.Time(r.UpdatedAt)).Hours() / 24)
		}
		out = append(out, d)
	}
	// Свежие доноры первыми — как в списке пропавших.
	sort.Slice(out, func(i, j int) bool {
		return time.Time(out[i].Since).After(time.Time(out[j].Since))
	})
	return out
}
