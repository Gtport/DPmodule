package domain

import (
	"sort"
	"time"
)

// Движок перезаписи вех выгрузки из уведомлений ГУ-2б (решение владельца
// 17.08.2026, правила подтверждены симуляцией на корпусе: 94.1% строк находят
// рейс, 98% пишутся, 1468 рейсов получают фактический терминал).
//
// Снимковый путь (10→12, applyUnloadOnLeave) ОСТАЁТСЯ резервом: он пишет веху
// первым, уведомление приходит позже и уточняет её — фактическим временем и,
// главное, фактическим ТЕРМИНАЛОМ: 11–15% рейсов — перестановки АЭ↔ГУТ-2, и в
// снимке у них стоит терминал назначения, а не тот, где вагон выгрузили.

// GU2BTrip — рейс из vagon_history глазами движка ГУ-2б: замок и текущие вехи.
type GU2BTrip struct {
	ID         string
	Vagon      string
	DatePrib   *LocalTime // прибытие (ЖД-штамп) — якорь замка
	DateVigr   *LocalTime // записанная веха выгрузки (МСК-факт); nil — вехи нет
	PlaceVigr  string     // записанное место выгрузки
	NotArrived bool       // «недоехавший»: рейс удалён из кандидатов — вехи не пишем
}

// GU2BPriorEvent — уже принятое ранее событие выгрузки вагона (из накопленных
// gu2b_car): против них дедупится текущая пачка, иначе повторное уведомление
// следующего тика прошло бы как новый факт.
type GU2BPriorEvent struct {
	Vagon          string
	NotificationID int64
	T              LocalTime
}

// GU2BApply / GU2BSkip — решения движка (по образцу движка памяток).
type GU2BApply struct {
	TripID string
	Fields map[string]any
}

type GU2BSkip struct {
	Vagon          string
	NotificationID int64
	Reason         string
}

// Причины пропуска (стабильные коды для журнала).
const (
	GU2BSkipNotSigned = "not_signed"    // документ не «Подписан» (заготовка/испорчен)
	GU2BSkipNotUnload = "not_unload"    // операция строки не выгрузка (БОП и т.п.)
	GU2BSkipNoTime    = "no_time"       // у уведомления нет ни создания, ни подписания
	GU2BSkipNoTrip    = "no_trip"       // нет рейса с прибытием-МСК ≤ момента выгрузки
	GU2BSkipDup72h    = "duplicate_72h" // дубль: более раннее уведомление < 72 ч уже принято
	GU2BSkipLater     = "later"         // веха уже стоит раньше — позднее уведомление не перетирает
)

// gu2bDupWindow — окно дедупликации повторных уведомлений: пары ближе 72 ч —
// один факт выгрузки (полный дубль документа, до-оформление хвоста, повтор с
// переоформленной накладной), дальше 7 суток — уже честные повторные рейсы.
// Берём ПЕРВОЕ — правило подтверждено эталоном АСУ (веха 60383536 = №4870,
// повтор №4887 проигнорирован).
const gu2bDupWindow = 72 * time.Hour

// gu2bOverwriteSlack — допуск перезаписи: веха могла быть записана с усечёнными
// секундами, поэтому «то же самое время» — это t ≤ веха + 2 мин.
const gu2bOverwriteSlack = 2 * time.Minute

// GU2BResolveTerminal — резолвер «уведомление → имя терминала» (краткое имя
// причала, как в place_vigr: АЭ/УТ-1/ГУТ-2). Терминал определяется по паре
// «ОКПО организации + станция ПО ИМЕНИ»: ОКПО консистентен на 100%, а коды
// станций документа 5-значные и с настроечными не совпадают. Пусто — не
// определить (движок тогда пишет только времена, место не трогает).
type GU2BResolveTerminal func(okpo, stationName string) string

// ApplyGU2B раскладывает пачку уведомлений по рейсам истории.
//
// Правила (решения владельца 17.08.2026):
//
//   - Берутся только ПОДПИСАННЫЕ документы и только строки с операцией выгрузки.
//   - Момент выгрузки t — создание уведомления (фолбэк — подписание).
//   - Дедуп: у вагона уже принято событие ближе 72 ч ДО t (в этой пачке или в
//     накопленных prior) — скип, «первое побеждает». Повтор ТОГО ЖЕ документа
//     (NotificationID совпал) дублем не считается: идемпотентная перезапись.
//   - Замок по вагону: ПОСЛЕДНИЙ рейс, чьё прибытие МСК-ФАКТОМ ≤ t. date_prib
//     хранится ЖД-штампом, сравнение — по arrivalFactFromJd (то же правило, что
//     у замка памяток, см. CLAUDE.md: без него 24% уведомлений не находили рейс).
//     «Недоехавшие» рейсы (not_arrived) пропускаются.
//   - Запись: веха пуста → пишем; веха стоит и t ≤ веха + 2 мин → уточняем
//     (снимковый путь пишет момент выбытия, уведомление несёт точный факт);
//     t позже — скип: повторные уведомления позже вехи игнорирует и АСУ.
//   - date_vigr_d — ЖД-сутки от t («час ≥ cutoff → дата +1»), считаем сами.
//   - place_vigr — фактический терминал по резолверу; не определился — место
//     не трогаем (времени это не мешает).
//
// Порядок применения — по возрастанию t (при равных — по NotificationID):
// более раннее уведомление и есть «первое» для дедупа. Решения видны следующим
// уведомлениям пачки через рабочую копию рейсов.
func ApplyGU2B(
	notifications []GU2BNotification,
	trips []GU2BTrip,
	prior []GU2BPriorEvent,
	resolve GU2BResolveTerminal,
	cutoff int,
) ([]GU2BApply, []GU2BSkip) {
	byVagon := make(map[string][]*GU2BTrip)
	for i := range trips {
		t := &trips[i]
		byVagon[t.Vagon] = append(byVagon[t.Vagon], t)
	}

	// Принятые события по вагонам: затравка — prior из БД, дальше пополняется
	// решениями этой пачки.
	accepted := make(map[string][]GU2BPriorEvent)
	for _, p := range prior {
		accepted[p.Vagon] = append(accepted[p.Vagon], p)
	}

	merged := make(map[string]map[string]any)
	var order []string
	var skips []GU2BSkip

	for _, n := range sortedByEventTime(notifications) {
		et := n.EventTime()
		if et == nil {
			skips = append(skips, GU2BSkip{NotificationID: n.NotificationID, Reason: GU2BSkipNoTime})
			continue
		}
		if !n.Signed() {
			for _, c := range n.Cars {
				skips = append(skips, GU2BSkip{Vagon: c.Vagon, NotificationID: n.NotificationID, Reason: GU2BSkipNotSigned})
			}
			continue
		}
		terminal := ""
		if resolve != nil {
			terminal = resolve(n.OrgOKPO, n.StationName)
		}
		for _, c := range n.Cars {
			if !c.IsUnload() {
				skips = append(skips, GU2BSkip{Vagon: c.Vagon, NotificationID: n.NotificationID, Reason: GU2BSkipNotUnload})
				continue
			}
			if isDup72h(accepted[c.Vagon], n.NotificationID, *et) {
				skips = append(skips, GU2BSkip{Vagon: c.Vagon, NotificationID: n.NotificationID, Reason: GU2BSkipDup72h})
				continue
			}
			trip := gu2bPickTrip(byVagon[c.Vagon], *et, cutoff)
			if trip == nil {
				skips = append(skips, GU2BSkip{Vagon: c.Vagon, NotificationID: n.NotificationID, Reason: GU2BSkipNoTrip})
				continue
			}
			if trip.DateVigr != nil && et.Time().After(trip.DateVigr.Time().Add(gu2bOverwriteSlack)) {
				skips = append(skips, GU2BSkip{Vagon: c.Vagon, NotificationID: n.NotificationID, Reason: GU2BSkipLater})
				continue
			}

			fields := map[string]any{
				"date_vigr":   et,
				"date_vigr_d": unloadJdDate(*et, cutoff),
			}
			if terminal != "" {
				fields["place_vigr"] = terminal
			}
			if _, seen := merged[trip.ID]; !seen {
				order = append(order, trip.ID)
				merged[trip.ID] = map[string]any{}
			}
			for k, v := range fields {
				merged[trip.ID][k] = v
			}
			// Рабочая копия: следующее уведомление пачки видит новую веху и
			// принятое событие (для правил later/duplicate).
			trip.DateVigr = et
			if terminal != "" {
				trip.PlaceVigr = terminal
			}
			accepted[c.Vagon] = append(accepted[c.Vagon], GU2BPriorEvent{
				Vagon: c.Vagon, NotificationID: n.NotificationID, T: *et,
			})
		}
	}

	out := make([]GU2BApply, 0, len(order))
	for _, id := range order {
		out = append(out, GU2BApply{TripID: id, Fields: merged[id]})
	}
	return out, skips
}

// isDup72h — есть ли у вагона принятое событие ближе 72 ч до t от ДРУГОГО
// документа. Повтор того же NotificationID — не дубль, а та же запись.
func isDup72h(events []GU2BPriorEvent, notifID int64, t LocalTime) bool {
	for _, e := range events {
		if e.NotificationID == notifID {
			continue
		}
		d := t.Time().Sub(e.T.Time())
		if d >= 0 && d < gu2bDupWindow {
			return true
		}
	}
	return false
}

// gu2bPickTrip — замок по времени прибытия: последний рейс вагона, чьё прибытие
// МСК-фактом ≤ t (то же правило, что pickTrip у памяток). Рейсы без прибытия и
// «недоехавшие» пропускаются.
func gu2bPickTrip(trips []*GU2BTrip, t LocalTime, cutoff int) *GU2BTrip {
	var found *GU2BTrip
	var foundFact time.Time
	for _, tr := range trips {
		if tr.DatePrib == nil || tr.NotArrived {
			continue
		}
		fact := arrivalFactFromJd(tr.DatePrib.Time(), cutoff)
		if fact.After(t.Time()) {
			continue
		}
		if found == nil || !fact.Before(foundFact) {
			found, foundFact = tr, fact
		}
	}
	return found
}

// unloadJdDate — ЖД-сутки момента выгрузки: «час ≥ cutoff → дата +1», время
// обнулено (колонка date_vigr_d — дата без времени). То же правило, что
// jdFromFact ручного ввода.
func unloadJdDate(t LocalTime, cutoff int) *LocalTime {
	if cutoff <= 0 {
		cutoff = 18
	}
	tt := t.Time()
	if tt.Hour() >= cutoff {
		tt = tt.AddDate(0, 0, 1)
	}
	d := LocalTime(time.Date(tt.Year(), tt.Month(), tt.Day(), 0, 0, 0, 0, time.UTC))
	return &d
}

// sortedByEventTime — уведомления по возрастанию момента выгрузки (nil — в
// конец, всё равно уйдут в no_time); при равных — по NotificationID для
// устойчивого порядка. Копия: входной срез не трогаем.
func sortedByEventTime(in []GU2BNotification) []GU2BNotification {
	out := make([]GU2BNotification, len(in))
	copy(out, in)
	sort.SliceStable(out, func(i, j int) bool {
		ti, tj := out[i].EventTime(), out[j].EventTime()
		if !sameTimePtr(ti, tj) {
			return lessTimePtr(ti, tj)
		}
		return out[i].NotificationID < out[j].NotificationID
	})
	return out
}
