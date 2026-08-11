package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
	"github.com/Gtport/DPmodule/internal/report"
)

// OverdueService — «Просрочка доставки» карточки «Работа»: вагоны текущего
// снимка с истекшим нормативным сроком доставки (delay > 0), группами по
// накладной — накладная является единицей претензии к перевозчику (пени по
// ст. 97 УЖТ считаются от провозной платы по накладной). Прибывшие и
// выгруженные (статусы 10/12) не показываются: их просрочка зафиксирована в
// vagon_history при прибытии и попадает в отчёт для претензии (ClaimExcel), а
// снимочный delay продолжает расти от «сегодня» и для них завышен (решение
// владельца 11.08.2026). Порожние В ПУТИ (статус 6) тоже исключены (решение
// владельца 11.08.2026, вторая итерация); порожние ПОД ПОГРУЗКУ живут обычным
// деревом статусов движения и в выборке остаются.
type OverdueService struct {
	actual  *ActualCache
	history port.HistoryRepository
}

func NewOverdueService(actual *ActualCache, history port.HistoryRepository) *OverdueService {
	return &OverdueService{actual: actual, history: history}
}

// OverdueVagonDTO — вагон разворота группы. ID — id рейса (= vagon_history.id):
// по нему интерфейс открывает «Историю движения вагона».
type OverdueVagonDTO struct {
	ID          string            `json:"id"`
	Vagon       string            `json:"vagon"`
	Index       string            `json:"index"`
	StationOper string            `json:"station_oper"`
	OperS       string            `json:"oper_s"`
	TimeOp      *domain.LocalTime `json:"time_op"`
	Status      *int              `json:"status"`
	CargoS      string            `json:"cargo_s"`
	Ves         *float64          `json:"ves"`
	Naznach     string            `json:"naznach"`
	DateNach    *domain.LocalTime `json:"date_nach"`
	DateDostav  *domain.LocalTime `json:"date_dostav"`
	Delay       int               `json:"delay"`
}

// OverdueGroupDTO — группа просроченных вагонов одной накладной. Ключ —
// invoice, у вагонов без текущей накладной — invoice_main; вагоны совсем без
// накладной собираются в одну группу с пустым ключом («Без накладной» на
// экране) — в претензию такие не годятся, но прятать их молча нельзя.
type OverdueGroupDTO struct {
	Key         string            `json:"key"`
	Invoice     string            `json:"invoice"`
	InvoiceMain string            `json:"invoice_main"`
	StationNach string            `json:"station_nach"`
	Gruzotpr    string            `json:"gruzotpr"`
	StanNazn    string            `json:"stan_nazn"`
	GruzpolS    string            `json:"gruzpol_s"`
	CargoS      string            `json:"cargo_s"`
	VagonCount  int               `json:"vagon_count"`
	MaxDelay    int               `json:"max_delay"`
	DateDostav  *domain.LocalTime `json:"date_dostav"` // ранний норматив группы
	Vagons      []OverdueVagonDTO `json:"vagons"`
}

// Groups — просроченные вагоны снимка группами по накладной. Порядок групп:
// по терминалу (gruzpol_s, пустой — в конце), внутри терминала — по максимальной
// просрочке от большой к малой (решение владельца 11.08.2026); вагоны внутри
// группы — по номеру.
func (s *OverdueService) Groups() []OverdueGroupDTO {
	rows := s.actual.All()
	groups := map[string]*OverdueGroupDTO{}
	var order []string
	for i := range rows {
		r := &rows[i]
		if r.Vagon == "" || r.Delay == nil || *r.Delay <= 0 {
			continue
		}
		// 10/12 — уже на станции назначения (просрочка зафиксирована в
		// истории), 6 — порожний в пути (решение владельца 11.08.2026).
		if r.Status != nil && (*r.Status == 6 || *r.Status == 10 || *r.Status == 12) {
			continue
		}
		key := r.Invoice
		if key == "" {
			key = r.InvoiceMain
		}
		g, ok := groups[key]
		if !ok {
			g = &OverdueGroupDTO{
				Key: key, Invoice: r.Invoice, InvoiceMain: r.InvoiceMain,
				StationNach: r.StationNach, Gruzotpr: r.Gruzotpr,
				StanNazn: r.StanNazn, GruzpolS: r.GruzpolS, CargoS: r.CargoS,
			}
			groups[key] = g
			order = append(order, key)
		}
		g.VagonCount++
		if *r.Delay > g.MaxDelay {
			g.MaxDelay = *r.Delay
		}
		if r.DateDostav != nil &&
			(g.DateDostav == nil || time.Time(*r.DateDostav).Before(time.Time(*g.DateDostav))) {
			g.DateDostav = r.DateDostav
		}
		g.Vagons = append(g.Vagons, OverdueVagonDTO{
			ID: r.ID, Vagon: r.Vagon, Index: r.Index,
			StationOper: r.StationOper, OperS: r.OperS, TimeOp: r.TimeOp,
			Status: r.Status, CargoS: r.CargoS, Ves: r.Ves,
			Naznach: r.Naznach, DateNach: r.DateNach,
			DateDostav: r.DateDostav, Delay: *r.Delay,
		})
	}

	out := make([]OverdueGroupDTO, 0, len(order))
	for _, k := range order {
		g := groups[k]
		// ActualCache.All() отдаёт вагоны в порядке мапы — сортируем для стабильного экрана.
		sort.Slice(g.Vagons, func(i, j int) bool { return g.Vagons[i].Vagon < g.Vagons[j].Vagon })
		out = append(out, *g)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.GruzpolS != b.GruzpolS {
			if a.GruzpolS == "" || b.GruzpolS == "" {
				return b.GruzpolS == "" // без терминала — в конец
			}
			return a.GruzpolS < b.GruzpolS
		}
		return a.MaxDelay > b.MaxDelay
	})
	return out
}

// ClaimExcel — «Отчёт для претензии»: накладные и вагоны с фактом просрочки
// доставки (vagon_history.delay > 0, фиксируется при прибытии) за период
// прибытия [from; to], опционально по одному терминалу (gruzpol_s — та же
// семантика, что чипы экрана истории). Сортировка по накладной — генератор
// агрегирует лист «Накладные» по смене ключа.
func (s *OverdueService) ClaimExcel(ctx context.Context, from, to domain.LocalTime, terminal string) ([]byte, string, error) {
	f := domain.HistorySearchFilter{
		DatePribDFrom: &from,
		DatePribDTo:   &to,
		OnlyOverdue:   true,
	}
	if terminal != "" {
		f.GruzpolS = []string{terminal}
	}
	_, total, err := s.history.SearchRows(ctx, f, "invoice", false, 1, 0)
	if err != nil {
		return nil, "", err
	}
	if total == 0 {
		return nil, "", fmt.Errorf("%w: за период нет прибытий с просрочкой доставки", ErrHistorySearchInvalid)
	}
	if total > HistoryExportMax {
		return nil, "", fmt.Errorf("%w: найдено %d строк — сузьте период (потолок экспорта %d)",
			ErrHistorySearchInvalid, total, HistoryExportMax)
	}
	data, err := report.OverdueClaimXLSX(func(fn func(domain.VagonHistory) error) error {
		return s.history.IterateSearch(ctx, f, "invoice", false, fn)
	})
	if err != nil {
		return nil, "", err
	}
	name := fmt.Sprintf("просрочка_доставки_%s-%s.xlsx",
		time.Time(from).Format("02.01.2006"), time.Time(to).Format("02.01.2006"))
	return data, name, nil
}
