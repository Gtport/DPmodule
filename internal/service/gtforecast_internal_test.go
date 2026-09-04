package service

import (
	"testing"
	"time"

	"github.com/Gtport/DPmodule/internal/domain"
)

// Группировка снимка в очередь GT: индекс из плановой нитки, подсчёт вагонов по
// подгруппам, нумерация Б/И, фильтры (статус ≥ 9, без прогноза, чужой терминал),
// признак универсальности по паре станций.
func TestGtTransitTrains(t *testing.T) {
	lt := func(s string) *domain.LocalTime {
		tt, err := time.Parse("2006-01-02T15:04:05", s)
		if err != nil {
			t.Fatal(err)
		}
		v := domain.LocalTime(tt)
		return &v
	}
	ip := func(v int) *int { return &v }

	prog := lt("2026-08-04T10:30:00")
	rows := []domain.Dislocation{
		// Поезд с плановой ниткой: три вагона, две подгруппы (разные станции отправления).
		{Index: "8650-101-9840", IndexPp: "8650-555-9840", StationOper: "ТАЙШЕТ", Status: ip(2),
			ProgJd: prog, RasstStanNazn: ip(500), Naznach: "АЭ", GruzpolS: "АЭ",
			StanNazn: "МЫС АСТАФЬЕВА", StationNach: "ЕРУНАКОВО", CargoGroup: "УГОЛЬ", Color: "#7030A0",
			DorogaOper: "ВСБ", TimeOp: lt("2026-08-03T20:00:00"), Oper: "ПРИБ", ProstCh: ip(5),
			Gruzotpr: "РУК", Client: "САВИТАР"},
		{Index: "8650-101-9840", IndexPp: "8650-555-9840", StationOper: "ТАЙШЕТ", Status: ip(2),
			ProgJd: prog, RasstStanNazn: ip(500), Naznach: "АЭ", GruzpolS: "АЭ",
			StanNazn: "МЫС АСТАФЬЕВА", StationNach: "ЕРУНАКОВО", CargoGroup: "УГОЛЬ", Color: "#7030A0",
			DorogaOper: "ВСБ", TimeOp: lt("2026-08-03T21:30:00"), Oper: "ОТПР", ProstCh: ip(3), ProstMin: ip(30),
			Gruzotpr: "РУК", Client: "САВИТАР"},
		{Index: "8650-101-9840", IndexPp: "8650-555-9840", StationOper: "ТАЙШЕТ", Status: ip(2),
			ProgJd: prog, RasstStanNazn: ip(500), Naznach: "ГУТ-2", GruzpolS: "АЭ",
			StanNazn: "МЫС АСТАФЬЕВА", StationNach: "УЛАК", CargoGroup: "МЕТАЛЛ", Color: "#FF5722",
			DorogaOper: "ВСБ", Gruzotpr: "ЕВРАЗ"},
		// Без индекса → Б/И 1.
		{Index: "", StationOper: "ХАБАРОВСК", Status: ip(4),
			ProgJd: lt("2026-08-05T02:00:00"), RasstStanNazn: ip(900), Naznach: "АЭ", GruzpolS: "АЭ",
			StanNazn: "МЫС АСТАФЬЕВА", StationNach: "НЕРЮНГРИ", CargoGroup: "УГОЛЬ"},
		// Отсекаются: прибывший, без прогноза, чужой терминал.
		{Index: "X1", StationOper: "П", Status: ip(10), ProgJd: prog, Naznach: "АЭ"},
		{Index: "X2", StationOper: "П", Status: ip(2), ProgJd: nil, Naznach: "АЭ"},
		{Index: "X3", StationOper: "П", Status: ip(2), ProgJd: prog, Naznach: "УТ-1"},
	}
	known := map[string]bool{"АЭ": true, "ГУТ-2": true}
	univers := map[string]bool{"МЫС АСТАФЬЕВА|УЛАК": true}

	got := gtTransitTrains(rows, known, univers)
	if len(got) != 2 {
		t.Fatalf("поездов %d, ожидалось 2", len(got))
	}

	tr := got[0]
	if tr.Index != "8650-555-9840" {
		t.Errorf("индекс поезда %q, ожидалась плановая нитка 8650-555-9840", tr.Index)
	}
	if tr.VagonCount != 3 || len(tr.SubGroups) != 2 {
		t.Errorf("вагонов %d (ждали 3), подгрупп %d (ждали 2)", tr.VagonCount, len(tr.SubGroups))
	}
	for _, sg := range tr.SubGroups {
		switch sg.Naznach {
		case "АЭ":
			if sg.VagonCount != 2 || sg.IsUniversal {
				t.Errorf("подгруппа АЭ: вагонов %d (ждали 2), универс %v (ждали false)", sg.VagonCount, sg.IsUniversal)
			}
		case "ГУТ-2":
			if sg.VagonCount != 1 || !sg.IsUniversal {
				t.Errorf("подгруппа ГУТ-2: вагонов %d (ждали 1), универс %v (ждали true — УЛАК)", sg.VagonCount, sg.IsUniversal)
			}
		}
	}
	if got[1].Index != "Б/И 1" {
		t.Errorf("безындексный поезд получил %q, ожидалось «Б/И 1»", got[1].Index)
	}

	// Дислокация на момент расчёта (аналитика «обстановка»): дорога и станция
	// назначения — с первого вагона; операция — самая поздняя по вагонам;
	// простой — минимальный (3 ч 30 мин, а не 5 ч); адрес и клиент — в подгруппе.
	if tr.DorogaOper != "ВСБ" || tr.StanNazn != "МЫС АСТАФЬЕВА" {
		t.Errorf("дорога %q / назначение %q, ожидались ВСБ / МЫС АСТАФЬЕВА", tr.DorogaOper, tr.StanNazn)
	}
	if tr.RasstStanNazn == nil || *tr.RasstStanNazn != 500 {
		t.Errorf("км до назначения %v, ожидалось 500", tr.RasstStanNazn)
	}
	if tr.TimeOp == nil || time.Time(*tr.TimeOp) != time.Time(*lt("2026-08-03T21:30:00")) || tr.Oper != "ОТПР" {
		t.Errorf("операция %v %q, ожидалась самая поздняя 21:30 ОТПР", tr.TimeOp, tr.Oper)
	}
	if tr.IdleHours == nil || *tr.IdleHours != 3.5 {
		t.Errorf("простой %v ч, ожидалось минимальное 3.5", tr.IdleHours)
	}
	for _, sg := range tr.SubGroups {
		switch sg.Naznach {
		case "АЭ":
			if sg.GruzpolS != "АЭ" || sg.Gruzotpr != "РУК" || sg.Client != "САВИТАР" {
				t.Errorf("подгруппа АЭ: адрес %q отправитель %q клиент %q", sg.GruzpolS, sg.Gruzotpr, sg.Client)
			}
		case "ГУТ-2":
			if sg.GruzpolS != "АЭ" { // адрес АЭ, назначение ГУТ-2 — перестановка видна из снапшота
				t.Errorf("подгруппа ГУТ-2: адрес %q, ожидался АЭ (перестановка)", sg.GruzpolS)
			}
		}
	}
	if got[1].IdleHours != nil || got[1].TimeOp != nil {
		t.Errorf("поезд без полей простоя/операции должен оставить их пустыми: %v %v", got[1].IdleHours, got[1].TimeOp)
	}
}

// Автоснапшот: сутки — текущие ЖД-сутки (час ≥ 18 → завтра) без времени; крон
// не перезаписывает ручной снапшот (в том числе с пустым kind до миграции
// 000064), но перезаписывает свой авто-снапшот.
func TestGtAutoSnapshotRules(t *testing.T) {
	d := gtAutoPlanDate(time.Date(2026, 9, 5, 6, 30, 0, 0, time.UTC))
	if d != time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC) {
		t.Errorf("06:30 → %v, ожидались сутки 05.09", d)
	}
	d = gtAutoPlanDate(time.Date(2026, 9, 5, 18, 5, 0, 0, time.UTC))
	if d != time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC) {
		t.Errorf("18:05 → %v, ожидались ЖД-сутки 06.09", d)
	}
	if !gtAutoShouldSave(nil) {
		t.Error("нет снапшота — надо сохранять")
	}
	if gtAutoShouldSave(&domain.GtSnapshot{Kind: domain.GtSnapshotManual}) || gtAutoShouldSave(&domain.GtSnapshot{}) {
		t.Error("ручной снапшот (и с пустым kind) крон трогать не должен")
	}
	if !gtAutoShouldSave(&domain.GtSnapshot{Kind: domain.GtSnapshotAuto}) {
		t.Error("свой авто-снапшот крон перезаписывает")
	}
}

// Извлечение поездов потока: фильтр по терминалу и группе груза; пустой
// cargo_key линии собирает все грузы терминала; поезд дублируется по подгруппам.
func TestGtFlowTrains(t *testing.T) {
	prog := domain.LocalTime(time.Date(2026, 8, 4, 10, 30, 0, 0, time.UTC))
	trains := []GtTrainDTO{{
		Index: "8650-555-9840", ProgJd: &prog,
		SubGroups: []GtSubGroupDTO{
			{Key: "a", Naznach: "АЭ", CargoGroup: "УГОЛЬ", VagonCount: 40},
			{Key: "b", Naznach: "ГУТ-2", CargoGroup: "МЕТАЛЛ", VagonCount: 22},
			{Key: "c", Naznach: "ГУТ-2", CargoGroup: "УГОЛЬ", VagonCount: 18},
		},
	}}

	ae := gtFlowTrains(trains, "АЭ", "")
	if len(ae) != 1 || ae[0].Sub.VagonCount != 40 {
		t.Errorf("поток АЭ: %d записей (ждали 1 с 40 ваг)", len(ae))
	}
	gutMetal := gtFlowTrains(trains, "ГУТ-2", "МЕТАЛЛ")
	if len(gutMetal) != 1 || gutMetal[0].Sub.VagonCount != 22 {
		t.Errorf("поток ГУТ-2/МЕТАЛЛ: %d записей (ждали 1 с 22 ваг)", len(gutMetal))
	}
	if got := gtFlowTrains(trains, "УТ-1", ""); len(got) != 0 {
		t.Errorf("поток УТ-1 должен быть пуст, получено %d", len(got))
	}
	// Расчётная шкала: ЖД 10:30 → 16:30 тех же суток.
	want := time.Date(2026, 8, 4, 16, 30, 0, 0, time.UTC)
	if !ae[0].CalcTime.Equal(want) {
		t.Errorf("calcTime %v, ожидалось %v", ae[0].CalcTime, want)
	}
}

// What-if правки: throw/restore/assign/move и падения на некорректном входе.
func TestApplyGtOverrides(t *testing.T) {
	lt := func(s string) *domain.LocalTime {
		tt, _ := time.Parse("2006-01-02T15:04:05", s)
		v := domain.LocalTime(tt)
		return &v
	}
	mk := func() []GtTrainDTO {
		return []GtTrainDTO{
			{Index: "8650-111-9840", Status: "2", PlanJd: lt("2026-08-05T20:00:00"), PlanMsk: lt("2026-08-04T20:00:00"),
				SubGroups: []GtSubGroupDTO{{Naznach: "ГУТ-2", CargoGroup: "УГОЛЬ", IsUniversal: true, VagonCount: 40},
					{Naznach: "ГУТ-2", CargoGroup: "МЕТАЛЛ", VagonCount: 20}}},
			{Index: "прибыл", IsArrived: true},
		}
	}

	// throw: план снят, статус 5, delay = сутки×24.
	trains := mk()
	delays, err := applyGtOverrides(trains, []GtOverride{{Index: "8650-111-9840", Action: "throw", DelayDays: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if trains[0].Status != "5" || trains[0].PlanJd != nil || trains[0].PlanMsk != nil {
		t.Errorf("throw: статус %q, план %v/%v — ожидалось 5 и nil", trains[0].Status, trains[0].PlanJd, trains[0].PlanMsk)
	}
	if d := delays["8650-111-9840"]; d != 48*time.Hour {
		t.Errorf("throw: задержка %v, ожидалось 48ч", d)
	}

	// restore c остаточной задержкой.
	trains = mk()
	trains[0].Status = "5"
	delays, err = applyGtOverrides(trains, []GtOverride{{Index: "8650-111-9840", Action: "restore", DelayHours: 6}})
	if err != nil {
		t.Fatal(err)
	}
	if trains[0].Status != "0" || delays["8650-111-9840"] != 6*time.Hour {
		t.Errorf("restore: статус %q, задержка %v", trains[0].Status, delays["8650-111-9840"])
	}

	// assign: план = слот, ЖД-производная по правилу ≥18 → +сутки.
	trains = mk()
	_, err = applyGtOverrides(trains, []GtOverride{{Index: "8650-111-9840", Action: "assign", Slot: lt("2026-08-06T21:00:00")}})
	if err != nil {
		t.Fatal(err)
	}
	if got := time.Time(*trains[0].PlanJd).Format("02.01 15:04"); got != "07.08 21:00" {
		t.Errorf("assign: plan_jd %s, ожидалось 07.08 21:00 (час ≥ 18 → +сутки)", got)
	}

	// move: только универсальные подгруппы меняют терминал.
	trains = mk()
	_, err = applyGtOverrides(trains, []GtOverride{{Index: "8650-111-9840", Action: "move", MoveTo: "АЭ"}})
	if err != nil {
		t.Fatal(err)
	}
	if trains[0].SubGroups[0].Naznach != "АЭ" || trains[0].SubGroups[1].Naznach != "ГУТ-2" {
		t.Errorf("move: подгруппы %q/%q — универсальная должна уехать, обычная остаться",
			trains[0].SubGroups[0].Naznach, trains[0].SubGroups[1].Naznach)
	}

	// Ошибки: неизвестный поезд, правка прибывшего, throw без суток.
	for _, bad := range []GtOverride{
		{Index: "нет такого", Action: "throw", DelayDays: 1},
		{Index: "прибыл", Action: "throw", DelayDays: 1},
		{Index: "8650-111-9840", Action: "throw"},
		{Index: "8650-111-9840", Action: "танцевать"},
	} {
		if _, err := applyGtOverrides(mk(), []GtOverride{bad}); err == nil {
			t.Errorf("правка %+v должна падать с ошибкой", bad)
		}
	}
}

// Прибывший поезд, чьи вехи прибытия проставлены порциями (боевой случай
// 05.08.2026, поезд 128: 21:00/21:30/22:20), — ОДНА группа по ЖД-суткам,
// прибытие = самая ранняя веха.
func TestGtArrivedTrains_PortionsMerge(t *testing.T) {
	lt := func(s string) *domain.LocalTime {
		tt, _ := time.Parse("2006-01-02T15:04:05", s)
		v := domain.LocalTime(tt)
		return &v
	}
	day := lt("2026-08-05T00:00:00")
	mk := func(prib string, n int) []domain.VagonHistory {
		out := make([]domain.VagonHistory, n)
		for i := range out {
			out[i] = domain.VagonHistory{
				IndexPp: "8650-128-9857", Naznach: "АЭ", StationNach: "ПЕТРОВСКИЙ ЗАВОД",
				CargoGroup: "УГОЛЬ", DatePrib: lt(prib), DatePribD: day,
			}
		}
		return out
	}
	rows := append(append(mk("2026-08-05T21:30:00", 20), mk("2026-08-05T21:00:00", 22)...),
		mk("2026-08-05T22:20:00", 21)...)

	got := gtArrivedTrains(rows, map[string]bool{"АЭ": true})
	if len(got) != 1 {
		t.Fatalf("групп %d, ожидалась 1 (порции вех не должны разваливать поезд)", len(got))
	}
	if got[0].VagonCount != 63 {
		t.Errorf("вагонов %d, ожидалось 63", got[0].VagonCount)
	}
	if p := time.Time(*got[0].ProgJd).Format("15:04"); p != "21:00" {
		t.Errorf("прибытие группы %s, ожидалась самая ранняя веха 21:00", p)
	}
}

// Свободные нитки: слоты расписания минус нитки плана по ЖД-суткам листа
// (дата PlanJd), занятость — нормализацией эталона «нитка → ближайший ещё
// свободный слот» (план верстают руками, время нитки отклоняется от канона;
// замечания владельца 07.08.2026 — свежий план на ЖД 07.08 давал «нитки за
// вчера», а плановый поезд 21:42 не гасил свой слот 21:00); плюс отсечка
// горизонтом прогноза.
func TestFreeSlotsInHorizon(t *testing.T) {
	lt := func(s string) *domain.LocalTime {
		tt, err := time.Parse("2006-01-02T15:04:05", s)
		if err != nil {
			t.Fatal(err)
		}
		v := domain.LocalTime(tt)
		return &v
	}
	check := func(name string, got []GtFreeSlotDTO, want ...string) {
		t.Helper()
		var have []string
		for _, s := range got {
			have = append(have, time.Time(s.Msk).Format("2006-01-02T15:04:05"))
		}
		if len(have) != len(want) {
			t.Fatalf("%s: слоты %v, ожидалось %v", name, have, want)
		}
		for i := range want {
			if have[i] != want[i] {
				t.Errorf("%s: слот %d = %s, ожидался %s", name, i, have[i], want[i])
			}
		}
	}
	start, _ := time.Parse("2006-01-02", "2026-08-07") // горизонт: расчётные 07.08–08.08
	slots := []domain.NitkaSlot{{Hour: 9, Minute: 0}, {Hour: 17, Minute: 30}, {Hour: 21, Minute: 0}}

	// План на ЖД 07.08: нитка 20:50 съедает ближайший слот 21:00, нитка 21:42 —
	// следующий по близости 17:30 (повтор у занятого слота вытесняет на
	// соседний, как в эталоне); «Остаток на 18:00» слотов не трогает.
	// Свободным остаётся утренний 09:00 (физически 07.08 — день ЖД-суток).
	nitki := []domain.PlanNitka{
		{PlanJd: lt("2026-08-07T21:42:00")},
		{PlanJd: lt("2026-08-07T20:50:00")},
		{IsOstatok: true, PlanJd: lt("2026-08-07T09:00:00")},
	}
	check("нормализация", freeSlotsInHorizon(slots, nitki, start, 2), "2026-08-07T09:00:00")

	// Одна утренняя нитка: занят 09:00, свободные 17:30 и вечерний 21:00 —
	// физически ВЕЧЕР 06.08 (ЖД-сутки лежат на двух календарных днях),
	// ЖД-подпись вечернего — свои же ЖД-сутки 07.08.
	morning := []domain.PlanNitka{{PlanJd: lt("2026-08-07T09:05:00")}}
	got := freeSlotsInHorizon(slots, morning, start, 2)
	check("вечерний слот", got, "2026-08-06T21:00:00", "2026-08-07T17:30:00")
	if jd := time.Time(got[0].Jd).Format("2006-01-02T15:04"); jd != "2026-08-07T21:00" {
		t.Errorf("ЖД вечернего слота %s, ожидалось 2026-08-07T21:00", jd)
	}

	// Залежавшийся план (ЖД 06.08) при старте 07.08 — все слоты за горизонтом.
	stale := []domain.PlanNitka{{PlanJd: lt("2026-08-06T05:00:00")}}
	check("за горизонтом", freeSlotsInHorizon(slots, stale, start, 2))
}
