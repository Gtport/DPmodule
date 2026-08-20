// Команда gtsnapconv — разовый конвертер сохранённых прогнозов старого GTport
// (gt_saved_plans) в формат dpport.gt_forecast_snapshot. Часть переноса
// 10 месяцев истории (правила — отчёт «Сверка GTport → DPport», 20.08.2026).
//
// JSON собирается ТЕМИ ЖЕ структурами GtTrainDTO/GtFlowDTO/GtFreeSlotDTO/
// GtSimulateRequest, что и боевой контракт вкладки, — совместимость с архивным
// просмотром и CSV-аналитикой гарантируется типами, а не соглашением.
//
// Времена: старые блобы несут wall-time с суффиксом «Z» (хак фронта gtport
// «московское как UTC») — суффикс отбрасывается БЕЗ сдвига значения.
//
//	go run ./cmd/gtsnapconv \
//	  -src "postgres://gtport_app@localhost:5433/gtport_src?sslmode=disable" \
//	  -dst "postgres://gtport_app@localhost:5433/dpport?sslmode=disable" \
//	  -out _reference/seed_gtport/gt_forecast_snapshot.csv
//
// Пароль роли берётся из PG_PASSWORD (как у остальных разовых команд).
// Заливка результата — scripts/import_gtport_snapshots.sql.
package main

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	_ "github.com/lib/pq"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/service"
)

// Старые структуры gt_saved_plans (формат SavedPlansDialog gtport).
type oldTrain struct {
	Index        string   `json:"index"`
	StationOper  string   `json:"station_oper"`
	Status       string   `json:"status"`
	IsHistorical bool     `json:"is_historical"`
	PlanJd       string   `json:"plan_jd"`
	ProgJd       string   `json:"prog_jd"`
	ProgMsk      string   `json:"prog_msk"`
	RaschJd      string   `json:"rasch_jd"`
	RaschMsk     string   `json:"rasch_msk"`
	Mistake      *float64 `json:"mistake"`
	ToGo         *float64 `json:"to_go"`
	DelayHours   float64  `json:"delay_hours"`
	VagonCount   int      `json:"vagon_count"`
	SubGroups    []oldSub `json:"sub_groups"`
}

type oldSub struct {
	StationNach string `json:"station_nach"`
	DateNach    string `json:"date_nach"`
	VagonCount  int    `json:"vagon_count"`
	CargoGroup  string `json:"cargo_group"`
	Naznach     string `json:"naznach"`
	GruzpolS    string `json:"gruzpol_s"`
	Color       string `json:"color"`
	IndexMain   string `json:"index_main"`
}

type oldGanttDay struct {
	Date              string       `json:"date"`
	Port              string       `json:"port"`
	CargoType         string       `json:"cargoType"`
	Speed             int          `json:"speed"`
	NormSpeed         int          `json:"normSpeed"`
	IncomingTotal     int          `json:"incomingTotal"`
	Arrival           int          `json:"arrival"`
	TotalFormation    int          `json:"totalFormation"`
	UsefulFormation   int          `json:"usefulFormation"`
	Unloaded          int          `json:"unloaded"`
	Remaining         int          `json:"remaining"`
	TotalWaitTime     float64      `json:"totalWaitTime"`
	Operations        []oldOp      `json:"operations"`
	CarriedOverTrains []oldCarried `json:"carriedOverTrains"`
}

type oldOp struct {
	TrainIndex          string  `json:"trainIndex"`
	TrainName           string  `json:"trainName"`
	StationNach         string  `json:"stationNach"`
	IndexMain           string  `json:"indexMain"`
	GruzpolS            string  `json:"gruzpol_s"`
	OriginalTrainIndex  string  `json:"originalTrainIndex"`
	StartTime           string  `json:"startTime"`
	EndTime             string  `json:"endTime"`
	StartRailwayTime    string  `json:"startRailwayTime"`
	EndRailwayTime      string  `json:"endRailwayTime"`
	Wagons              int     `json:"wagons"`
	TotalWagons         int     `json:"totalWagons"`
	Color               string  `json:"color"`
	IsRemainder         bool    `json:"isRemainder"`
	IsCarriedOver       bool    `json:"isCarriedOver"`
	IsPartialRemainder  bool    `json:"isPartialRemainder"`
	WaitTime            float64 `json:"waitTime"`
	OriginalArrivalTime string  `json:"originalArrivalTime"`
}

type oldCarried struct {
	Index      string `json:"index"`
	VagonCount int    `json:"vagon_count"`
}

type oldStatDay struct {
	Date        string `json:"date"` // «19.08.26» — ЖД-сутки
	UnusedSlots []struct {
		Time  string `json:"time"` // «03:47»
		Count int    `json:"count"`
	} `json:"unused_slots"`
}

func main() {
	src := flag.String("src", "postgres://gtport_app@localhost:5433/gtport_src?sslmode=disable", "DSN базы старого GTport")
	dst := flag.String("dst", "postgres://gtport_app@localhost:5433/dpport?sslmode=disable", "DSN dpport (справочники линий и цветов)")
	out := flag.String("out", "_reference/seed_gtport/gt_forecast_snapshot.csv", "куда писать CSV")
	modeMap := flag.String("modes", "АЭ+ГУТ=985702,УТ-1=984700", "режим старой вкладки = код причальной станции")
	flag.Parse()

	modes := map[string]string{}
	for _, pair := range strings.Split(*modeMap, ",") {
		k, v, ok := strings.Cut(pair, "=")
		if !ok {
			log.Fatalf("флаг -modes: не разобрана пара %q", pair)
		}
		modes[k] = v
	}

	srcDB := open(*src)
	defer srcDB.Close()
	dstDB := open(*dst)
	defer dstDB.Close()

	labels, colors := loadLines(dstDB)

	rows, err := srcDB.Query(`SELECT plan_date::text, page_mode, start_date::text, days_count,
		trains::text, initial_remainders::text, gantt::text, port_statistics::text,
		coalesce(saved_by,''), created_at::text, updated_at::text
		FROM public.gt_saved_plans ORDER BY plan_date, page_mode`)
	if err != nil {
		log.Fatalf("чтение gt_saved_plans: %v", err)
	}
	defer rows.Close()

	f, err := os.Create(*out)
	if err != nil {
		log.Fatalf("создание %s: %v", *out, err)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write([]string{"plan_date", "station", "start_date", "days_count",
		"request", "trains", "flows", "free_slots", "journal", "saved_by", "created_at", "updated_at"})

	total, converted := 0, 0
	var failed []string
	for rows.Next() {
		total++
		var planDate, mode, startDate, trainsJS, remJS, ganttJS, statJS, savedBy, createdAt, updatedAt string
		var days int
		if err := rows.Scan(&planDate, &mode, &startDate, &days, &trainsJS, &remJS, &ganttJS, &statJS, &savedBy, &createdAt, &updatedAt); err != nil {
			log.Fatalf("scan: %v", err)
		}
		rec, err := convert(modes, labels, colors, planDate, mode, startDate, days, trainsJS, remJS, ganttJS, statJS)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s / %s: %v", planDate, mode, err))
			continue
		}
		_ = w.Write(append([]string{planDate, rec.station, startDate, fmt.Sprint(days)},
			rec.request, rec.trains, rec.flows, rec.freeSlots, "[]", savedBy,
			normTS(createdAt), normTS(updatedAt)))
		converted++
	}
	if err := rows.Err(); err != nil {
		log.Fatalf("чтение: %v", err)
	}
	w.Flush()
	if err := w.Error(); err != nil {
		log.Fatalf("запись CSV: %v", err)
	}

	fmt.Printf("снапшотов: %d, сконвертировано: %d, отказов: %d\n", total, converted, len(failed))
	for _, msg := range failed {
		fmt.Println("  ОТКАЗ:", msg)
	}
	if len(failed) > 0 {
		os.Exit(1) // отказы разбираются руками; частичный CSV пригоден к заливке
	}
}

type converted struct {
	station, request, trains, flows, freeSlots string
}

func convert(modes map[string]string, labels map[string]string, colors map[string]string,
	planDate, mode, startDate string, days int,
	trainsJS, remJS, ganttJS, statJS string) (*converted, error) {

	station, ok := modes[mode]
	if !ok {
		return nil, fmt.Errorf("неизвестный режим %q", mode)
	}

	// ── trains ──
	var oldTrains []oldTrain
	if err := json.Unmarshal([]byte(trainsJS), &oldTrains); err != nil {
		return nil, fmt.Errorf("trains: %w", err)
	}
	newTrains := make([]service.GtTrainDTO, 0, len(oldTrains))
	for _, t := range oldTrains {
		status := t.Status
		if t.IsHistorical {
			status = "history"
		}
		subs := make([]service.GtSubGroupDTO, 0, len(t.SubGroups))
		for _, sg := range t.SubGroups {
			subs = append(subs, service.GtSubGroupDTO{
				Key:         strings.Join([]string{sg.IndexMain, sg.StationNach, sg.GruzpolS, sg.Naznach, sg.CargoGroup}, "|"),
				StationNach: sg.StationNach, DateNach: lt(sg.DateNach),
				VagonCount: sg.VagonCount, CargoGroup: sg.CargoGroup, Naznach: sg.Naznach,
				Color: sg.Color, IndexMain: sg.IndexMain,
			})
		}
		newTrains = append(newTrains, service.GtTrainDTO{
			Index: t.Index, StationOper: t.StationOper, Status: status, IsArrived: t.IsHistorical,
			PlanJd: lt(t.PlanJd), ProgJd: lt(t.ProgJd), ProgMsk: lt(t.ProgMsk),
			RaschJd: lt(t.RaschJd), RaschMsk: lt(t.RaschMsk),
			Mistake: t.Mistake, ToGo: t.ToGo, DelayHours: t.DelayHours,
			VagonCount: t.VagonCount, SubGroups: subs,
		})
	}

	// ── gantt → flows ──
	var gantt []oldGanttDay
	if err := json.Unmarshal([]byte(ganttJS), &gantt); err != nil {
		return nil, fmt.Errorf("gantt: %w", err)
	}
	var remainders map[string]struct {
		Value int `json:"value"`
	}
	if remJS != "" {
		if err := json.Unmarshal([]byte(remJS), &remainders); err != nil {
			return nil, fmt.Errorf("initial_remainders: %w", err)
		}
	}
	type flowKey struct{ port, cargo string }
	flowDays := map[flowKey][]service.GtDayDTO{}
	var order []flowKey
	for _, d := range gantt {
		key := flowKey{d.Port, d.CargoType}
		if _, seen := flowDays[key]; !seen {
			order = append(order, key)
		}
		ops := make([]service.GtOperationDTO, 0, len(d.Operations))
		for _, op := range d.Operations {
			so, err1 := ltReq(op.StartTime)
			eo, err2 := ltReq(op.EndTime)
			sj, err3 := ltReq(op.StartRailwayTime)
			ej, err4 := ltReq(op.EndRailwayTime)
			if err := firstErr(err1, err2, err3, err4); err != nil {
				return nil, fmt.Errorf("gantt %s/%s %s: операция %q: %w", d.Port, d.CargoType, d.Date, op.TrainIndex, err)
			}
			ops = append(ops, service.GtOperationDTO{
				TrainIndex: op.TrainIndex, TrainName: op.TrainName,
				StationNach: op.StationNach, IndexMain: op.IndexMain, GruzpolS: op.GruzpolS,
				OrigIndex: op.OriginalTrainIndex,
				StartCalc: so, EndCalc: eo, StartJd: sj, EndJd: ej,
				Wagons: op.Wagons, TotalWagons: op.TotalWagons, Color: op.Color,
				IsRemainder: op.IsRemainder, IsCarriedOver: op.IsCarriedOver,
				IsPartial: op.IsPartialRemainder, WaitMin: op.WaitTime,
				OrigArrivalJd: lt(op.OriginalArrivalTime),
			})
		}
		carried := make([]service.GtCarriedDTO, 0, len(d.CarriedOverTrains))
		for _, c := range d.CarriedOverTrains {
			carried = append(carried, service.GtCarriedDTO{Index: c.Index, Wagons: c.VagonCount})
		}
		flowDays[key] = append(flowDays[key], service.GtDayDTO{
			Date: d.Date, PlanSpeed: d.Speed, NormSpeed: d.NormSpeed,
			IncomingTotal: d.IncomingTotal, Arrival: d.Arrival,
			TotalFormation: d.TotalFormation, UsefulFormation: d.UsefulFormation,
			Unloaded: d.Unloaded, Remaining: d.Remaining, TotalWaitMin: d.TotalWaitTime,
			CarriedOver: carried, Operations: ops,
		})
	}
	flows := make([]service.GtFlowDTO, 0, len(order))
	for _, key := range order {
		cargoKey := ""
		if key.port == "ГУТ-2" {
			cargoKey = key.cargo // у АЭ/УТ-1 один поток: «ОБЩИЙ» → пустой ключ
		}
		rem := 0
		if r, ok := remainders[fmt.Sprintf("%s_%s_%s", key.port, key.cargo, startDate)]; ok {
			rem = r.Value
		}
		flows = append(flows, service.GtFlowDTO{
			Terminal: key.port, CargoKey: cargoKey,
			Label: labels[key.port+"|"+cargoKey], Color: colors[key.port],
			InitialRemainder: rem, Days: flowDays[key],
		})
	}

	// ── port_statistics → free_slots ──
	var stat []oldStatDay
	if statJS != "" {
		if err := json.Unmarshal([]byte(statJS), &stat); err != nil {
			return nil, fmt.Errorf("port_statistics: %w", err)
		}
	}
	slots := []service.GtFreeSlotDTO{}
	for _, day := range stat {
		jdDate, err := time.Parse("02.01.06", day.Date)
		if err != nil {
			return nil, fmt.Errorf("port_statistics: дата %q: %w", day.Date, err)
		}
		for _, sl := range day.UnusedSlots {
			hm, err := time.Parse("15:04", sl.Time)
			if err != nil {
				return nil, fmt.Errorf("port_statistics: нитка %q: %w", sl.Time, err)
			}
			jd := time.Date(jdDate.Year(), jdDate.Month(), jdDate.Day(), hm.Hour(), hm.Minute(), 0, 0, time.UTC)
			msk := jd
			if jd.Hour() >= 18 { // обратное «час ≥ 18 → +1 сутки»
				msk = jd.AddDate(0, 0, -1)
			}
			n := sl.Count
			if n < 1 {
				n = 1
			}
			for i := 0; i < n; i++ {
				slots = append(slots, service.GtFreeSlotDTO{
					Msk: domain.LocalTime(msk), Jd: domain.LocalTime(jd),
				})
			}
		}
	}
	sort.Slice(slots, func(i, j int) bool {
		return time.Time(slots[i].Jd).Before(time.Time(slots[j].Jd))
	})

	// ── request: минимальный синтез (архив его не рисует, аналитика не читает;
	// скорости по дням и так лежат в flows) ──
	req := service.GtSimulateRequest{
		Station: station, StartDate: startDate, Days: days,
		SpeedOverrides: map[string]map[string]int{}, Overrides: []service.GtOverride{},
	}

	return &converted{
		station:   station,
		request:   mustJSON(req),
		trains:    mustJSON(newTrains),
		flows:     mustJSON(flows),
		freeSlots: mustJSON(slots),
	}, nil
}

// lt разбирает wall-time старого формата, отбрасывая «Z» и доли секунд БЕЗ
// сдвига; пусто или мусор → nil (для optional-полей).
func lt(s string) *domain.LocalTime {
	v, err := ltReq(s)
	if err != nil {
		return nil
	}
	return &v
}

func ltReq(s string) (domain.LocalTime, error) {
	s = strings.TrimSuffix(strings.TrimSpace(s), "Z")
	if i := strings.IndexByte(s, '.'); i > 0 {
		s = s[:i]
	}
	if len(s) == 16 { // без секунд
		s += ":00"
	}
	t, err := time.Parse("2006-01-02T15:04:05", s)
	if err != nil {
		return domain.LocalTime{}, fmt.Errorf("время %q: %w", s, err)
	}
	return domain.LocalTime(t), nil
}

// normTS приводит штамп из БД к формату LocalTime (отрезает доли секунд).
func normTS(s string) string {
	s = strings.Replace(strings.TrimSpace(s), " ", "T", 1)
	if i := strings.IndexByte(s, '.'); i > 0 {
		s = s[:i]
	}
	return s
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		log.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func open(dsn string) *sql.DB {
	if pw := os.Getenv("PG_PASSWORD"); pw != "" && !strings.Contains(dsn, ":"+url.QueryEscape(pw)+"@") {
		if u, err := url.Parse(dsn); err == nil && u.User != nil {
			if _, has := u.User.Password(); !has {
				u.User = url.UserPassword(u.User.Username(), pw)
				dsn = u.String()
			}
		}
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("подключение: %v", err)
	}
	if err := db.Ping(); err != nil {
		log.Fatalf("ping: %v", err)
	}
	return db
}

// loadLines: метки линий выгрузки и цвета терминалов из справочников dpport.
func loadLines(db *sql.DB) (labels, colors map[string]string) {
	labels, colors = map[string]string{}, map[string]string{}
	rows, err := db.Query(`SELECT terminal, cargo_key, label FROM dpport.port_cargo_line WHERE kind='unload'`)
	if err != nil {
		log.Fatalf("port_cargo_line: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var t, ck, l string
		if err := rows.Scan(&t, &ck, &l); err != nil {
			log.Fatalf("port_cargo_line scan: %v", err)
		}
		labels[t+"|"+ck] = l
	}
	prows, err := db.Query(`SELECT name_s, color FROM dpport.ports`)
	if err != nil {
		log.Fatalf("ports: %v", err)
	}
	defer prows.Close()
	for prows.Next() {
		var n, c string
		if err := prows.Scan(&n, &c); err != nil {
			log.Fatalf("ports scan: %v", err)
		}
		colors[n] = c
	}
	return labels, colors
}
