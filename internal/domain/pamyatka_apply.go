package domain

import "sort"

// Стадия заполнения рейса памятками ГУ-45 (колонка vagon_history.pamyatka_state).
const (
	PamyatkaStateNone = 0 // памяток не было
	PamyatkaStatePod  = 1 // подача проставлена
	PamyatkaStateUbor = 2 // уборка проставлена
)

// Значения OPERATION_TYPE провайдера. Других не бывает: проверено на боевой
// выборке за 3 суток (167 памяток — 59 «подачу», 108 «уборку»).
const (
	PamyatkaOperPod  = "подачу"
	PamyatkaOperUbor = "уборку"
)

// PamyatkaTrip — рейс из vagon_history глазами движка памяток: только то, что
// нужно, чтобы выбрать строку и решить, писать ли в неё. Полную строку истории
// движок не читает — она большая, а решают четыре поля.
type PamyatkaTrip struct {
	ID          string     // id строки vagon_history
	Vagon       string     // номер вагона
	DatePrib    *LocalTime // прибытие: якорь привязки памятки к рейсу
	State       int        // pamyatka_state: 0/1/2
	NomGu45Pod  string     // номер уже записанной памятки на подачу
	NomGu45Ubor string     // номер уже записанной памятки на уборку
	DateUbor    *LocalTime // записанная уборка: признак «рейс уже закрыт памяткой»
}

// PamyatkaApply — что движок решил записать в одну строку истории.
// Fields — колонки для точечного UPDATE (канон GORM-гибрида: только непустые).
type PamyatkaApply struct {
	TripID string
	Fields map[string]any
}

// PamyatkaSkip — почему строка памятки не легла ни на один рейс. Копится ради
// журнала: молчаливая потеря трети строк — ровно тот случай, который приходится
// расследовать месяцами.
type PamyatkaSkip struct {
	Vagon  string
	Number string
	Reason string
}

// Причины пропуска (стабильные коды для журнала и счётчиков).
const (
	PamyatkaSkipNoTrip     = "no_trip"     // не нашлось рейса с прибытием до подачи
	PamyatkaSkipNoGetIn    = "no_get_in"   // строка вагона без времени подачи (контракт такого не даёт)
	PamyatkaSkipNotFuller  = "not_fuller"  // рейс уже занят другой памяткой, и новая не полнее
	PamyatkaSkipUnknownOp  = "unknown_op"  // OPERATION_TYPE не «подачу»/«уборку»
	PamyatkaSkipStaleState = "stale_state" // стадия рейса уже дальше того, что несёт памятка
)

// ApplyPamyatki раскладывает пачку памяток по рейсам истории.
//
// trips — рейсы всех вагонов пачки (по вагону может быть несколько). Правила
// (решения владельца 28.07.2026):
//
//   - Рейс выбирается ЗАМКОМ ПО ВРЕМЕНИ ПРИБЫТИЯ: последний рейс вагона, у
//     которого date_prib ≤ время подачи. Матч по одному номеру вагона на боевых
//     данных сажает треть строк (510 из 1564) на посторонний рейс: памятки
//     приходят задним числом — документ от 25.07 описывает подачу от 03.07, а
//     вагон к тому моменту успел приехать заново.
//   - Памятка на уборку несёт время подачи ВСЕГДА (100 % боевых строк), поэтому
//     на рейсе со стадией 0 она заполняет оба блока и ставит сразу 2. Иначе
//     вагоны, чью подачу инкремент не показал, застряли бы на нуле навсегда:
//     уборочных строк приходит больше, чем подачных (1959 против 1564).
//   - Та же памятка приходит повторно (документ дозаполняют — у 160 подачных
//     строк уже проставлен GET_OUT): узнаём по совпадению номера и обновляем
//     на месте, стадию при этом не понижаем.
//   - Другая памятка на тот же рейс перезаписывает его, только если она ПОЛНЕЕ
//     (несёт уборку, а в рейсе её ещё нет). Так вагон 62231725 достаётся
//     памятке №11314 (подача + уведомление + уборка), а не пришедшей первой
//     №11255 с одной лишь подачей.
//
// Порядок применения внутри пачки — по дате составления памятки: если в одном
// ответе пришли и подача, и уборка одного рейса, побеждает более поздняя.
// Результат — по одной записи на рейс: правки нескольких памяток слиты.
func ApplyPamyatki(pamyatki []Pamyatka, trips []PamyatkaTrip) ([]PamyatkaApply, []PamyatkaSkip) {
	byVagon := make(map[string][]PamyatkaTrip, len(trips))
	for _, t := range trips {
		byVagon[t.Vagon] = append(byVagon[t.Vagon], t)
	}
	// Внутри вагона — по возрастанию прибытия: замок ищет последний подходящий.
	for v := range byVagon {
		sort.SliceStable(byVagon[v], func(i, j int) bool {
			return lessTimePtr(byVagon[v][i].DatePrib, byVagon[v][j].DatePrib)
		})
	}

	// state — рабочая копия рейсов: правки предыдущих памяток пачки видны
	// следующим, иначе две памятки одного рейса решали бы вслепую друг о друге.
	state := make(map[string]*PamyatkaTrip, len(trips))
	for i := range trips {
		t := trips[i]
		state[t.ID] = &t
	}

	merged := make(map[string]map[string]any, len(trips))
	order := make([]string, 0, len(trips))
	var skips []PamyatkaSkip

	for _, p := range sortedByCreate(pamyatki) {
		if p.OperationType != PamyatkaOperPod && p.OperationType != PamyatkaOperUbor {
			for _, v := range p.Vagons {
				skips = append(skips, PamyatkaSkip{Vagon: v.Vagon, Number: p.Number, Reason: PamyatkaSkipUnknownOp})
			}
			continue
		}
		for _, v := range p.Vagons {
			if v.GetIn == nil {
				skips = append(skips, PamyatkaSkip{Vagon: v.Vagon, Number: p.Number, Reason: PamyatkaSkipNoGetIn})
				continue
			}
			trip := pickTrip(byVagon[v.Vagon], state, *v.GetIn)
			if trip == nil {
				skips = append(skips, PamyatkaSkip{Vagon: v.Vagon, Number: p.Number, Reason: PamyatkaSkipNoTrip})
				continue
			}
			fields, reason := decide(&p, v, trip)
			if fields == nil {
				skips = append(skips, PamyatkaSkip{Vagon: v.Vagon, Number: p.Number, Reason: reason})
				continue
			}
			if _, seen := merged[trip.ID]; !seen {
				order = append(order, trip.ID)
				merged[trip.ID] = map[string]any{}
			}
			for k, val := range fields {
				merged[trip.ID][k] = val
			}
		}
	}

	out := make([]PamyatkaApply, 0, len(order))
	for _, id := range order {
		out = append(out, PamyatkaApply{TripID: id, Fields: merged[id]})
	}
	return out, skips
}

// pickTrip — замок по времени прибытия: последний рейс вагона с date_prib ≤ getIn.
// Рейсы без прибытия пропускаем: привязать памятку не к чему.
func pickTrip(trips []PamyatkaTrip, state map[string]*PamyatkaTrip, getIn LocalTime) *PamyatkaTrip {
	var found *PamyatkaTrip
	for i := range trips {
		if trips[i].DatePrib == nil || trips[i].DatePrib.Time().After(getIn.Time()) {
			continue
		}
		found = state[trips[i].ID]
	}
	return found
}

// decide — писать ли эту строку памятки в этот рейс, и какие колонки. Возвращает
// nil и причину, если писать не надо. Обновляет стадию/номера в рабочей копии
// рейса, чтобы следующая памятка пачки видела уже принятое решение.
func decide(p *Pamyatka, v PamyatkaVagon, trip *PamyatkaTrip) (map[string]any, string) {
	pod := p.OperationType == PamyatkaOperPod
	sameDoc := (pod && trip.NomGu45Pod == p.Number) || (!pod && trip.NomGu45Ubor == p.Number)

	// Повторный приход того же документа — всегда обновляем на месте: он мог
	// дозаполниться уведомлением и уборкой.
	if !sameDoc {
		switch {
		case trip.State == PamyatkaStateNone:
			// свободный рейс — пишем
		case trip.State == PamyatkaStatePod && !pod:
			// подача уже есть, пришла уборка — штатное продолжение цепочки
		case v.GetOut != nil && trip.DateUbor == nil:
			// рейс занят другой памяткой, но новая полнее: несёт уборку
		case trip.State == PamyatkaStateUbor || (trip.State == PamyatkaStatePod && pod):
			return nil, PamyatkaSkipStaleState
		default:
			return nil, PamyatkaSkipNotFuller
		}
	}

	fields := map[string]any{}
	if pod || trip.State == PamyatkaStateNone {
		// Блок подачи пишем из подачной памятки, а из уборочной — только когда
		// подачи ещё не было (уборочная несёт GET_IN всегда).
		fields["nom_gu45_pod"] = p.Number
		fields["date_gu45_pod"] = p.DateCreate
		fields["date_pod"] = v.GetIn
		fields["place_pod"] = p.GetPlace
		trip.NomGu45Pod = p.Number
	}
	if v.Report != nil {
		// Окончание грузовой операции по ГУ-45. Веху date_vigr из истории АСУ
		// не трогаем — два источника должны расходиться видимо.
		fields["date_vigr_gu45"] = v.Report
	}
	if v.GetOut != nil {
		fields["nom_gu45_ubor"] = p.Number
		fields["date_gu45_ubor"] = p.DateCreate
		fields["date_ubor"] = v.GetOut
		trip.NomGu45Ubor = p.Number
		trip.DateUbor = v.GetOut
	}

	next := PamyatkaStatePod
	if v.GetOut != nil {
		next = PamyatkaStateUbor
	}
	if next > trip.State {
		trip.State = next
		fields["pamyatka_state"] = next
	}
	return fields, ""
}

// sortedByCreate — памятки по возрастанию даты составления (при равной — по
// номеру, чтобы порядок был устойчив). Копия: входной срез не трогаем.
func sortedByCreate(in []Pamyatka) []Pamyatka {
	out := make([]Pamyatka, len(in))
	copy(out, in)
	sort.SliceStable(out, func(i, j int) bool {
		if !sameTimePtr(out[i].DateCreate, out[j].DateCreate) {
			return lessTimePtr(out[i].DateCreate, out[j].DateCreate)
		}
		return out[i].Number < out[j].Number
	})
	return out
}

func lessTimePtr(a, b *LocalTime) bool {
	switch {
	case a == nil:
		return b != nil
	case b == nil:
		return false
	default:
		return a.Time().Before(b.Time())
	}
}

func sameTimePtr(a, b *LocalTime) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Time().Equal(b.Time())
}
