package unloadsim

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Golden-тесты: фикстуры testdata/*.json сгенерированы прогоном ЭТАЛОННОГО
// TypeScript-кода gtlogic (scripts/golden/gen_gtsim_fixtures.mts). Тест строит
// потоки из входа фикстуры, гоняет Go-движок и сравнивает результат с выводом
// эталона поле в поле (времена — до миллисекунды, простои — до 1e-6 минуты).

type fxSubGroup struct {
	Key         string `json:"key"`
	Naznach     string `json:"naznach"`
	CargoGroup  string `json:"cargo_group"`
	VagonCount  int    `json:"vagon_count"`
	Color       string `json:"color"`
	StationNach string `json:"station_nach"`
	IndexMain   string `json:"index_main"`
	GruzpolS    string `json:"gruzpol_s"`
}

type fxTrain struct {
	Index     string       `json:"index"`
	ProgJd    string       `json:"prog_jd"`
	SubGroups []fxSubGroup `json:"sub_groups"`
}

type fxSpeed struct {
	Default     int            `json:"default"`
	UserDefined map[string]int `json:"userDefined"`
}

type fxInput struct {
	Port              string             `json:"port"`
	StartDate         string             `json:"start_date"`
	Days              int                `json:"days"`
	Speeds            map[string]fxSpeed `json:"speeds"`
	Norms             map[string]int     `json:"norms"`
	InitialRemainders map[string]int     `json:"initial_remainders"`
	Trains            []fxTrain          `json:"trains"`
}

type fxOp struct {
	TrainIndex    string  `json:"train_index"`
	TrainName     string  `json:"train_name"`
	StationNach   string  `json:"station_nach"`
	IndexMain     string  `json:"index_main"`
	StartCalc     string  `json:"start_calc"`
	EndCalc       string  `json:"end_calc"`
	StartJd       string  `json:"start_jd"`
	EndJd         string  `json:"end_jd"`
	Wagons        int     `json:"wagons"`
	TotalWagons   int     `json:"total_wagons"`
	Color         string  `json:"color"`
	IsRemainder   bool    `json:"is_remainder"`
	IsCarriedOver bool    `json:"is_carried_over"`
	IsPartial     bool    `json:"is_partial"`
	WaitMin       float64 `json:"wait_min"`
	OrigArrivalJd string  `json:"original_arrival_jd"`
}

type fxCarried struct {
	Index       string `json:"index"`
	Wagons      int    `json:"wagons"`
	SubgroupKey string `json:"subgroup_key"`
}

type fxDay struct {
	Date            string      `json:"date"`
	CargoType       string      `json:"cargo_type"`
	Speed           int         `json:"speed"`
	NormSpeed       int         `json:"norm_speed"`
	IncomingTotal   int         `json:"incoming_total"`
	Arrival         int         `json:"arrival"`
	TotalFormation  int         `json:"total_formation"`
	UsefulFormation int         `json:"useful_formation"`
	Unloaded        int         `json:"unloaded"`
	Remaining       int         `json:"remaining"`
	TotalWaitMin    float64     `json:"total_wait_min"`
	RemainderOut    int         `json:"remainder_out"`
	RemainderOutIdx string      `json:"remainder_out_index"`
	CarriedOver     []fxCarried `json:"carried_over"`
	Operations      []fxOp      `json:"operations"`
}

type fixture struct {
	Comment      string  `json:"comment"`
	Input        fxInput `json:"input"`
	ExpectedDays []fxDay `json:"expected_days"`
}

const maxTrainWagonsFixture = 63 // эталон gtlogic (constants.ts MAX_TRAIN_WAGONS)

func TestGoldenFixtures(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("фикстуры не найдены: %v", err)
	}
	for _, file := range files {
		file := file
		t.Run(filepath.Base(file), func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			var fx fixture
			if err := json.Unmarshal(raw, &fx); err != nil {
				t.Fatal(err)
			}
			got := runFixture(t, fx.Input)
			if len(got) != len(fx.ExpectedDays) {
				t.Fatalf("дней: получено %d, ожидалось %d", len(got), len(fx.ExpectedDays))
			}
			for i, want := range fx.ExpectedDays {
				compareDay(t, i, got[i], want)
			}
		})
	}
}

// runFixture повторяет группировку и порядок эталона (useUnloading.ts):
// поезд дублируется по подгруппам своего порта; ГУТ-2 — три потока в порядке
// УГОЛЬ/МЕТАЛЛ/ЧУГУН; вывод — по дням, внутри дня — по потокам.
func runFixture(t *testing.T, in fxInput) []DayResult {
	t.Helper()
	start, err := time.Parse("2006-01-02", in.StartDate)
	if err != nil {
		t.Fatal(err)
	}

	cargos := []string{"ОБЩИЙ"}
	if in.Port == "ГУТ-2" {
		cargos = []string{"УГОЛЬ", "МЕТАЛЛ", "ЧУГУН"}
	}

	byCargo := map[string][]Train{}
	for _, ft := range in.Trains {
		jd, err := time.Parse("2006-01-02T15:04:05", ft.ProgJd)
		if err != nil {
			t.Fatalf("prog_jd %q: %v", ft.ProgJd, err)
		}
		for _, sg := range ft.SubGroups {
			if sg.Naznach != in.Port {
				continue
			}
			cargo := "ОБЩИЙ"
			if in.Port == "ГУТ-2" {
				switch sg.CargoGroup {
				case "УГОЛЬ", "МЕТАЛЛ", "ЧУГУН":
					cargo = sg.CargoGroup
				default:
					continue
				}
			}
			byCargo[cargo] = append(byCargo[cargo], Train{
				Index:    ft.Index,
				CalcTime: RailwayToCalc(jd),
				OrigJd:   jd,
				Sub: SubGroup{
					Key: sg.Key, VagonCount: sg.VagonCount, Color: sg.Color,
					StationNach: sg.StationNach, IndexMain: sg.IndexMain, GruzpolS: sg.GruzpolS,
				},
			})
		}
	}

	perFlow := map[string][]DayResult{}
	for _, cargo := range cargos {
		key := fmt.Sprintf("%s_%s", in.Port, cargo)
		sp := in.Speeds[key]
		flow := Flow{
			Port: in.Port, Cargo: cargo,
			Trains:           byCargo[cargo],
			InitialRemainder: in.InitialRemainders[key],
			PlanSpeed:        sp.Default,
			PlanOverrides:    sp.UserDefined,
			NormSpeed:        in.Norms[key],
			MaxTrainWagons:   maxTrainWagonsFixture,
		}
		perFlow[cargo] = SimulateFlow(flow, start, in.Days)
	}

	var out []DayResult
	for d := 0; d < in.Days; d++ {
		for _, cargo := range cargos {
			out = append(out, perFlow[cargo][d])
		}
	}
	return out
}

func compareDay(t *testing.T, i int, got DayResult, want fxDay) {
	t.Helper()
	pfx := fmt.Sprintf("день %d (%s %s)", i, want.Date, want.CargoType)
	eqStr(t, pfx+" date", got.Date, want.Date)
	eqStr(t, pfx+" cargo", got.Cargo, want.CargoType)
	eqInt(t, pfx+" speed", got.PlanSpeed, want.Speed)
	eqInt(t, pfx+" norm_speed", got.NormSpeed, want.NormSpeed)
	eqInt(t, pfx+" incoming_total", got.IncomingTotal, want.IncomingTotal)
	eqInt(t, pfx+" arrival", got.Arrival, want.Arrival)
	eqInt(t, pfx+" total_formation", got.TotalFormation, want.TotalFormation)
	eqInt(t, pfx+" useful_formation", got.UsefulFormation, want.UsefulFormation)
	eqInt(t, pfx+" unloaded", got.Unloaded, want.Unloaded)
	eqInt(t, pfx+" remaining", got.Remaining, want.Remaining)
	eqFloat(t, pfx+" total_wait_min", got.TotalWaitMin, want.TotalWaitMin)
	eqInt(t, pfx+" remainder_out", got.RemainderOut, want.RemainderOut)
	eqStr(t, pfx+" remainder_out_index", got.RemainderOutIdx, want.RemainderOutIdx)

	if len(got.CarriedOver) != len(want.CarriedOver) {
		t.Errorf("%s: перенесено %d поездов, ожидалось %d", pfx, len(got.CarriedOver), len(want.CarriedOver))
	} else {
		for j, w := range want.CarriedOver {
			g := got.CarriedOver[j]
			eqStr(t, fmt.Sprintf("%s carried[%d].index", pfx, j), g.Index, w.Index)
			eqInt(t, fmt.Sprintf("%s carried[%d].wagons", pfx, j), g.Sub.VagonCount, w.Wagons)
			eqStr(t, fmt.Sprintf("%s carried[%d].key", pfx, j), g.Sub.Key, w.SubgroupKey)
		}
	}

	if len(got.Operations) != len(want.Operations) {
		t.Fatalf("%s: операций %d, ожидалось %d\nполучено: %+v", pfx, len(got.Operations), len(want.Operations), opNames(got.Operations))
	}
	for j, w := range want.Operations {
		g := got.Operations[j]
		p := fmt.Sprintf("%s op[%d] (%s)", pfx, j, w.TrainName)
		eqStr(t, p+" train_index", g.TrainIndex, w.TrainIndex)
		eqStr(t, p+" train_name", g.TrainName, w.TrainName)
		eqStr(t, p+" station_nach", g.StationNach, w.StationNach)
		eqStr(t, p+" index_main", g.IndexMain, w.IndexMain)
		eqStr(t, p+" start_calc", fmtMs(g.StartCalc), w.StartCalc)
		eqStr(t, p+" end_calc", fmtMs(g.EndCalc), w.EndCalc)
		eqStr(t, p+" start_jd", fmtMs(g.StartJd), w.StartJd)
		eqStr(t, p+" end_jd", fmtMs(g.EndJd), w.EndJd)
		eqInt(t, p+" wagons", g.Wagons, w.Wagons)
		eqInt(t, p+" total_wagons", g.TotalWagons, w.TotalWagons)
		eqStr(t, p+" color", g.Color, w.Color)
		eqBool(t, p+" is_remainder", g.IsRemainder, w.IsRemainder)
		eqBool(t, p+" is_carried_over", g.IsCarriedOver, w.IsCarriedOver)
		eqBool(t, p+" is_partial", g.IsPartial, w.IsPartial)
		eqFloat(t, p+" wait_min", g.WaitMin, w.WaitMin)
		orig := ""
		if !g.OrigArrivalJd.IsZero() {
			orig = fmtMs(g.OrigArrivalJd)
		}
		eqStr(t, p+" original_arrival_jd", orig, w.OrigArrivalJd)
	}
}

// Отход от эталона (задокументирован в simulateDay): поезд с нулевой частичной
// выгрузкой (прибыл в последние минуты суток) в gtlogic попадал в перенос
// дважды. Тест закрепляет, что в Go он переносится ровно один раз.
func TestPartialZeroNotDuplicated(t *testing.T) {
	start, _ := time.Parse("2006-01-02", "2026-08-04")
	// Прибытие 23:58 расчётной шкалы: round(130/24 * ~0.03ч) = 0 вагонов.
	jd, _ := time.Parse("2006-01-02T15:04:05", "2026-08-04T17:58:00")
	flow := Flow{
		Port: "АЭ", Cargo: "ОБЩИЙ",
		Trains: []Train{{
			Index: "8650-901-9840", CalcTime: RailwayToCalc(jd), OrigJd: jd,
			Sub: SubGroup{Key: "z1", VagonCount: 50},
		}},
		PlanSpeed: 130, NormSpeed: 144, MaxTrainWagons: 63,
	}
	days := SimulateFlow(flow, start, 2)
	if n := len(days[0].CarriedOver); n != 1 {
		t.Fatalf("перенесено %d записей поезда, должно быть ровно 1 (без задвоения)", n)
	}
	if w := days[0].CarriedOver[0].Sub.VagonCount; w != 50 {
		t.Fatalf("перенесено %d вагонов, ожидалось 50", w)
	}
	if got := days[1].Unloaded; got != 50 {
		t.Fatalf("на второй день выгружено %d, ожидалось 50", got)
	}
}

func opNames(ops []Operation) []string {
	names := make([]string, len(ops))
	for i, op := range ops {
		names[i] = op.TrainName
	}
	return names
}

func fmtMs(t time.Time) string { return t.Format("2006-01-02T15:04:05.000") }

func eqStr(t *testing.T, what, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s: получено %q, ожидалось %q", what, got, want)
	}
}

func eqInt(t *testing.T, what string, got, want int) {
	t.Helper()
	if got != want {
		t.Errorf("%s: получено %d, ожидалось %d", what, got, want)
	}
}

func eqBool(t *testing.T, what string, got, want bool) {
	t.Helper()
	if got != want {
		t.Errorf("%s: получено %v, ожидалось %v", what, got, want)
	}
}

func eqFloat(t *testing.T, what string, got, want float64) {
	t.Helper()
	if math.Abs(round6(got)-want) > 1e-9 {
		t.Errorf("%s: получено %.6f, ожидалось %.6f", what, got, want)
	}
}

func round6(x float64) float64 { return math.Round(x*1e6) / 1e6 }
