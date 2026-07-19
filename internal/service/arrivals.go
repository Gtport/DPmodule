package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
)

// ArrivalsService — «История прибывших» (домашняя страница, перенос gtport
// HistoryTable). Читает бизнес-историю vagon_history (веха прибытия — статус 10:
// date_prib/plan_*/otkl проставлены Stage 2) и группирует как эталон:
// группа = index_pp + date_prib → подгруппы по index_main/naznach/gruzpol_s/sms_1
// → вагоны. Станция на фронте — это набор её терминалов (реестр ports, не хардкод):
// фильтр по naznach, как в gtport (Мыс = АЭ+ГУТ-2, Находка = УТ-1).
type ArrivalsService struct {
	repo port.HistoryRepository
	dir  *DirectoryCache
}

func NewArrivalsService(repo port.HistoryRepository, dir *DirectoryCache) *ArrivalsService {
	return &ArrivalsService{repo: repo, dir: dir}
}

// ArrivalVagonDTO — вагон подгруппы (разворот по клику).
type ArrivalVagonDTO struct {
	ID        string `json:"id"`
	Vagon     string `json:"vagon"`
	Shipments string `json:"shipments,omitempty"` // судовая партия
	Status    *int   `json:"status,omitempty"`
}

// ArrivalSubgroupDTO — подгруппа прибывшего поезда (одно назначение/получатель).
type ArrivalSubgroupDTO struct {
	Key         string            `json:"key"`
	IndexMain   string            `json:"index_main"`
	StationNach string            `json:"station_nach"`
	Naznach     string            `json:"naznach"`
	GruzpolS    string            `json:"gruzpol_s"`
	Sms1        string            `json:"sms_1,omitempty"`
	VagonCount  int               `json:"vagon_count"`
	Display     string            `json:"display"` // «(N)-783-Челутай АЭ» / «… ГУТ-2 → АЭ»
	Vagons      []ArrivalVagonDTO `json:"vagons"`
}

// ArrivalGroupDTO — прибывший поезд (группа index_pp + date_prib).
type ArrivalGroupDTO struct {
	Key        string               `json:"key"`
	IndexPp    string               `json:"index_pp"`
	StanNazn   string               `json:"stan_nazn"`
	DatePribD  *domain.LocalTime    `json:"date_prib_d"`
	DatePrib   *domain.LocalTime    `json:"date_prib"`
	PlanMsk    *domain.LocalTime    `json:"plan_msk"`
	PlanJd     *domain.LocalTime    `json:"plan_jd"`
	Otkl       string               `json:"otkl"`
	VagonCount int                  `json:"vagon_count"`
	SubGroups  []ArrivalSubgroupDTO `json:"sub_groups"`
}

// ArrivalsDTO — ответ ручки: группы за период + реестр терминалов (для колонок).
type ArrivalsDTO struct {
	From    string            `json:"from"`
	To      string            `json:"to"`
	Groups  []ArrivalGroupDTO `json:"groups"`
	Targets []TargetDTO       `json:"targets"`
	Total   int               `json:"total"`
}

// Groups — прибывшие за период [from; to] (yyyy-MM-dd; пусто — вчера/сегодня по
// МСК), с фильтром по терминалам naznach (пусто — все).
func (s *ArrivalsService) Groups(ctx context.Context, fromS, toS string, naznach []string) (ArrivalsDTO, error) {
	from, to, err := arrivalsRange(fromS, toS)
	if err != nil {
		return ArrivalsDTO{}, err
	}
	rows, err := s.repo.ArrivedRows(ctx, from, to, naznach)
	if err != nil {
		return ArrivalsDTO{}, err
	}
	groups := groupArrivals(rows)
	return ArrivalsDTO{
		From: from.String()[:10], To: to.String()[:10],
		Groups: groups, Targets: terminalTargets(s.dir), Total: len(groups),
	}, nil
}

// Terminals — реестр целей (терминал + его станция) для раскладки домашней
// страницы по станциям (не хардкод — ports).
func (s *ArrivalsService) Terminals() []TargetDTO {
	return terminalTargets(s.dir)
}

// arrivalsRange — границы периода из строк yyyy-MM-dd; дефолт «вчера — сегодня»
// (эталон gtport «СЕГОДНЯ/ВЧЕРА») от московского clock.Now().
func arrivalsRange(fromS, toS string) (domain.LocalTime, domain.LocalTime, error) {
	today := clock.Now().Time().Truncate(24 * time.Hour)
	from, to := today.AddDate(0, 0, -1), today
	if fromS != "" {
		t, err := time.Parse("2006-01-02", fromS)
		if err != nil {
			return domain.LocalTime{}, domain.LocalTime{}, fmt.Errorf("некорректная дата from %q (ожидается yyyy-MM-dd)", fromS)
		}
		from = t
	}
	if toS != "" {
		t, err := time.Parse("2006-01-02", toS)
		if err != nil {
			return domain.LocalTime{}, domain.LocalTime{}, fmt.Errorf("некорректная дата to %q (ожидается yyyy-MM-dd)", toS)
		}
		to = t
	}
	if to.Before(from) {
		from, to = to, from
	}
	return domain.LocalTime(from), domain.LocalTime(to), nil
}

// groupArrivals — группировка строк истории в поезда (эталон gtport):
// группа = index_pp + date_prib, подгруппа = index_main|naznach|gruzpol_s|sms_1.
// Строки приходят отсортированными (date_prib, index_pp, vagon) — порядок
// групп/вагонов сохраняется стабильным.
func groupArrivals(rows []domain.VagonHistory) []ArrivalGroupDTO {
	type subKey struct{ im, nz, gp, sms string }
	var order []string
	groups := map[string]*ArrivalGroupDTO{}
	subs := map[string]map[subKey]*ArrivalSubgroupDTO{}
	subOrder := map[string][]subKey{}

	for i := range rows {
		r := &rows[i]
		// Метка поезда: index_pp (нитка плана); для строк без матча с планом —
		// родительский индекс (иначе группа безымянная: старые записи истории
		// создавались до фиксации index_pp на прибытии).
		label := r.IndexPp
		if label == "" {
			label = r.IndexMain
		}
		gk := label + "|" + ltKey(r.DatePrib)
		g, ok := groups[gk]
		if !ok {
			g = &ArrivalGroupDTO{
				Key: gk, IndexPp: label, StanNazn: r.StanNazn,
				DatePribD: r.DatePribD, DatePrib: r.DatePrib,
				PlanMsk: r.PlanMsk, PlanJd: r.PlanJd, Otkl: r.Otkl,
			}
			groups[gk] = g
			subs[gk] = map[subKey]*ArrivalSubgroupDTO{}
			order = append(order, gk)
		}
		g.VagonCount++

		sk := subKey{r.IndexMain, r.Naznach, r.GruzpolS, r.Sms1}
		sg, ok := subs[gk][sk]
		if !ok {
			sg = &ArrivalSubgroupDTO{
				Key:       r.IndexMain + "|" + r.Naznach + "|" + r.GruzpolS + "|" + r.Sms1,
				IndexMain: r.IndexMain, StationNach: r.StationNach,
				Naznach: r.Naznach, GruzpolS: r.GruzpolS, Sms1: r.Sms1,
			}
			subs[gk][sk] = sg
			subOrder[gk] = append(subOrder[gk], sk)
		}
		sg.VagonCount++
		sg.Vagons = append(sg.Vagons, ArrivalVagonDTO{
			ID: r.ID, Vagon: r.Vagon, Shipments: r.Shipments, Status: r.Status,
		})
	}

	out := make([]ArrivalGroupDTO, 0, len(order))
	for _, gk := range order {
		g := groups[gk]
		for _, sk := range subOrder[gk] {
			sg := subs[gk][sk]
			sg.Display = arrivalDisplay(sg)
			g.SubGroups = append(g.SubGroups, *sg)
		}
		sort.SliceStable(g.SubGroups, func(i, j int) bool { return g.SubGroups[i].Key < g.SubGroups[j].Key })
		out = append(out, *g)
	}
	return out
}

// arrivalDisplay — строка состава подгруппы в формате gtport:
// «(59)-783-Челутай АЭ»; переставленный чужой груз — «(8)-782-Челутай ГУТ-2 → АЭ»
// (грузополучатель → фактическое назначение).
func arrivalDisplay(sg *ArrivalSubgroupDTO) string {
	dest := sg.Naznach
	if sg.GruzpolS != "" && sg.Naznach != "" && sg.GruzpolS != sg.Naznach {
		dest = sg.GruzpolS + " → " + sg.Naznach
	} else if dest == "" {
		dest = sg.GruzpolS
	}
	parts := []string{fmt.Sprintf("(%d)", sg.VagonCount)}
	if mid := indexMiddle(sg.IndexMain); mid != "" {
		parts = append(parts, mid)
	}
	if sg.StationNach != "" {
		parts = append(parts, sg.StationNach)
	}
	res := strings.Join(parts, "-")
	if dest != "" {
		res += " " + dest
	}
	return res
}

// indexMiddle — средняя часть индекса поезда «9379-783-9857» → «783».
func indexMiddle(index string) string {
	parts := strings.Split(index, "-")
	if len(parts) == 3 {
		return parts[1]
	}
	return ""
}

// ltKey — стабильный ключ времени для группировки (nil-безопасно).
func ltKey(lt *domain.LocalTime) string {
	if lt == nil {
		return ""
	}
	return lt.String()
}
