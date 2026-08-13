package service

// Тесты НМТП-отчёта: нормализатор марки (реальные написания freight_exact_name
// из боевых данных) и чистая агрегация buildNmtpReport (специфичность правил,
// «прочее», секции, брошенные, пометки перестановок, формула прогноза).

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/domain"
)

var nmtpTestMarks = []string{
	"Ж+ГЖ-ОТД", "ГЖ-ОТД", "Ж-ОТД", "ГЖ+Ж", "ГЖБС", "ДОМСШ", "АОМСШ", "ТОМСШ",
	"АМСШ", "ОС+К", "ГЖО", "ГЖ", "КС", "Ж", "Г", "Д", "Т", "К",
}

func TestMarkNormalizer_BoevyeNapisaniya(t *testing.T) {
	n := NewMarkNormalizer(nmtpTestMarks)
	cases := []struct {
		freight, sms, want string
	}{
		// Реальные написания из freight_exact_name (снимок 29.07.2026).
		{"КОНЦЕНТРАТ УГОЛЬНЫЙ Ж", "КОНЦ", "Ж"},
		{"КОНЦЕНТРАТ УГОЛЬНЫЙ ГЖ+Ж", "КОНЦ", "ГЖ+Ж"},
		{"КОНЦЕНТРАТ УГОЛЬНЫЙ. МАРКА ГЖ+Ж.", "КОНЦ", "ГЖ+Ж"},
		{"КОНЦЕНТРАТ УГОЛЬНЫЙ.МАРКА ГЖ+Ж", "КОНЦ", "ГЖ+Ж"},
		{"КОНЦЕНТРАТ УГОЛЬНЫЙ ГЖ", "КОНЦ", "ГЖ"},
		{"КОНЦЕНТРАТ УГОЛЬНЫЙ, марка \"ГЖ\"", "КОНЦ", "ГЖ"},
		{"КОНЦЕНТРАТ УГОЛЬНЫЙ Г", "КОНЦ", "Г"},
		{"КОНЦЕНТРАТ УГОЛЬНЫЙ ОС+К", "КОНЦ", "ОС+К"},
		{"ШИХТА УГОЛЬНАЯ ОС+К", "ШИХТА", "ОС+К"},
		{"УГОЛЬ КАМЕННЫЙ МАРКИ Д (0-50)", "Д", "Д"},
		{"УГОЛЬ КАМЕННЫЙ МАРКИ Д ДОМСШ 0-50", "Д", "ДОМСШ"},
		{"УГОЛЬ КАМЕННЫЙ МАРКИ Г-ГАЗОВЫЙ (0-50)", "Г", "Г"},
		{"УГОЛЬ КАМЕННЫЙ МАРКИ Г-ГАЗОВЫЙ ГЖО (0-20)", "Г", "ГЖО"},
		{"УГОЛЬ КАМЕННЫЙ МАРКИ Г-ГАЗОВЫЙ. Марка ГЖО (0-20)", "Г", "ГЖО"},
		{"КОНЦЕНТРАТ УГОЛЬНЫЙ Ж (0-50)", "КОНЦ", "Ж"},
		// Марки в тексте нет — фолбэк на метку словаря cargo.
		{"КОНЦЕНТРАТ УГОЛЬНЫЙ", "КОНЦ", "КОНЦ"},
		{"", "КОНЦ", "КОНЦ"},
		// Металл: продукцию даёт cargo_sms (кодом ЕТСНГ), в имени марок нет.
		{"ЗАГОТОВКА СТАЛЬНАЯ", "ЗАГ", "ЗАГ"},
		// «Г» внутри «Г-ГАЗОВЫЙ» не считается словом (граница — часть марки).
		{"КОНЦЕНТРАТ Г-ГАЗОВЫЙ", "КОНЦ", "КОНЦ"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, n.Mark(c.freight, c.sms), "freight=%q", c.freight)
	}
}

func nmtpVagon(vagon, idx, client, station, freight, sms string, npp int, ves float64) domain.Dislocation {
	n := npp
	v := ves
	prog := nmtpLT(2026, 7, 30, 6, 0)
	return domain.Dislocation{
		Vagon: vagon, IndexPp: idx, Client: client, StationNach: station,
		FreightExactName: freight, CargoSms: sms, NppVag: &n, Ves: &v,
		GruzpolS: "ГУТ-2", Naznach: "ГУТ-2",
		StationOper: "ТАЛДАН", DorogaOper: "ЗАБ",
		ProgJd: &prog, DateNach: nmtpLTP(2026, 7, 20),
	}
}

func nmtpLT(y, m, d, h, min int) domain.LocalTime {
	return domain.LocalTime(time.Date(y, time.Month(m), d, h, min, 0, 0, time.UTC))
}

func nmtpLTP(y, m, d int) *domain.LocalTime {
	v := nmtpLT(y, m, d, 0, 0)
	return &v
}

func nmtpTestCols() []domain.NmtpColumn {
	return []domain.NmtpColumn{
		{GroupLabel: "ЗСМК", MarkLabel: "СЛЯБЫ", MatchClients: "ЕВРАЗ ТК", MatchStations: "НОВОКУЗНЕЦК-СЕВЕРНЫЙ", MatchMarks: "СЛЯБЫ"},
		{GroupLabel: "ЗСМК", MarkLabel: "ПРОКАТ", MatchClients: "ЕВРАЗ ТК", MatchStations: "НОВОКУЗНЕЦК-СЕВЕРНЫЙ"},
		{GroupLabel: "САВИТАР", StationLabel: "МЕЖ.", MarkLabel: "ОС+К", MatchClients: "САВИТАР РЕЙЛ ООО", MatchStations: "МЕЖДУРЕЧЕНСК", MatchMarks: "ОС+К"},
		{GroupLabel: "САВИТАР", StationLabel: "МЕЖ.", MarkLabel: "ГЖ", MatchClients: "САВИТАР РЕЙЛ ООО", MatchStations: "МЕЖДУРЕЧЕНСК"},
		{GroupLabel: "КЛЦ МАРИС", StationLabel: "ЧЕЛУТАЙ", MarkLabel: "ДОМСШ", MatchClients: "КЛЦ МАРИС", MatchStations: "ЧЕЛУТАЙ"},
	}
}

func TestBuildNmtpReport(t *testing.T) {
	sl := nmtpVagon("60000001", "1111-111-1111", "ЕВРАЗ ТК", "НОВОКУЗНЕЦК-СЕВЕРНЫЙ", "СЛЯБЫ (ЗАГОТОВКИ СТАЛЬНЫЕ)", "СЛЯБЫ", 1, 70)
	kat := nmtpVagon("60000002", "1111-111-1111", "ЕВРАЗ ТК", "НОВОКУЗНЕЦК-СЕВЕРНЫЙ", "КАТАНКА СТАЛЬНАЯ", "КАТ", 2, 70)
	shx := nmtpVagon("60000003", "2222-222-2222", "САВИТАР РЕЙЛ ООО", "МЕЖДУРЕЧЕНСК", "ШИХТА УГОЛЬНАЯ ОС+К", "ШИХТА", 1, 69)
	kon := nmtpVagon("60000004", "2222-222-2222", "САВИТАР РЕЙЛ ООО", "МЕЖДУРЕЧЕНСК", "КОНЦЕНТРАТ УГОЛЬНЫЙ", "КОНЦ", 2, 69)
	// Чужое правило — «прочее».
	alien := nmtpVagon("60000005", "2222-222-2222", "НЕИЗВЕСТНЫЙ КЛИЕНТ", "ТАКСИМО", "УГОЛЬ БУРЫЙ", "Б", 3, 69)

	// Перестановка «С АЭ»: получатель АЭ, назначение ГУТ-2.
	swap := nmtpVagon("60000006", "3333-333-3333", "КЛЦ МАРИС", "ЧЕЛУТАЙ", "УГОЛЬ КАМЕННЫЙ МАРКИ Д", "Д", 1, 69)
	swap.GruzpolS = "АЭ"
	swap.StationOper, swap.DorogaOper = "ЛЕНА", "ВСБ"

	// Брошенный поезд (статус 5) на КРС.
	bros := nmtpVagon("60000007", "4444-444-4444", "КЛЦ МАРИС", "ЧЕЛУТАЙ", "УГОЛЬ КАМЕННЫЙ МАРКИ Д", "Д", 1, 69)
	five := 5
	bros.Status = &five
	bros.StationOper, bros.DorogaOper = "МАНА", "КРС"

	// На станции терминала (верхняя секция).
	near := nmtpVagon("60000008", "5555-555-5555", "КЛЦ МАРИС", "ЧЕЛУТАЙ", "УГОЛЬ КАМЕННЫЙ МАРКИ Д", "Д", 1, 69)
	near.StationOper, near.DorogaOper = "МЫС АСТАФЬЕВА", "ДВС"

	// Прибывший и чужой терминал — выпадают.
	arrived := nmtpVagon("60000009", "6666-666-6666", "КЛЦ МАРИС", "ЧЕЛУТАЙ", "", "Д", 1, 69)
	ten := 10
	arrived.Status = &ten
	foreign := nmtpVagon("60000010", "7777-777-7777", "КЛЦ МАРИС", "ЧЕЛУТАЙ", "", "Д", 1, 69)
	foreign.GruzpolS, foreign.Naznach = "УТ-1", "УТ-1"

	records := []domain.Dislocation{sl, kat, shx, kon, alien, swap, bros, near, arrived, foreign}
	brosDate := nmtpLTP(2026, 7, 25)
	rep, _, _ := buildNmtpReport(records, nmtpTestCols(), nmtpTestMarks, "ГУТ-2",
		[]string{"МЫС АСТАФЬЕВА", "НАХОДКА"}, map[string]*domain.LocalTime{"4444-444-4444": brosDate}, false, nil)

	// «прочее» появилось (alien), специфичность: СЛЯБЫ не съедены ПРОКАТОМ.
	assert.True(t, rep.HasOther)
	assert.Equal(t, []int{1, 1, 1, 1, 3, 1}, rep.ColCounts) // 5 колонок + прочее

	// Секции: МЫС АСТАФЬЕВА (near) → НАХОДКА → дороги.
	require.GreaterOrEqual(t, len(rep.Sections), 7)
	assert.Equal(t, "МЫС АСТАФЬЕВА", rep.Sections[0].Label)
	assert.Equal(t, 1, rep.Sections[0].Total)
	assert.True(t, rep.Sections[0].Near)
	assert.Equal(t, "НАХОДКА", rep.Sections[1].Label)
	assert.Equal(t, "ДАЛЬНЕВОСТОЧНАЯ ЖД", rep.Sections[2].Label)

	// ЗАБ: два поезда (метал + савитар), ВСБ: перестановка с пометкой.
	var zab, vsb, krs domain.NmtpSection
	for _, s := range rep.Sections {
		switch s.Label {
		case "ЗАБАЙКАЛЬСКАЯ ЖД":
			zab = s
		case "ВОСТОЧНО-СИБИРСКАЯ ЖД":
			vsb = s
		case "КРАСНОЯРСКАЯ ЖД":
			krs = s
		}
	}
	assert.Len(t, zab.Rows, 2)
	assert.Equal(t, 5, zab.Total) // 2 метал + 3 савитар (вкл. «прочее»)
	require.Len(t, vsb.Rows, 1)
	assert.Equal(t, "С АЭ", vsb.Rows[0].Note)
	assert.Empty(t, krs.Rows) // брошенный ушёл в секцию брошенных

	// Брошенные: КРС с датой из bros; в активных его нет.
	var krsAb domain.NmtpSection
	for _, s := range rep.Abandoned {
		if s.Label == "КРАСНОЯРСКАЯ ЖД" {
			krsAb = s
		}
	}
	require.Len(t, krsAb.Rows, 1)
	assert.Equal(t, brosDate, krsAb.Rows[0].DateBros)

	// Вагон для контроля — головной (npp=1).
	assert.Equal(t, "60000001", zab.Rows[0].ControlVagon)

	// Счётчики: все поезда фикстуры короче nmtpMinTrainVagons — поездами не
	// считаются (правило владельца 30.07.2026), хотя строки и вагоны на месте.
	// Ближние = МЫС(1) + ДВС(0) + ЗАБ(5) + ВСБ(1) = 7 → 1.0.
	assert.Equal(t, 0, rep.TrainsActive)
	assert.Equal(t, 0, rep.TrainsAbandoned)
	assert.InDelta(t, 1.0, rep.UnloadForecast, 0.001)

	// Тоннаж: 8 вагонов всего, тыс. тонн.
	assert.Equal(t, 8, rep.TotalVagons)
	assert.InDelta(t, (70+70+69*6)/1000.0, rep.TotalTons, 0.001)

	// Свод по клиентам: группы колонок с ненулевым тоннажом + ПРОЧЕЕ.
	groups := map[string]bool{}
	for _, ct := range rep.ClientTons {
		groups[ct.Client] = true
	}
	assert.True(t, groups["ЗСМК"] && groups["САВИТАР"] && groups["КЛЦ МАРИС"] && groups["ПРОЧЕЕ"])
}

// Пустая раскладка nmtp_column — не ошибка (решение владельца 13.08.2026,
// отчёт «Подход груза» доступен любому клиенту): форма строится с одной
// грузовой колонкой «прочее», куда падает весь груз.
func TestBuildNmtpReport_NoColumns(t *testing.T) {
	v1 := nmtpVagon("60000021", "1111-111-1111", "ЕВРАЗ ТК", "НОВОКУЗНЕЦК-СЕВЕРНЫЙ", "СЛЯБЫ", "СЛЯБЫ", 1, 70)
	v2 := nmtpVagon("60000022", "1111-111-1111", "ЕВРАЗ ТК", "НОВОКУЗНЕЦК-СЕВЕРНЫЙ", "КАТАНКА", "КАТ", 2, 70)

	rep, _, _ := buildNmtpReport([]domain.Dislocation{v1, v2}, nil, nmtpTestMarks, "ГУТ-2",
		[]string{"МЫС АСТАФЬЕВА", "НАХОДКА"}, nil, false, nil)

	// НЕ nil, а именно []: null в JSON ронял рендер модалки (@for по columns) —
	// пустой «Подход груза» АНБ, разбор 14.08.2026.
	assert.NotNil(t, rep.Columns)
	assert.Empty(t, rep.Columns)
	assert.True(t, rep.HasOther)
	assert.Equal(t, []int{2}, rep.ColCounts) // единственная колонка — «прочее»
	assert.Equal(t, 2, rep.TotalVagons)
	require.Len(t, rep.ClientTons, 1)
	assert.Equal(t, "ПРОЧЕЕ", rep.ClientTons[0].Client)
}

// Безиндексные поезда («Б/И») не слипаются: ключ строки — индекс + станция +
// прогноз, иначе все Б/И подхода сливались в одну строку (баг, найден сверкой
// с выгрузкой gtport по АЭ 30.07.2026: 5 поездов Б/И → одна строка на 285 ваг).
func TestBuildNmtpReport_BezindeksnyeNeSlipayutsya(t *testing.T) {
	a1 := nmtpVagon("60000011", "", "КЛЦ МАРИС", "ЧЕЛУТАЙ", "УГОЛЬ КАМЕННЫЙ МАРКИ Д", "Д", 1, 69)
	a1.Index = "Б/И"
	a1.StationOper = "ПЕТРОВСКИЙ ЗАВОД"
	a2 := nmtpVagon("60000012", "", "КЛЦ МАРИС", "ЧЕЛУТАЙ", "УГОЛЬ КАМЕННЫЙ МАРКИ Д", "Д", 2, 69)
	a2.Index = "Б/И"
	a2.StationOper = "ПЕТРОВСКИЙ ЗАВОД"
	b := nmtpVagon("60000013", "", "КЛЦ МАРИС", "ЧЕЛУТАЙ", "УГОЛЬ КАМЕННЫЙ МАРКИ Д", "Д", 1, 69)
	b.Index = "Б/И"
	b.StationOper = "СУХОВСКАЯ"
	// Та же станция, что у a1/a2, но другой прогноз — тоже отдельный поезд.
	c := nmtpVagon("60000014", "", "КЛЦ МАРИС", "ЧЕЛУТАЙ", "УГОЛЬ КАМЕННЫЙ МАРКИ Д", "Д", 1, 69)
	c.Index = "Б/И"
	c.StationOper = "ПЕТРОВСКИЙ ЗАВОД"
	prog := nmtpLT(2026, 7, 30, 9, 30)
	c.ProgJd = &prog

	rep, _, _ := buildNmtpReport([]domain.Dislocation{a1, a2, b, c}, nmtpTestCols(), nmtpTestMarks,
		"ГУТ-2", []string{"МЫС АСТАФЬЕВА", "НАХОДКА"}, nil, false, nil)

	var rows []domain.NmtpTrainRow
	for _, s := range rep.Sections {
		rows = append(rows, s.Rows...)
	}
	require.Len(t, rows, 3)
	totals := map[string]int{}
	for _, r := range rows {
		assert.Equal(t, "Б/И", r.Index)
		totals[r.StationOper+"|"+r.Prog.String()] += r.Total
	}
	assert.Equal(t, 2, totals["ПЕТРОВСКИЙ ЗАВОД|2026-07-30T06:00:00"])
	assert.Equal(t, 1, totals["СУХОВСКАЯ|2026-07-30T06:00:00"])
	assert.Equal(t, 1, totals["ПЕТРОВСКИЙ ЗАВОД|2026-07-30T09:30:00"])
}

// Правила владельца 30.07.2026: короткий состав (<20 ваг.) — не поезд в
// счётчиках; «ожид. прибытие» — только плановым (есть plan_jd); режим
// naznachOnly («скрыть перестановки») отбирает строго по назначению.
func TestBuildNmtpReport_PorogPlanRezhim(t *testing.T) {
	// Полный поезд: 20 вагонов, один из них в плане подвода → плановый.
	var recs []domain.Dislocation
	for i := 0; i < 20; i++ {
		v := nmtpVagon(fmt.Sprintf("610000%02d", i), "1111-111-1111", "КЛЦ МАРИС", "ЧЕЛУТАЙ",
			"УГОЛЬ КАМЕННЫЙ МАРКИ Д", "Д", i+1, 69)
		recs = append(recs, v)
	}
	plan := nmtpLT(2026, 8, 1, 9, 0)
	recs[0].PlanJd = &plan

	// Короткий бесплановый (19 вагонов) — строка есть, поездом не считается.
	for i := 0; i < 19; i++ {
		recs = append(recs, nmtpVagon(fmt.Sprintf("620000%02d", i), "2222-222-2222", "КЛЦ МАРИС", "ЧЕЛУТАЙ",
			"УГОЛЬ КАМЕННЫЙ МАРКИ Д", "Д", i+1, 69))
	}

	// Перестановка «НА АЭ» (уходит от нас): в режиме по умолчанию видна, в
	// naznachOnly — нет. Индекс менялся → в примечании ещё «был NNN» (gtport prim1).
	away := nmtpVagon("63000001", "3333-333-3333", "КЛЦ МАРИС", "ЧЕЛУТАЙ", "УГОЛЬ КАМЕННЫЙ МАРКИ Д", "Д", 1, 69)
	away.GruzpolS, away.Naznach = "ГУТ-2", "АЭ"
	away.IndexMain = "9999-182-9999"
	recs = append(recs, away)

	def, _, _ := buildNmtpReport(recs, nmtpTestCols(), nmtpTestMarks, "ГУТ-2",
		[]string{"МЫС АСТАФЬЕВА", "НАХОДКА"}, nil, false, nil)
	assert.Equal(t, 1, def.TrainsActive) // только полный поезд
	assert.Equal(t, 40, def.TotalVagons) // 20 + 19 + перестановка

	var full, short, moved *domain.NmtpTrainRow
	for si := range def.Sections {
		for ri := range def.Sections[si].Rows {
			r := &def.Sections[si].Rows[ri]
			switch r.Index {
			case "1111-111-1111":
				full = r
			case "2222-222-2222":
				short = r
			case "3333-333-3333":
				moved = r
			}
		}
	}
	require.NotNil(t, full)
	require.NotNil(t, short)
	require.NotNil(t, moved)
	assert.True(t, full.Planned)
	assert.False(t, short.Planned)
	assert.Equal(t, "был 182, НА АЭ", moved.Note)

	nazn, _, _ := buildNmtpReport(recs, nmtpTestCols(), nmtpTestMarks, "ГУТ-2",
		[]string{"МЫС АСТАФЬЕВА", "НАХОДКА"}, nil, true, nil)
	assert.Equal(t, 39, nazn.TotalVagons) // перестановки «НА АЭ» нет
	for _, s := range nazn.Sections {
		for _, r := range s.Rows {
			assert.NotEqual(t, "3333-333-3333", r.Index)
		}
	}
}

// Ручная привязка «вагон → колонка» (указание грузовладельца) сильнее правил;
// привязка старого рейса (дата приёма позже привязки) протухает и возвращается
// в stale; привязки вагонов вне подхода — непогашенные (не в seen).
func TestBuildNmtpReport_VagonOverride(t *testing.T) {
	v := nmtpVagon("60000021", "1111-111-1111", "КЛЦ МАРИС", "ЧЕЛУТАЙ", "УГОЛЬ КАМЕННЫЙ МАРКИ Д", "Д", 1, 69)
	// Тот же поезд, но вагон принят к перевозке ПОСЛЕ назначения привязки —
	// это уже новый рейс, привязка обязана протухнуть и лечь по правилам.
	fresh := nmtpVagon("60000022", "1111-111-1111", "КЛЦ МАРИС", "ЧЕЛУТАЙ", "УГОЛЬ КАМЕННЫЙ МАРКИ Д", "Д", 2, 69)
	fresh.DateNach = nmtpLTP(2026, 7, 29)

	bindTime := nmtpLT(2026, 7, 25, 12, 0).Time() // позже DateNach v (20.07), раньше fresh (29.07)
	// По правилам вагоны легли бы в колонку 4 (КЛЦ МАРИС/ЧЕЛУТАЙ) — привязка ведёт в 0.
	overrides := map[string]nmtpOverride{
		"60000021": {col: 0, created: bindTime},
		"60000022": {col: 0, created: bindTime},
		"60999999": {col: 3, created: bindTime},
	}
	rep, seen, stale := buildNmtpReport([]domain.Dislocation{v, fresh}, nmtpTestCols(), nmtpTestMarks,
		"ГУТ-2", []string{"МЫС АСТАФЬЕВА", "НАХОДКА"}, nil, false, overrides)
	assert.Equal(t, []int{1, 0, 0, 0, 1, 0}, rep.ColCounts) // 60000021 → привязка, 60000022 → правила
	assert.True(t, seen["60000021"])
	assert.True(t, stale["60000022"])                        // новый рейс — кандидат на гашение
	assert.False(t, seen["60999999"] || stale["60999999"]) // вне подхода — гасится по отсутствию
}
