package domain

import (
	"testing"
	"time"
)

func gu2bD(y int, m time.Month, d, h, min int) LocalTime {
	return LocalTime(time.Date(y, m, d, h, min, 0, 0, time.UTC))
}

func gu2bT(y int, m time.Month, d, h, min int) *LocalTime {
	v := gu2bD(y, m, d, h, min)
	return &v
}

func gu2bNotif(id int64, t *LocalTime, vagons ...string) GU2BNotification {
	n := GU2BNotification{
		NotificationID: id, State: "Подписан", DateCreate: t,
		OrgOKPO: "1126022", StationName: "МЫС АСТАФЬЕВА",
	}
	for i, v := range vagons {
		n.Cars = append(n.Cars, GU2BCar{CarOrder: i + 1, Vagon: v, OperationShort: "ВЫГР", OperationName: "Выгрузка"})
	}
	return n
}

func resolveGUT2(string, string) string { return "ГУТ-2" }

// Вечернее прибытие хранится ЖД-штампом «дата +1»: штамп 24.08 23:28 означает
// факт 23.08 23:28, и уведомление утра 24.08 обязано найти этот рейс. Сравнение
// по хранимому значению его бы потеряло — ровно дефект, стоивший памяткам 22%
// строк (боевой замер 17.08.2026).
func TestApplyGU2B_ЗамокПоМСКФакту(t *testing.T) {
	trips := []GU2BTrip{{ID: "r1", Vagon: "52962537", DatePrib: gu2bT(2026, 8, 24, 23, 28)}}
	n := gu2bNotif(1, gu2bT(2026, 8, 24, 9, 0), "52962537")

	applies, skips := ApplyGU2B([]GU2BNotification{n}, trips, nil, resolveGUT2, 0)
	if len(skips) != 0 || len(applies) != 1 {
		t.Fatalf("applies=%v skips=%v, ждали одну запись без пропусков", applies, skips)
	}
	f := applies[0].Fields
	if f["place_vigr"] != "ГУТ-2" {
		t.Errorf("place_vigr = %v, ждали ГУТ-2", f["place_vigr"])
	}
	if got := f["date_vigr"].(*LocalTime); !got.Time().Equal(gu2bD(2026, 8, 24, 9, 0).Time()) {
		t.Errorf("date_vigr = %v", got)
	}
	// 09:00 < 18 → ЖД-сутки совпадают с календарными.
	if got := f["date_vigr_d"].(*LocalTime); !got.Time().Equal(gu2bD(2026, 8, 24, 0, 0).Time()) {
		t.Errorf("date_vigr_d = %v", got)
	}
}

// Вечерняя выгрузка уходит в следующие ЖД-сутки: «час ≥ 18 → дата +1».
func TestApplyGU2B_ВечерняяВыгрузкаЖДСутки(t *testing.T) {
	trips := []GU2BTrip{{ID: "r1", Vagon: "1", DatePrib: gu2bT(2026, 8, 20, 10, 0)}}
	n := gu2bNotif(1, gu2bT(2026, 8, 24, 19, 30), "1")

	applies, _ := ApplyGU2B([]GU2BNotification{n}, trips, nil, nil, 0)
	if len(applies) != 1 {
		t.Fatalf("applies = %v", applies)
	}
	if got := applies[0].Fields["date_vigr_d"].(*LocalTime); !got.Time().Equal(gu2bD(2026, 8, 25, 0, 0).Time()) {
		t.Errorf("date_vigr_d = %v, ждали 25.08 (час ≥ 18 → +1 сутки)", got)
	}
	if _, ok := applies[0].Fields["place_vigr"]; ok {
		t.Error("резолвера нет — place_vigr трогать нельзя")
	}
}

// Снимковый путь пишет веху первым (момент выбытия), уведомление уточняет её,
// когда несёт то же или более раннее время (допуск 2 мин на усечение секунд).
// Более позднее уведомление веху НЕ перетирает — как у АСУ.
func TestApplyGU2B_ПерезаписьИДопуск(t *testing.T) {
	prib := gu2bT(2026, 8, 20, 10, 0)
	mk := func(vigr *LocalTime) []GU2BTrip {
		return []GU2BTrip{{ID: "r1", Vagon: "1", DatePrib: prib, DateVigr: vigr, PlaceVigr: "АЭ"}}
	}

	// t раньше вехи → уточнение.
	applies, skips := ApplyGU2B([]GU2BNotification{gu2bNotif(1, gu2bT(2026, 8, 24, 9, 0), "1")},
		mk(gu2bT(2026, 8, 24, 12, 0)), nil, resolveGUT2, 0)
	if len(applies) != 1 || len(skips) != 0 {
		t.Fatalf("раннее уведомление должно уточнять веху: applies=%v skips=%v", applies, skips)
	}
	if applies[0].Fields["place_vigr"] != "ГУТ-2" {
		t.Errorf("перестановка не записана: %v", applies[0].Fields)
	}

	// t в пределах допуска (веха + 2 мин) → тоже уточнение.
	applies, skips = ApplyGU2B([]GU2BNotification{gu2bNotif(1, gu2bT(2026, 8, 24, 12, 1), "1")},
		mk(gu2bT(2026, 8, 24, 12, 0)), nil, resolveGUT2, 0)
	if len(applies) != 1 || len(skips) != 0 {
		t.Fatalf("допуск 2 мин не сработал: applies=%v skips=%v", applies, skips)
	}

	// t позже допуска → скип later.
	applies, skips = ApplyGU2B([]GU2BNotification{gu2bNotif(1, gu2bT(2026, 8, 24, 12, 3), "1")},
		mk(gu2bT(2026, 8, 24, 12, 0)), nil, resolveGUT2, 0)
	if len(applies) != 0 || len(skips) != 1 || skips[0].Reason != GU2BSkipLater {
		t.Fatalf("позднее уведомление должно скипаться: applies=%v skips=%v", applies, skips)
	}
}

// Пары уведомлений ближе 72 ч — один факт выгрузки, побеждает ПЕРВОЕ (правило
// подтверждено эталоном АСУ: веха 60383536 = №4870, повтор №4887 проигнорирован).
// Повтор ТОГО ЖЕ документа дублем не считается — идемпотентная перезапись.
func TestApplyGU2B_Дедуп72Часа(t *testing.T) {
	trips := []GU2BTrip{{ID: "r1", Vagon: "1", DatePrib: gu2bT(2026, 8, 20, 10, 0)}}
	first := gu2bNotif(100, gu2bT(2026, 8, 24, 9, 0), "1")
	repeat := gu2bNotif(101, gu2bT(2026, 8, 26, 8, 0), "1") // 47 ч позже — дубль

	applies, skips := ApplyGU2B([]GU2BNotification{first, repeat}, trips, nil, nil, 0)
	if len(applies) != 1 || len(skips) != 1 || skips[0].Reason != GU2BSkipDup72h {
		t.Fatalf("дубль <72ч должен скипаться: applies=%v skips=%v", applies, skips)
	}
	if got := applies[0].Fields["date_vigr"].(*LocalTime); !got.Time().Equal(gu2bD(2026, 8, 24, 9, 0).Time()) {
		t.Errorf("должно победить ПЕРВОЕ уведомление, а веха = %v", got)
	}

	// Дедуп против накопленных прошлых тиков (prior из БД).
	prior := []GU2BPriorEvent{{Vagon: "1", NotificationID: 100, T: gu2bD(2026, 8, 24, 9, 0)}}
	applies, skips = ApplyGU2B([]GU2BNotification{repeat}, trips, prior, nil, 0)
	if len(applies) != 0 || len(skips) != 1 || skips[0].Reason != GU2BSkipDup72h {
		t.Fatalf("дубль против prior не пойман: applies=%v skips=%v", applies, skips)
	}

	// Тот же NotificationID из prior — не дубль (повторный приход документа).
	sameDoc := gu2bNotif(100, gu2bT(2026, 8, 24, 9, 0), "1")
	applies, _ = ApplyGU2B([]GU2BNotification{sameDoc}, trips, prior, nil, 0)
	if len(applies) != 1 {
		t.Fatalf("повтор того же документа должен применяться идемпотентно: %v", applies)
	}
}

// Заготовки/испорченные и не-выгрузочные операции вехи не пишут; рейс без
// подходящего прибытия и «недоехавший» — no_trip.
func TestApplyGU2B_Фильтры(t *testing.T) {
	trips := []GU2BTrip{
		{ID: "r1", Vagon: "1", DatePrib: gu2bT(2026, 8, 20, 10, 0)},
		{ID: "r2", Vagon: "2", DatePrib: gu2bT(2026, 8, 25, 10, 0)},               // прибыл ПОЗЖЕ выгрузки
		{ID: "r3", Vagon: "3", DatePrib: gu2bT(2026, 8, 20, 10, 0), NotArrived: true}, // «недоехавший»
	}

	draft := gu2bNotif(1, gu2bT(2026, 8, 24, 9, 0), "1")
	draft.State = "Заготовка"
	bop := gu2bNotif(2, gu2bT(2026, 8, 24, 9, 0), "1")
	bop.Cars[0].OperationShort = "БОП"
	bop.Cars[0].OperationName = "БОП"
	late := gu2bNotif(3, gu2bT(2026, 8, 24, 9, 0), "2")
	gone := gu2bNotif(4, gu2bT(2026, 8, 24, 9, 0), "3")

	applies, skips := ApplyGU2B([]GU2BNotification{draft, bop, late, gone}, trips, nil, nil, 0)
	if len(applies) != 0 {
		t.Fatalf("ни одна запись не должна пройти: %v", applies)
	}
	got := map[string]int{}
	for _, s := range skips {
		got[s.Reason]++
	}
	want := map[string]int{GU2BSkipNotSigned: 1, GU2BSkipNotUnload: 1, GU2BSkipNoTrip: 2}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("skips[%s] = %d, ждали %d (всё: %v)", k, got[k], v, got)
		}
	}
}
