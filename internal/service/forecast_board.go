package service

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
)

// Экран «Новый прогноз» (перенос gtport PrognozNew): сервер отдаёт одним ответом
// сырьё для доски — едущие поезда с прогнозом Stage 4, прибывшие за сегодняшние
// сутки и линии выгрузки терминалов (норма + входящий остаток). Раскладку по
// датам-колонкам, ручные смещения и итоги (ВСЕГО/Выгрузка/Остаток/Все едет/
// В пути) считает клиент — как в gtport, где смещения и правки нормы живут
// только в сеансе и в БД не пишутся.
//
// Отличия от эталона — только в источниках (расхардкоживание):
//   - подтаблицы (АЭ / ГУТ-2×уголь-металл-чугун / УТ-1) — из справочника
//     port_cargo_line (kind=unload), а не из switch по имени порта;
//   - норма выгрузки — pc линии (fallback ports.pc_*), а не gt_port_speeds;
//   - «Остаток на 18:00» — вчерашний расчётный Ost «Грузовой работы», а не
//     таблицы vigr_at/vigr_ut/vigr_gut.

// ForecastSubGroup — строка таблицы прогноза: подгруппа вагонов поезда с одной
// станции отправления, одного назначения и группы груза.
type ForecastSubGroup struct {
	StationNach string            `json:"station_nach"`
	DateNach    *domain.LocalTime `json:"date_nach"` // дата погрузки (максимум по вагонам)
	VagonCount  int               `json:"vagon_count"`
	CargoGroup  string            `json:"cargo_group"`
	Naznach     string            `json:"naznach"`
	Color       string            `json:"color"`
}

// ForecastTrainGroup — едущий поезд с прогнозом прибытия (Stage 4).
type ForecastTrainGroup struct {
	Index       string             `json:"index"` // index_pp, если нитка задана планом
	StationOper string             `json:"station_oper"`
	Status      *int               `json:"status"` // 5 — брошен (красная подсветка)
	ProgJd      *domain.LocalTime  `json:"prog_jd"`
	SubGroups   []ForecastSubGroup `json:"sub_groups"`
}

// ForecastArrivedGroup — поезд, прибывший за сегодняшние сутки (vagon_history).
type ForecastArrivedGroup struct {
	IndexPp   string             `json:"index_pp"`
	DatePrib  *domain.LocalTime  `json:"date_prib"`
	SubGroups []ForecastSubGroup `json:"sub_groups"`
}

// ForecastLine — линия выгрузки терминала: подтаблица экрана, её норма и
// входящий остаток.
type ForecastLine struct {
	Terminal string `json:"terminal"`
	CargoKey string `json:"cargo_key"` // пусто — терминал считается одной таблицей
	Label    string `json:"label"`
	Pc       int    `json:"pc"`  // норма выгрузки, ваг/сут (стартовое значение поля «Выгрузка»)
	Ost      int    `json:"ost"` // «Остаток на 18:00»: вчерашний остаток линии
}

// ForecastBoardDTO — всё сырьё экрана одним ответом.
type ForecastBoardDTO struct {
	Groups  []ForecastTrainGroup   `json:"groups"`
	Arrived []ForecastArrivedGroup `json:"arrived"`
	Lines   []ForecastLine         `json:"lines"`
}

// ForecastBoard — сборка доски прогноза из RAM-снимка, истории и грузовой работы.
type ForecastBoard struct {
	actual  *ActualCache
	dir     *DirectoryCache
	history port.HistoryRepository
	cargo   *CargoWorkService
}

func NewForecastBoard(actual *ActualCache, dir *DirectoryCache,
	history port.HistoryRepository, cargo *CargoWorkService) *ForecastBoard {
	return &ForecastBoard{actual: actual, dir: dir, history: history, cargo: cargo}
}

// Board собирает ответ экрана. «Сегодня» — календарные сутки МСК (как в gtport,
// где история бралась за текущую дату), «вчера» — предыдущие для остатков.
func (b *ForecastBoard) Board(ctx context.Context) (ForecastBoardDTO, error) {
	known := map[string]bool{}
	for _, n := range b.dir.EnabledTerminals() {
		known[n] = true
	}

	dto := ForecastBoardDTO{Groups: forecastTransitGroups(b.actual.All(), known)}

	today := domain.LocalTime(dayStart(clock.Now().Time()))
	rows, err := b.history.ArrivedRows(ctx, today, today, nil)
	if err != nil {
		return ForecastBoardDTO{}, fmt.Errorf("прибывшие за сутки: %w", err)
	}
	dto.Arrived = forecastArrivedGroups(rows, known)

	if dto.Lines, err = b.terminalLines(ctx); err != nil {
		return ForecastBoardDTO{}, err
	}
	return dto, nil
}

// terminalLines — линии выгрузки всех включённых терминалов + вчерашний остаток.
// Терминал без строк в port_cargo_line получает одну «сводную» линию с pc_total
// и нулевым остатком — экран остаётся рабочим, пока справочник не заполнили.
func (b *ForecastBoard) terminalLines(ctx context.Context) ([]ForecastLine, error) {
	yesterday := dayStart(clock.Now().Time()).AddDate(0, 0, -1)
	out := []ForecastLine{}
	for _, term := range b.dir.EnabledTerminals() {
		lns, err := b.cargo.UnloadLines(ctx, term)
		if err != nil {
			return nil, err
		}
		if len(lns) == 0 {
			pc := 0
			if p, ok := b.dir.PortByNameS(term); ok && p.PcTotal != nil {
				pc = *p.PcTotal
			}
			out = append(out, ForecastLine{Terminal: term, Pc: pc})
			continue
		}
		// Остаток на конец вчерашних суток: лист «Грузовой работы» (несохранённый
		// пересобирается на лету — цифра живая даже без закрытого учёта).
		day, err := b.cargo.Day(ctx, yesterday, term)
		if err != nil {
			return nil, fmt.Errorf("остаток за вчера (%s): %w", term, err)
		}
		ost := map[string]int{}
		for _, l := range day.Lines {
			ost[l.CargoKey] = l.Ost
		}
		for _, ln := range lns {
			out = append(out, ForecastLine{
				Terminal: term, CargoKey: ln.CargoKey, Label: ln.Label,
				Pc: b.cargo.LinePc(ln, term), Ost: ost[ln.CargoKey],
			})
		}
	}
	return out, nil
}

// forecastTransitGroups — группировка снимка в поезда с подгруппами (перенос
// gtport groupByExtendedCollectiveTrain + has_prog_jd + normalizeTrainIndices):
// только вагоны в подходе (статус < 9) с рассчитанным prog_jd и назначением на
// известный терминал; индекс поезда — плановая нитка index_pp, если задана.
func forecastTransitGroups(rows []domain.Dislocation, known map[string]bool) []ForecastTrainGroup {
	type agg struct {
		g    ForecastTrainGroup
		subs map[string]*ForecastSubGroup
		ord  []string
	}
	trains := map[string]*agg{}
	var order []string

	for _, r := range rows {
		if r.Status != nil && *r.Status >= 9 {
			continue // кандидат прибытия / прибыл / выгружен — не в подходе
		}
		if r.ProgJd == nil || !known[r.Naznach] {
			continue
		}
		index := r.Index
		if r.IndexPp != "" {
			index = r.IndexPp // gtport: поезд склеивается по плановой нитке
		}
		status := ""
		if r.Status != nil {
			status = strconv.Itoa(*r.Status)
		}
		key := index + "|" + r.StationOper + "|" + status
		t, ok := trains[key]
		if !ok {
			t = &agg{
				g: ForecastTrainGroup{
					Index: index, StationOper: r.StationOper,
					Status: r.Status, ProgJd: r.ProgJd,
				},
				subs: map[string]*ForecastSubGroup{},
			}
			trains[key] = t
			order = append(order, key)
		}

		subKey := r.IndexMain + "|" + r.StationNach + "|" + r.GruzpolS + "|" + r.Naznach + "|" + r.CargoGroup
		sg, ok := t.subs[subKey]
		if !ok {
			sg = &ForecastSubGroup{
				StationNach: r.StationNach, CargoGroup: r.CargoGroup,
				Naznach: r.Naznach, Color: r.Color,
			}
			t.subs[subKey] = sg
			t.ord = append(t.ord, subKey)
		}
		sg.VagonCount++
		if r.DateNach != nil && (sg.DateNach == nil ||
			time.Time(*r.DateNach).After(time.Time(*sg.DateNach))) {
			sg.DateNach = r.DateNach
		}
		if sg.Color == "" {
			sg.Color = r.Color
		}
	}

	out := make([]ForecastTrainGroup, 0, len(order))
	for _, key := range order {
		t := trains[key]
		for _, sk := range t.ord {
			t.g.SubGroups = append(t.g.SubGroups, *t.subs[sk])
		}
		sort.SliceStable(t.g.SubGroups, func(i, j int) bool {
			return t.g.SubGroups[i].StationNach < t.g.SubGroups[j].StationNach
		})
		out = append(out, t.g)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ti, tj := time.Time(*out[i].ProgJd), time.Time(*out[j].ProgJd)
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		return out[i].Index < out[j].Index
	})
	return out
}

// forecastArrivedGroups — прибывшие поезда из вех истории: группа по
// (index_pp, date_prib), подгруппы — как у едущих (перенос gtport
// GetHistoryGroups для экрана прогноза).
func forecastArrivedGroups(rows []domain.VagonHistory, known map[string]bool) []ForecastArrivedGroup {
	type agg struct {
		g    ForecastArrivedGroup
		subs map[string]*ForecastSubGroup
		ord  []string
	}
	groups := map[string]*agg{}
	var order []string

	for _, r := range rows {
		if !known[r.Naznach] {
			continue
		}
		prib := ""
		if r.DatePrib != nil {
			prib = time.Time(*r.DatePrib).Format("2006-01-02 15:04:05")
		}
		key := r.IndexPp + "|" + prib
		g, ok := groups[key]
		if !ok {
			g = &agg{
				g:    ForecastArrivedGroup{IndexPp: r.IndexPp, DatePrib: r.DatePrib},
				subs: map[string]*ForecastSubGroup{},
			}
			groups[key] = g
			order = append(order, key)
		}

		subKey := r.IndexMain + "|" + r.StationNach + "|" + r.Naznach + "|" + r.CargoGroup
		sg, ok := g.subs[subKey]
		if !ok {
			sg = &ForecastSubGroup{
				StationNach: r.StationNach, CargoGroup: r.CargoGroup,
				Naznach: r.Naznach, Color: r.Color,
			}
			g.subs[subKey] = sg
			g.ord = append(g.ord, subKey)
		}
		sg.VagonCount++
		if r.DateNachD != nil && (sg.DateNach == nil ||
			time.Time(*r.DateNachD).After(time.Time(*sg.DateNach))) {
			sg.DateNach = r.DateNachD
		}
		if sg.Color == "" {
			sg.Color = r.Color
		}
	}

	out := make([]ForecastArrivedGroup, 0, len(order))
	for _, key := range order {
		g := groups[key]
		for _, sk := range g.ord {
			g.g.SubGroups = append(g.g.SubGroups, *g.subs[sk])
		}
		out = append(out, g.g)
	}
	sort.SliceStable(out, func(i, j int) bool {
		var ti, tj time.Time
		if out[i].DatePrib != nil {
			ti = time.Time(*out[i].DatePrib)
		}
		if out[j].DatePrib != nil {
			tj = time.Time(*out[j].DatePrib)
		}
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		return out[i].IndexPp < out[j].IndexPp
	})
	return out
}
