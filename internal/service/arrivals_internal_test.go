package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Gtport/DPmodule/internal/auth"
	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

func histRow(id, vagon, indexPp, indexMain, stationNach, naznach, gruzpol string, prib time.Time) domain.VagonHistory {
	return domain.VagonHistory{
		ID: id, Vagon: vagon, IndexPp: indexPp, IndexMain: indexMain,
		StationNach: stationNach, StanNazn: "МЫС АСТАФЬЕВА",
		Naznach: naznach, GruzpolS: gruzpol,
		DatePrib: domain.NewLocalTime(prib), Otkl: "+01:49",
	}
}

// Группировка прибывших: группа = index_pp+date_prib, подгруппы по
// index_main/naznach/gruzpol_s, display в формате gtport.
func TestGroupArrivals(t *testing.T) {
	prib := time.Date(2026, 7, 17, 21, 11, 0, 0, time.UTC)
	rows := []domain.VagonHistory{
		histRow("1", "V1", "9379-783-9857", "9379-783-9857", "ЧЕЛУТАЙ", "АЭ", "АЭ", prib),
		histRow("2", "V2", "9379-783-9857", "9379-783-9857", "ЧЕЛУТАЙ", "АЭ", "АЭ", prib),
		// тот же поезд, переставленная подгруппа (чужой груз ГУТ-2 → АЭ)
		histRow("3", "V3", "9379-783-9857", "9379-782-9857", "ЧЕЛУТАЙ", "АЭ", "ГУТ-2", prib),
		// другой поезд (другой index_pp)
		histRow("4", "V4", "9853-008-9856", "9853-008-9856", "СЛЯБЫ", "ГУТ-2", "ГУТ-2", prib.Add(time.Hour)),
	}

	groups := groupArrivals(rows)
	if len(groups) != 2 {
		t.Fatalf("ожидалось 2 группы, получено %d", len(groups))
	}
	g := groups[0]
	if g.IndexPp != "9379-783-9857" || g.VagonCount != 3 || len(g.SubGroups) != 2 {
		t.Fatalf("группа 1: %+v", g)
	}
	if g.Otkl != "+01:49" {
		t.Errorf("otkl должен пробрасываться как есть, получено %q", g.Otkl)
	}
	var disp []string
	for _, sg := range g.SubGroups {
		disp = append(disp, sg.Display)
	}
	want := map[string]bool{
		"(2)-783-ЧЕЛУТАЙ АЭ":         false, // родная подгруппа
		"(1)-782-ЧЕЛУТАЙ ГУТ-2 → АЭ": false, // переставленный чужой груз
	}
	for _, d := range disp {
		if _, ok := want[d]; !ok {
			t.Errorf("неожиданный display %q", d)
		}
		want[d] = true
	}
	for d, seen := range want {
		if !seen {
			t.Errorf("не найден display %q (есть: %v)", d, disp)
		}
	}
}

// Порядок поездов — по реальному (МСК) моменту прибытия, а не по ЖД-штампу:
// поезд с ЖД-временем 02.09 21:12 пришёл 01.09 вечером и должен стоять раньше
// утреннего 02.09 07:31, хотя строки из базы идут по возрастанию штампа.
func TestGroupArrivalsChronological(t *testing.T) {
	jd := func(d, h, m int) time.Time { return time.Date(2026, 9, d, h, m, 0, 0, time.UTC) }
	rows := []domain.VagonHistory{ // порядок из БД: ORDER BY date_prib, index_pp
		histRow("1", "V1", "9700-468-9845", "9700-468-9845", "МЕЖДУРЕЧЕНСК", "УТ-1", "УТ-1", jd(2, 7, 31)),
		histRow("2", "V2", "8631-903-9847", "8631-903-9847", "БАРДИНО", "УТ-1", "УТ-1", jd(2, 17, 6)),
		histRow("3", "V3", "9131-609-9861", "9131-609-9861", "УЛАК", "УТ-1", "УТ-1", jd(2, 21, 12)),      // реально 01.09 21:12
		histRow("4", "V4", "9700-470-9845", "9700-470-9845", "ПОЛОСУХИНО", "УТ-1", "УТ-1", jd(2, 22, 9)), // реально 01.09 22:09
		histRow("5", "V5", "9700-474-9845", "9700-474-9845", "УЛАК", "УТ-1", "УТ-1", jd(3, 3, 38)),
	}
	var got []string
	for _, g := range groupArrivals(rows) {
		got = append(got, g.IndexPp)
	}
	want := []string{"9131-609-9861", "9700-470-9845", "9700-468-9845", "8631-903-9847", "9700-474-9845"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("порядок поездов:\n got %v\nwant %v", got, want)
	}
}

// Дефолтный период — вчера/сегодня; кривые даты — ошибка; перепутанные — свап.
func TestArrivalsRange(t *testing.T) {
	from, to, err := arrivalsRange("", "")
	if err != nil {
		t.Fatalf("дефолт: %v", err)
	}
	if d := to.Time().Sub(from.Time()); d != 24*time.Hour {
		t.Errorf("дефолтный период должен быть сутки (вчера-сегодня), получено %v", d)
	}
	if _, _, err := arrivalsRange("2026-13-40", ""); err == nil {
		t.Error("кривая дата: ожидалась ошибка")
	}
	from, to, err = arrivalsRange("2026-07-18", "2026-07-17")
	if err != nil {
		t.Fatalf("свап: %v", err)
	}
	if !from.Time().Before(to.Time()) {
		t.Errorf("границы не свапнулись: from=%v to=%v", from, to)
	}
}

// Пересчёты правки прибытия: date_prib_d, plan_msk (час ≥18 → −сутки), otkl.
func TestArrivalUpdateFields(t *testing.T) {
	now := domain.LocalTime(time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC))
	row := domain.VagonHistory{ID: "1", PlanMsk: domain.NewLocalTime(time.Date(2026, 7, 19, 5, 0, 0, 0, time.UTC))}

	t.Run("изменение факта — otkl от существующего плана", func(t *testing.T) {
		f := arrivalUpdateFields(&row, ArrivalsUpdateRequest{
			DatePrib: domain.NewLocalTime(time.Date(2026, 7, 19, 7, 30, 0, 0, time.UTC)),
		}, now)
		if f["otkl"] != "+02:30" {
			t.Errorf("otkl = %v, ожидалось +02:30", f["otkl"])
		}
		if f["date_prib_d"].(*domain.LocalTime).Time().Hour() != 0 {
			t.Error("date_prib_d должен быть без времени")
		}
	})

	t.Run("план ЖД с часом ≥18 — plan_msk минус сутки", func(t *testing.T) {
		f := arrivalUpdateFields(&row, ArrivalsUpdateRequest{
			PlanJd: domain.NewLocalTime(time.Date(2026, 7, 19, 21, 0, 0, 0, time.UTC)),
		}, now)
		pm := f["plan_msk"].(*domain.LocalTime).Time()
		if pm.Day() != 18 || pm.Hour() != 21 {
			t.Errorf("plan_msk = %v, ожидалось 18.07 21:00", pm)
		}
	})

	t.Run("выгрузка", func(t *testing.T) {
		place := "АЭ"
		frost := 30
		f := arrivalUpdateFields(&row, ArrivalsUpdateRequest{
			DateVigr: domain.NewLocalTime(time.Date(2026, 7, 19, 9, 0, 0, 0, time.UTC)),
			PlaceVigr: &place, Frost: &frost,
		}, now)
		if f["place_vigr"] != "АЭ" || *(f["frost"].(*int)) != 30 || f["date_vigr"] == nil || f["date_vigr_d"] == nil {
			t.Errorf("поля выгрузки: %v", f)
		}
		if d := f["date_vigr_d"].(*domain.LocalTime).Time().Day(); d != 19 {
			t.Errorf("утренняя выгрузка: ЖД-сутки должны совпадать с датой, получено %d", d)
		}
	})

	t.Run("вечерняя выгрузка — ЖД-сутки следующие (час ≥ 18)", func(t *testing.T) {
		f := arrivalUpdateFields(&row, ArrivalsUpdateRequest{
			DateVigr: domain.NewLocalTime(time.Date(2026, 7, 19, 20, 39, 0, 0, time.UTC)),
		}, now)
		if d := f["date_vigr_d"].(*domain.LocalTime).Time().Day(); d != 20 {
			t.Errorf("вечерняя выгрузка 19.07 20:39: ЖД-сутки должны быть 20.07, получено %d", d)
		}
		if f["date_vigr"].(*domain.LocalTime).Time().Hour() != 20 {
			t.Error("date_vigr должен остаться фактическим временем (20:39)")
		}
	})

	// Смена назначения у ВЫГРУЖЕННОГО вагона: место выгрузки едет следом, иначе
	// вагон прибыл на один терминал, а выгружен на другом (отчёты врут).
	t.Run("смена назначения выгруженного — place_vigr следом", func(t *testing.T) {
		vigr := domain.VagonHistory{
			ID:        "2",
			DateVigr:  domain.NewLocalTime(time.Date(2026, 7, 19, 9, 0, 0, 0, time.UTC)),
			PlaceVigr: "АЭ",
		}
		f := arrivalUpdateFields(&vigr, ArrivalsUpdateRequest{Naznach: "ГУТ-2"}, now)
		if f["naznach"] != "ГУТ-2" || f["place_vigr"] != "ГУТ-2" {
			t.Errorf("ожидались naznach и place_vigr = ГУТ-2, получено: %v", f)
		}
		if f["date_vigr"] != nil || f["date_vigr_d"] != nil {
			t.Errorf("время выгрузки трогать нельзя: %v", f)
		}
	})

	t.Run("смена назначения невыгруженного — place_vigr не появляется", func(t *testing.T) {
		f := arrivalUpdateFields(&row, ArrivalsUpdateRequest{Naznach: "ГУТ-2"}, now)
		if _, ok := f["place_vigr"]; ok {
			t.Errorf("вагон не выгружен — место выгрузки ставить рано: %v", f)
		}
	})

	t.Run("выгрузка и смена назначения разом — явное место сильнее", func(t *testing.T) {
		place := "АЭ"
		f := arrivalUpdateFields(&row, ArrivalsUpdateRequest{
			DateVigr:  domain.NewLocalTime(time.Date(2026, 7, 19, 9, 0, 0, 0, time.UTC)),
			PlaceVigr: &place,
			Naznach:   "ГУТ-2",
		}, now)
		if f["place_vigr"] != "АЭ" {
			t.Errorf("явно указанное место выгрузки должно победить: %v", f)
		}
	})

	// Выгрузка без явного места + смена назначения: место берём из нового назначения.
	t.Run("выгрузка без места + смена назначения", func(t *testing.T) {
		f := arrivalUpdateFields(&row, ArrivalsUpdateRequest{
			DateVigr: domain.NewLocalTime(time.Date(2026, 7, 19, 9, 0, 0, 0, time.UTC)),
			Naznach:  "ГУТ-2",
		}, now)
		if f["place_vigr"] != "ГУТ-2" {
			t.Errorf("место выгрузки должно взяться из нового назначения: %v", f)
		}
	})
}

// Правило дат: не-администратору можно править только сегодня/вчера.
func TestCheckArrivalsEditAccess(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC))
	defer restore()

	old := domain.VagonHistory{Vagon: "V", DatePribD: domain.NewLocalTime(time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC))}
	fresh := domain.VagonHistory{Vagon: "V", DatePribD: domain.NewLocalTime(time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC))}

	disp := auth.WithClaims(context.Background(), &auth.Claims{Roles: []auth.Role{auth.RoleOperator}})
	admin := auth.WithClaims(context.Background(), &auth.Claims{Roles: []auth.Role{auth.RoleAdmin}})

	if err := checkArrivalsEditAccess(disp, []domain.VagonHistory{fresh}); err != nil {
		t.Errorf("вчера для оператора должно быть разрешено: %v", err)
	}
	if err := checkArrivalsEditAccess(disp, []domain.VagonHistory{old}); err == nil {
		t.Error("старое прибытие для оператора должно быть запрещено")
	}
	if err := checkArrivalsEditAccess(admin, []domain.VagonHistory{old}); err != nil {
		t.Errorf("администратору можно всё: %v", err)
	}
	if err := checkArrivalsEditAccess(context.Background(), []domain.VagonHistory{old}); err != nil {
		t.Errorf("без claims (auth выключен) — разрешаем: %v", err)
	}
}
