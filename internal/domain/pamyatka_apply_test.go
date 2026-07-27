package domain

import (
	"testing"
	"time"
)

func lt(s string) *LocalTime {
	t, err := time.Parse("2006-01-02 15:04", s)
	if err != nil {
		panic(err)
	}
	return NewLocalTime(t)
}

func pam(number, oper, create, place string, vagons ...PamyatkaVagon) Pamyatka {
	return Pamyatka{
		Number:        number,
		OperationType: oper,
		DateCreate:    lt(create + " 00:00"),
		GetPlace:      place,
		Vagons:        vagons,
	}
}

func vag(v, getIn, report, getOut string) PamyatkaVagon {
	pv := PamyatkaVagon{Vagon: v, GetIn: lt(getIn)}
	if report != "" {
		pv.Report = lt(report)
	}
	if getOut != "" {
		pv.GetOut = lt(getOut)
	}
	return pv
}

// applyOne — применение пачки, от которого ждём ровно одну правку и ни одного
// пропуска (частый случай в тестах ниже).
func applyOne(t *testing.T, pamyatki []Pamyatka, trips []PamyatkaTrip) PamyatkaApply {
	t.Helper()
	applies, skips := ApplyPamyatki(pamyatki, trips)
	if len(skips) != 0 {
		t.Fatalf("ожидали применение без пропусков, получили пропуски: %+v", skips)
	}
	if len(applies) != 1 {
		t.Fatalf("ожидали ровно одну правку, получили %d: %+v", len(applies), applies)
	}
	return applies[0]
}

func wantField(t *testing.T, f map[string]any, key string, want any) {
	t.Helper()
	got, ok := f[key]
	if !ok {
		t.Fatalf("колонка %s не заполнена; есть: %v", key, keysOf(f))
	}
	switch w := want.(type) {
	case *LocalTime:
		g, ok := got.(*LocalTime)
		if !ok || !g.Time().Equal(w.Time()) {
			t.Fatalf("колонка %s = %v, ожидали %v", key, got, w)
		}
	default:
		if got != want {
			t.Fatalf("колонка %s = %v, ожидали %v", key, got, want)
		}
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Подача ложится на рейс, прибывший ДО неё, и раскладывается по колонкам
// решения владельца 28.07.2026.
func TestApplyPamyatki_PodachaFieldMapping(t *testing.T) {
	trips := []PamyatkaTrip{{ID: "t1", Vagon: "62428651", DatePrib: lt("2026-07-24 08:00")}}
	p := pam("11255", PamyatkaOperPod, "2026-07-25", "Аттис -1 путь",
		vag("62428651", "2026-07-25 00:10", "", ""))

	got := applyOne(t, []Pamyatka{p}, trips)

	if got.TripID != "t1" {
		t.Fatalf("правка ушла в рейс %s, ожидали t1", got.TripID)
	}
	wantField(t, got.Fields, "nom_gu45_pod", "11255")
	wantField(t, got.Fields, "date_gu45_pod", lt("2026-07-25 00:00"))
	wantField(t, got.Fields, "date_pod", lt("2026-07-25 00:10"))
	wantField(t, got.Fields, "place_pod", "Аттис -1 путь")
	wantField(t, got.Fields, "pamyatka_state", PamyatkaStatePod)
	if _, ok := got.Fields["date_ubor"]; ok {
		t.Fatal("подача без GET_OUT не должна писать уборку")
	}
	if _, ok := got.Fields["date_pod_gu45"]; ok {
		t.Fatal("колонка-дубль date_pod_gu45 заполняться не должна")
	}
}

// Замок по времени: у вагона два рейса, памятка обязана лечь на тот, что
// прибыл до подачи, а не на самый свежий. Это тот случай, что на боевых данных
// уводил 510 строк из 1564 на посторонний рейс.
func TestApplyPamyatki_PicksTripByArrivalNotLatest(t *testing.T) {
	trips := []PamyatkaTrip{
		{ID: "старый", Vagon: "62231725", DatePrib: lt("2026-07-02 06:00")},
		{ID: "новый", Vagon: "62231725", DatePrib: lt("2026-07-20 06:00")},
	}
	p := pam("11255", PamyatkaOperPod, "2026-07-25", "Аттис -1 путь",
		vag("62231725", "2026-07-03 10:15", "", ""))

	got := applyOne(t, []Pamyatka{p}, trips)
	if got.TripID != "старый" {
		t.Fatalf("памятка легла на рейс %s, ожидали «старый» (прибытие до подачи)", got.TripID)
	}
}

// Прибытий позже подачи быть не может: рейса нет — памятка пропускается с
// причиной, а не садится на первый попавшийся.
func TestApplyPamyatki_NoTripSkips(t *testing.T) {
	trips := []PamyatkaTrip{{ID: "t1", Vagon: "62428651", DatePrib: lt("2026-07-26 08:00")}}
	p := pam("11255", PamyatkaOperPod, "2026-07-25", "путь",
		vag("62428651", "2026-07-03 10:15", "", ""))

	applies, skips := ApplyPamyatki([]Pamyatka{p}, trips)
	if len(applies) != 0 {
		t.Fatalf("правок быть не должно, получили %+v", applies)
	}
	if len(skips) != 1 || skips[0].Reason != PamyatkaSkipNoTrip {
		t.Fatalf("ожидали один пропуск no_trip, получили %+v", skips)
	}
}

// Уборочная памятка на рейс со стадией 0 заполняет ОБА блока и ставит сразу 2:
// GET_IN в уборочной есть всегда, иначе вагон застрял бы на нуле навсегда.
func TestApplyPamyatki_UborkaOnFreshTripFillsBothBlocks(t *testing.T) {
	trips := []PamyatkaTrip{{ID: "t1", Vagon: "52957172", DatePrib: lt("2026-07-23 10:00")}}
	p := pam("11252", PamyatkaOperUbor, "2026-07-23", "1 путь - 71, 72, 73 причал (уголь)",
		vag("52957172", "2026-07-23 18:35", "2026-07-24 10:24", "2026-07-24 21:21"))

	got := applyOne(t, []Pamyatka{p}, trips)

	wantField(t, got.Fields, "date_pod", lt("2026-07-23 18:35"))
	wantField(t, got.Fields, "place_pod", "1 путь - 71, 72, 73 причал (уголь)")
	wantField(t, got.Fields, "date_vigr_gu45", lt("2026-07-24 10:24"))
	wantField(t, got.Fields, "date_ubor", lt("2026-07-24 21:21"))
	wantField(t, got.Fields, "nom_gu45_ubor", "11252")
	wantField(t, got.Fields, "pamyatka_state", PamyatkaStateUbor)
}

// Штатная цепочка: подача проставлена (стадия 1), приходит уборка — дописывает
// свой блок, блок подачи чужой памяткой не перетирается.
func TestApplyPamyatki_UborkaAfterPodachaKeepsPodacha(t *testing.T) {
	trips := []PamyatkaTrip{{
		ID: "t1", Vagon: "52957172", DatePrib: lt("2026-07-23 10:00"),
		State: PamyatkaStatePod, NomGu45Pod: "11200",
	}}
	p := pam("11252", PamyatkaOperUbor, "2026-07-25", "причал",
		vag("52957172", "2026-07-23 18:35", "", "2026-07-24 21:21"))

	got := applyOne(t, []Pamyatka{p}, trips)

	if _, ok := got.Fields["nom_gu45_pod"]; ok {
		t.Fatal("уборочная памятка не должна переписывать блок подачи на стадии 1")
	}
	wantField(t, got.Fields, "nom_gu45_ubor", "11252")
	wantField(t, got.Fields, "pamyatka_state", PamyatkaStateUbor)
}

// Повторный приход ТОЙ ЖЕ памятки (документ дозаполнили уборкой) обновляет
// строку на месте, невзирая на стадию.
func TestApplyPamyatki_SameDocumentUpdatesInPlace(t *testing.T) {
	trips := []PamyatkaTrip{{
		ID: "t1", Vagon: "62231725", DatePrib: lt("2026-07-02 06:00"),
		State: PamyatkaStatePod, NomGu45Pod: "11314",
	}}
	p := pam("11314", PamyatkaOperPod, "2026-07-26", "4 путь - 76 тыл (уголь)",
		vag("62231725", "2026-07-03 10:15", "2026-07-04 04:42", "2026-07-04 10:38"))

	got := applyOne(t, []Pamyatka{p}, trips)

	wantField(t, got.Fields, "date_vigr_gu45", lt("2026-07-04 04:42"))
	wantField(t, got.Fields, "date_ubor", lt("2026-07-04 10:38"))
	wantField(t, got.Fields, "pamyatka_state", PamyatkaStateUbor)
}

// Боевой случай вагона 62231725: две РАЗНЫЕ подачные памятки на один рейс с
// одинаковым временем подачи. Побеждает более полная (№11314 с уборкой), даже
// если первой пришла №11255.
func TestApplyPamyatki_FullerDocumentWins(t *testing.T) {
	trips := []PamyatkaTrip{{ID: "t1", Vagon: "62231725", DatePrib: lt("2026-07-02 06:00")}}
	first := pam("11255", PamyatkaOperPod, "2026-07-25", "Аттис -1 путь",
		vag("62231725", "2026-07-03 10:15", "", ""))
	second := pam("11314", PamyatkaOperPod, "2026-07-26", "4 путь - 76 тыл (уголь)",
		vag("62231725", "2026-07-03 10:15", "2026-07-04 04:42", "2026-07-04 10:38"))

	got := applyOne(t, []Pamyatka{first, second}, trips)

	wantField(t, got.Fields, "nom_gu45_pod", "11314")
	wantField(t, got.Fields, "place_pod", "4 путь - 76 тыл (уголь)")
	wantField(t, got.Fields, "date_ubor", lt("2026-07-04 10:38"))
	wantField(t, got.Fields, "pamyatka_state", PamyatkaStateUbor)
}

// Порядок в пачке значения не имеет: сортировка по дате составления делает
// результат одинаковым, как бы провайдер ни отдал памятки.
func TestApplyPamyatki_OrderIndependent(t *testing.T) {
	trips := []PamyatkaTrip{{ID: "t1", Vagon: "62231725", DatePrib: lt("2026-07-02 06:00")}}
	first := pam("11255", PamyatkaOperPod, "2026-07-25", "Аттис -1 путь",
		vag("62231725", "2026-07-03 10:15", "", ""))
	second := pam("11314", PamyatkaOperPod, "2026-07-26", "4 путь - 76 тыл (уголь)",
		vag("62231725", "2026-07-03 10:15", "2026-07-04 04:42", "2026-07-04 10:38"))

	forward := applyOne(t, []Pamyatka{first, second}, trips)
	backward := applyOne(t, []Pamyatka{second, first}, trips)

	if forward.Fields["nom_gu45_pod"] != backward.Fields["nom_gu45_pod"] {
		t.Fatalf("результат зависит от порядка пачки: %v против %v",
			forward.Fields["nom_gu45_pod"], backward.Fields["nom_gu45_pod"])
	}
}

// Менее полная памятка на занятый рейс отбрасывается с причиной.
func TestApplyPamyatki_NotFullerSkips(t *testing.T) {
	trips := []PamyatkaTrip{{
		ID: "t1", Vagon: "62231725", DatePrib: lt("2026-07-02 06:00"),
		State: PamyatkaStateUbor, NomGu45Pod: "11314", NomGu45Ubor: "11314",
		DateUbor: lt("2026-07-04 10:38"),
	}}
	p := pam("11255", PamyatkaOperPod, "2026-07-25", "Аттис -1 путь",
		vag("62231725", "2026-07-03 10:15", "", ""))

	applies, skips := ApplyPamyatki([]Pamyatka{p}, trips)
	if len(applies) != 0 {
		t.Fatalf("правок быть не должно, получили %+v", applies)
	}
	if len(skips) != 1 || skips[0].Reason != PamyatkaSkipStaleState {
		t.Fatalf("ожидали пропуск stale_state, получили %+v", skips)
	}
}

// Вагон чужого клиента: рейса в истории нет — молча пропускаем с причиной.
// На боевой выборке таких 241 строка из 1564.
func TestApplyPamyatki_UnknownVagonSkips(t *testing.T) {
	p := pam("11255", PamyatkaOperPod, "2026-07-25", "путь",
		vag("99999999", "2026-07-25 00:10", "", ""))

	applies, skips := ApplyPamyatki([]Pamyatka{p}, nil)
	if len(applies) != 0 {
		t.Fatalf("правок быть не должно, получили %+v", applies)
	}
	if len(skips) != 1 || skips[0].Reason != PamyatkaSkipNoTrip {
		t.Fatalf("ожидали пропуск no_trip, получили %+v", skips)
	}
}

// Две памятки на разные вагоны одного рейса-однофамильца не сливаются, а правки
// нескольких памяток одного рейса приходят одной записью.
func TestApplyPamyatki_MergesPerTrip(t *testing.T) {
	trips := []PamyatkaTrip{
		{ID: "t1", Vagon: "111", DatePrib: lt("2026-07-01 00:00")},
		{ID: "t2", Vagon: "222", DatePrib: lt("2026-07-01 00:00")},
	}
	podacha := pam("1", PamyatkaOperPod, "2026-07-02", "путь A",
		vag("111", "2026-07-02 10:00", "", ""), vag("222", "2026-07-02 10:00", "", ""))
	uborka := pam("2", PamyatkaOperUbor, "2026-07-03", "путь A",
		vag("111", "2026-07-02 10:00", "", "2026-07-03 12:00"))

	applies, skips := ApplyPamyatki([]Pamyatka{podacha, uborka}, trips)
	if len(skips) != 0 {
		t.Fatalf("пропусков быть не должно: %+v", skips)
	}
	if len(applies) != 2 {
		t.Fatalf("ожидали по одной записи на рейс (2), получили %d", len(applies))
	}
	for _, a := range applies {
		switch a.TripID {
		case "t1":
			wantField(t, a.Fields, "nom_gu45_pod", "1")
			wantField(t, a.Fields, "nom_gu45_ubor", "2")
			wantField(t, a.Fields, "pamyatka_state", PamyatkaStateUbor)
		case "t2":
			wantField(t, a.Fields, "pamyatka_state", PamyatkaStatePod)
			if _, ok := a.Fields["date_ubor"]; ok {
				t.Fatal("вагон 222 уборки не получал")
			}
		}
	}
}

// Неизвестный OPERATION_TYPE — падать нельзя, но и писать вслепую тоже.
func TestApplyPamyatki_UnknownOperationSkips(t *testing.T) {
	trips := []PamyatkaTrip{{ID: "t1", Vagon: "111", DatePrib: lt("2026-07-01 00:00")}}
	p := pam("1", "перестановку", "2026-07-02", "путь",
		vag("111", "2026-07-02 10:00", "", ""))

	applies, skips := ApplyPamyatki([]Pamyatka{p}, trips)
	if len(applies) != 0 {
		t.Fatalf("правок быть не должно, получили %+v", applies)
	}
	if len(skips) != 1 || skips[0].Reason != PamyatkaSkipUnknownOp {
		t.Fatalf("ожидали пропуск unknown_op, получили %+v", skips)
	}
}
