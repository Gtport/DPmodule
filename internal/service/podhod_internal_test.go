package service

// Golden-тест агрегации отчёта «Подход» — страховка переноса из gtport
// (port_report_repository.go GetPortReport/buildPrimFields). Фикстура покрывает:
// порядок поездов и n по prog_msk; index_pp поверх index; выпадение записей без
// прогноза и со статусами 10/12; подгруппы в порядке появления; sprav_2 = первый
// вагон; суммы кол-ва/веса; моду date_nach; фильтр клиентов; prim_1 «был CCC»;
// prim_2 переадресации и обе формулировки перестановок; prim_4 = color; список
// клиентов терминала.

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/Gtport/DPmodule/internal/domain"
)

func podhodLT(month time.Month, day, hour, minute int) *domain.LocalTime {
	lt := domain.LocalTime(time.Date(2026, month, day, hour, minute, 0, 0, time.UTC))
	return &lt
}

func podhodInt(v int) *int             { return &v }
func podhodF64(v float64) *float64     { return &v }

// Станции терминалов: АЭ и ГУТ-2 на одной станции (перестановка), УТ-1 — на
// другой (переадресация). Как реестр ports, но без БД.
var podhodStations = map[string]string{
	"АЭ":    "98050",
	"ГУТ-2": "98050",
	"УТ-1":  "98060",
}

// podhodFixture — снимок дислокации для терминала АЭ: 4 поезда в отчёте +
// 4 записи, которые обязаны выпасть.
func podhodFixture() []domain.Dislocation {
	base := func(vagon, index string) domain.Dislocation {
		return domain.Dislocation{
			Vagon: vagon, Index: index, IndexMain: index,
			StanNazn: "МЫС АСТАФЬЕВА", GruzpolS: "АЭ", Naznach: "АЭ",
			Status: podhodInt(0),
		}
	}

	// Поезд A: две подгруппы (УГОЛЬ 2 вагона, МЕТАЛЛ 3 вагона со сменой индекса).
	r1 := base("60000001", "12340-111-98050")
	r1.ProgMsk, r1.ProgJd, r1.PlanMsk = podhodLT(7, 30, 10, 0), podhodLT(7, 30, 10, 0), podhodLT(7, 30, 9, 0)
	r1.StationOper, r1.DorogaOper, r1.OperS = "РУЖИНО", "ДВС", "ВО"
	r1.CargoS, r1.StationNach, r1.Gruzotpr = "УГОЛЬ", "ЕРУНАКОВО", "РАЗРЕЗ"
	r1.DateNach, r1.Ves = podhodLT(7, 1, 8, 0), podhodF64(69.5)
	r1.Client, r1.Sprav1, r1.Sprav3, r1.Color = "КЛЦ МАРИС", "спр1", "спр3", "yellow"

	r2 := r1
	r2.Vagon, r2.Ves = "60000002", podhodF64(70.0)

	r3 := r1
	r3.Vagon, r3.Ves = "60000003", podhodF64(60)
	r3.CargoS = "МЕТАЛЛ"
	r3.IndexMain = "12340-999-98050" // сменил назначение → prim_1 «был 999»
	r3.DateNach = podhodLT(6, 30, 12, 0)
	r3.Sprav1, r3.Sprav3, r3.Color = "", "", ""

	r4 := r3
	r4.Vagon, r4.Ves, r4.DateNach = "60000004", podhodF64(61), podhodLT(7, 1, 9, 0)
	r5 := r3
	r5.Vagon, r5.Ves, r5.DateNach = "60000005", podhodF64(62), podhodLT(7, 1, 10, 0)

	// Поезд B: самый ранний прогноз (n=1); index_pp поверх index; попадает в
	// терминал по naznach; перестановка ГУТ-2 → АЭ (одна станция); брошен.
	r6 := base("61000001", "98010-000-98050")
	r6.IndexPp, r6.IndexMain = "98010-555-98050", "98010-555-98050"
	r6.GruzpolS, r6.Naznach = "ГУТ-2", "АЭ"
	r6.ProgMsk, r6.ProgJd = podhodLT(7, 30, 8, 0), podhodLT(7, 30, 8, 0)
	r6.StationOper, r6.DorogaOper, r6.OperS = "УГОЛЬНАЯ", "ДВС", "БРОС"
	r6.CargoS, r6.StationNach, r6.Gruzotpr = "УГОЛЬ", "НЕРЮНГРИ", "ЯКУТ"
	r6.DateNach, r6.Ves = podhodLT(6, 28, 6, 0), podhodF64(68)
	r6.Client, r6.Color, r6.Status = "HORIZON COMMODITIES TRADING LIMITED", "red", podhodInt(5)

	// Поезд C: переадресация на внешний порт (ВП + pereadr_port).
	r7 := base("62000001", "11111-222-98050")
	r7.Naznach, r7.PereadrType, r7.PereadrPort = domain.NaznachExternalPort, domain.PereadrExt, "ВАНИНО"
	r7.ProgMsk, r7.ProgJd = podhodLT(7, 31, 12, 0), podhodLT(7, 31, 12, 0)
	r7.StationOper, r7.DorogaOper, r7.OperS = "ХАБАРОВСК II", "ДВС", "СЛ"
	r7.CargoS, r7.StationNach, r7.Gruzotpr = "УГОЛЬ", "МЕЖДУРЕЧЕНСК", "РАЗРЕЗ-2"
	r7.DateNach, r7.Ves = podhodLT(7, 2, 4, 0), podhodF64(65)
	r7.Client = "ЕВРАЗ ТК"

	// Поезд D: переадр на терминал другой станции; статус и дата погрузки не заданы.
	r8 := base("63000001", "22222-333-98060")
	r8.Naznach = "УТ-1"
	r8.ProgMsk, r8.ProgJd = podhodLT(8, 1, 6, 0), podhodLT(8, 1, 6, 0)
	r8.StationOper, r8.DorogaOper, r8.OperS = "СМОЛЯНИНОВО", "ДВС", "ВО"
	r8.CargoS, r8.StationNach, r8.Gruzotpr = "КОКС", "АЛТАЙСКАЯ", "КОКСОХИМ"
	r8.Ves = podhodF64(64)
	r8.Client, r8.Status, r8.DateNach = "КЛЦ МАРИС", nil, nil

	// Выпадают: без прогноза; статус 10; статус 12; чужой терминал.
	r9 := base("70000001", "33333-111-98050")
	r9.ProgMsk, r9.Client = nil, "ФАНТОМ" // клиент не должен попасть в список
	r10 := base("70000002", "33333-222-98050")
	r10.ProgMsk, r10.Status = podhodLT(7, 29, 1, 0), podhodInt(10)
	r11 := base("70000003", "33333-333-98050")
	r11.ProgMsk, r11.Status = podhodLT(7, 29, 2, 0), podhodInt(12)
	r12 := base("70000004", "44444-111-98060")
	r12.GruzpolS, r12.Naznach = "УТ-1", "УТ-1"
	r12.ProgMsk = podhodLT(7, 29, 3, 0)

	return []domain.Dislocation{r1, r2, r3, r4, r5, r6, r7, r8, r9, r10, r11, r12}
}

func TestAggregatePodhod_Golden(t *testing.T) {
	got := aggregatePodhod(podhodFixture(), "АЭ", nil, podhodStations)

	want := PodhodReport{
		Total: 4,
		Clients: []string{
			"HORIZON COMMODITIES TRADING LIMITED", "ЕВРАЗ ТК", "КЛЦ МАРИС",
		},
		Items: []PodhodItem{
			{
				N: 1, Index: "98010-555-98050", PlanMsk: nil,
				StationOper: "УГОЛЬНАЯ", DorogaOper: "ДВС", OperS: "БРОС",
				ProgMsk: podhodLT(7, 30, 8, 0),
				Subgroups: []PodhodSubgroup{{
					StationNach: "НЕРЮНГРИ", DateNach: podhodLT(6, 28, 6, 0), Gruzotpr: "ЯКУТ",
					VagonCount: 1, TotalWeight: 68, Sprav2: "61000001",
					Prim2: "Перестановка на АЭ", Prim3: "Перестановка на АЭ", Prim4: "red",
				}},
			},
			{
				N: 2, Index: "12340-111-98050", PlanMsk: podhodLT(7, 30, 9, 0),
				StationOper: "РУЖИНО", DorogaOper: "ДВС", OperS: "ВО",
				ProgMsk: podhodLT(7, 30, 10, 0),
				Subgroups: []PodhodSubgroup{
					{
						StationNach: "ЕРУНАКОВО", DateNach: podhodLT(7, 1, 8, 0), Gruzotpr: "РАЗРЕЗ",
						VagonCount: 2, TotalWeight: 139.5,
						Sprav1: "спр1", Sprav2: "60000001", Sprav3: "спр3", Prim4: "yellow",
					},
					{
						// Мода date_nach: 30.06 (первый вагон) проигрывает 01.07 (два вагона).
						StationNach: "ЕРУНАКОВО", DateNach: podhodLT(7, 1, 9, 0), Gruzotpr: "РАЗРЕЗ",
						VagonCount: 3, TotalWeight: 183, Sprav2: "60000003",
						Prim1: "был 999", Prim3: "был 999",
					},
				},
			},
			{
				N: 3, Index: "11111-222-98050", PlanMsk: nil,
				StationOper: "ХАБАРОВСК II", DorogaOper: "ДВС", OperS: "СЛ",
				ProgMsk: podhodLT(7, 31, 12, 0),
				Subgroups: []PodhodSubgroup{{
					StationNach: "МЕЖДУРЕЧЕНСК", DateNach: podhodLT(7, 2, 4, 0), Gruzotpr: "РАЗРЕЗ-2",
					VagonCount: 1, TotalWeight: 65, Sprav2: "62000001",
					Prim2: "Переадресация на ВАНИНО", Prim3: "Переадресация на ВАНИНО",
				}},
			},
			{
				N: 4, Index: "22222-333-98060", PlanMsk: nil,
				StationOper: "СМОЛЯНИНОВО", DorogaOper: "ДВС", OperS: "ВО",
				ProgMsk: podhodLT(8, 1, 6, 0),
				Subgroups: []PodhodSubgroup{{
					StationNach: "АЛТАЙСКАЯ", DateNach: nil, Gruzotpr: "КОКСОХИМ",
					VagonCount: 1, TotalWeight: 64, Sprav2: "63000001",
					Prim2: "Переадр с АЭ на УТ-1", Prim3: "Переадр с АЭ на УТ-1",
				}},
			},
		},
	}

	assert.Equal(t, want, got)
}

// Клиентский фильтр (пресет «Марис»): чужие клиенты выпадают из отчёта, но
// полный список клиентов терминала остаётся — он питает мультиселект.
func TestAggregatePodhod_ClientFilter(t *testing.T) {
	clients := podhodClientSet("КЛЦ МАРИС|HORIZON COMMODITIES TRADING LIMITED")
	got := aggregatePodhod(podhodFixture(), "АЭ", clients, podhodStations)

	assert.Equal(t, 3, got.Total) // поезд C (ЕВРАЗ ТК) выпал
	indexes := make([]string, 0, len(got.Items))
	for _, it := range got.Items {
		indexes = append(indexes, it.Index)
	}
	assert.Equal(t, []string{"98010-555-98050", "12340-111-98050", "22222-333-98060"}, indexes)
	assert.Len(t, got.Clients, 3, "список клиентов — до фильтра")
}

func TestPodhodClientSet(t *testing.T) {
	assert.Nil(t, podhodClientSet(""))
	assert.Nil(t, podhodClientSet(" | "))
	assert.Equal(t, map[string]bool{"A": true, "B": true}, podhodClientSet("A| B "))
}
