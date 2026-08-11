package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

// Эмиттеры уведомлений конвейера дислокации (перенос триггеров gtport:
// notifyNewBrosToOperators / notifyStopBrosToOperators /
// notifyTrainArrivalToOperators / NotifyStationsEditRequired /
// NotifyMarkaEditRequired). Тексты — эталон gtport, дедуп — персистентный
// dedup_key вместо RAM-мап (рестарт не респамит). Все вызовы идут только из
// ProcessRecords (поток ЛК/АСУ/робота); операторский MutateSnapshot не эмитит —
// как в gtport, уведомляет только поток.

// BrosEvent — событие реконсиляции брошенных (applyBros) для уведомлений:
// новый/повторный бросок либо подъём.
type BrosEvent struct {
	Kind       string // "new" | "stop"
	ID         string // id_status5 (ключ bros)
	Index      string
	Station    string // станция бросания
	Doroga     string
	VagonCount int
	Sostav     string            // описание состава (new)
	DateBr     *domain.LocalTime // дата бросания
	DatePod    *domain.LocalTime // дата подъёма (stop)
}

const (
	brosEventNew  = "new"
	brosEventStop = "stop"
)

// NotifyBrosEvents — уведомления о брошенных/поднятых поездах (аудитория oper).
// Дедуп-ключ несёт дату СЕГОДНЯ: переоткрытие того же id_status5 спустя дни —
// честное новое событие, а повторы внутри суток гасятся.
func (s *NotificationService) NotifyBrosEvents(ctx context.Context, events []BrosEvent) {
	if s == nil || len(events) == 0 {
		return
	}
	today := clock.Now().Time().Format("2006-01-02")
	for _, e := range events {
		switch e.Kind {
		case brosEventNew:
			msg := fmt.Sprintf("Поезд %s брошен на станции %s (%s).", e.Index, e.Station, e.Doroga)
			if e.VagonCount > 0 {
				msg += fmt.Sprintf(" Состав: %d вагонов", e.VagonCount)
				if e.Sostav != "" {
					msg += fmt.Sprintf(" (%s)", e.Sostav)
				}
				msg += "."
			}
			msg += fmt.Sprintf(" Дата бросания: %s.", notifDate(e.DateBr))
			s.Notify(ctx, domain.Notification{
				Type: domain.NotifyTypeWarning, Audience: domain.AudienceOper,
				Title: "Брошен поезд!", Message: msg,
				ActionComponent: domain.NotifyActionBros,
				ActionParams: notifParams(map[string]any{
					"bros_id": e.ID, "index": e.Index, "station": e.Station,
					"doroga": e.Doroga, "vagon_count": e.VagonCount, "date_br": notifDate(e.DateBr),
				}),
				DedupKey: fmt.Sprintf("bros_new_%s_%s", e.ID, today),
			})
		case brosEventStop:
			msg := fmt.Sprintf("Поезд %s поднят на станции %s. Брошенный %s поднят %s.",
				e.Index, e.Station, notifDate(e.DateBr), notifDate(e.DatePod))
			if e.VagonCount > 0 {
				msg += fmt.Sprintf(" Вагонов: %d.", e.VagonCount)
			}
			s.Notify(ctx, domain.Notification{
				Type: domain.NotifyTypeInfo, Audience: domain.AudienceOper,
				Title: "Поезд восстановлен в движении", Message: msg,
				ActionComponent: domain.NotifyActionBros,
				ActionParams: notifParams(map[string]any{
					"bros_id": e.ID, "index": e.Index, "station": e.Station,
					"vagon_count": e.VagonCount, "date_br": notifDate(e.DateBr),
					"date_pod": notifDate(e.DatePod), "duration_days": brosDurationDays(e.DateBr, e.DatePod),
				}),
				DedupKey: fmt.Sprintf("bros_stop_%s_%s", e.ID, today),
			})
		}
	}
}

// ArrivalGroup — прибывший поезд (переходы <10 → 10 одного забора), группа по
// индексу и единому моменту прибытия.
type ArrivalGroup struct {
	Index      string
	Station    string // станция назначения
	Prib       *domain.LocalTime
	VagonCount int
}

// collectArrivalGroups — прибывшие поезда батча: вагон в статусе 10, которого в
// прежнем снимке не было либо он был со статусом < 10 (sticky-10 повторно не
// сигналит). Группировка по индексу + момент прибытия (единый штамп на поезд от
// провайдера); пустой индекс и «Б/И» пропускаются — как в gtport (status10).
// Зовётся ДО подмены снимка (actual = прежний).
func collectArrivalGroups(all []domain.Dislocation, actual *ActualCache, cutoff int) []ArrivalGroup {
	type akey struct {
		index string
		prib  string
	}
	groups := map[akey]*ArrivalGroup{}
	var order []akey
	for i := range all {
		r := &all[i]
		if r.Status == nil || *r.Status != 10 {
			continue
		}
		if r.Index == "" || r.Index == "Б/И" {
			continue
		}
		if prev, ok := actual.FindVagonInActual(r.Vagon); ok &&
			prev.Status != nil && *prev.Status >= 10 {
			continue
		}
		prib := historyArrival(r, cutoff)
		k := akey{index: r.Index, prib: notifMinute(prib)}
		g := groups[k]
		if g == nil {
			g = &ArrivalGroup{Index: r.Index, Station: r.StanNazn, Prib: prib}
			groups[k] = g
			order = append(order, k)
		}
		g.VagonCount++
	}
	out := make([]ArrivalGroup, 0, len(order))
	for _, k := range order {
		out = append(out, *groups[k])
	}
	return out
}

// NotifyArrivals — уведомления о прибытии поездов (аудитория oper). Дедуп по
// индексу + минуте прибытия: вагоны состава, доехавшие до статуса 10 разными
// заборами, несут единый штамп прибытия и второй раз не сигналят.
func (s *NotificationService) NotifyArrivals(ctx context.Context, groups []ArrivalGroup) {
	if s == nil {
		return
	}
	for _, g := range groups {
		when := "время не указано"
		if g.Prib != nil {
			when = g.Prib.Time().Format("02.01.06 15:04")
		}
		s.Notify(ctx, domain.Notification{
			Type: domain.NotifyTypeInfo, Audience: domain.AudienceOper,
			Title: "Прибытие поезда",
			Message: fmt.Sprintf("Поезд %s прибыл на станцию %s. Время прибытия: %s. Вагонов: %d",
				g.Index, g.Station, when, g.VagonCount),
			ActionComponent: domain.NotifyActionArrivals,
			ActionParams: notifParams(map[string]any{
				"index_pp": g.Index, "station": g.Station,
				"arrival_time": when, "vagon_count": g.VagonCount,
			}),
			DedupKey: fmt.Sprintf("arr_%s_%s", g.Index, notifMinute(g.Prib)),
		})
	}
}

// NotifyStationsMissing — станции вне справочника (Stage 1, enr.StationsNotFound):
// аудитория dicts (та же граница, что правка словаря), deep-link в админ-редактор
// stations. Дедуп по коду; после purge (72 ч) неисправленная станция напомнит
// о себе снова — осознанное поведение-напоминание.
func (s *NotificationService) NotifyStationsMissing(ctx context.Context, codes []int) {
	if s == nil {
		return
	}
	for _, kod := range codes {
		s.Notify(ctx, domain.Notification{
			Type: domain.NotifyTypeService, Audience: domain.AudienceDicts,
			Title: "Станция не найдена в справочнике",
			Message: fmt.Sprintf("Станция с кодом %06d отсутствует в справочнике. "+
				"Необходимо добавить её в таблицу станций.", kod),
			ActionComponent: domain.NotifyActionAdminDict,
			ActionParams:    notifParams(map[string]any{"table": "stations", "kod": kod}),
			DedupKey:        fmt.Sprintf("station_%d", kod),
		})
	}
}

// MarkaMissCombo — комбинация ключа marka без записи в словаре (тот же ключ и
// фильтры, что у экрана «Без атрибуции» — MarkaUnmatchedService.Groups).
type MarkaMissCombo struct {
	Okpo       string // ОКПО грузоотправителя (сырой, как в снимке)
	Station    string // код станции отправления
	CargoGroup string // группа груза (словарь cargo)
	VagonCount int
}

// collectMarkaMissing — комбинации несматченных вагонов батча: гружёный, без
// атрибуции (gruzotpr пуст), не порожний под погрузку. Зовётся после
// applyMarkaEnrichment (S2-3), до подмены снимка.
func collectMarkaMissing(all []domain.Dislocation, dir *DirectoryCache) []MarkaMissCombo {
	type mkey struct{ okpo, station, group string }
	combos := map[mkey]*MarkaMissCombo{}
	var order []mkey
	for i := range all {
		r := &all[i]
		if r.Gruzotpr != "" || r.Vagon == "" {
			continue
		}
		if dir.PorozhInbound(r) {
			continue
		}
		k := mkey{r.GruzotprOkpo, r.CodeStationNach, r.CargoGroup}
		c := combos[k]
		if c == nil {
			c = &MarkaMissCombo{Okpo: r.GruzotprOkpo, Station: r.CodeStationNach, CargoGroup: r.CargoGroup}
			combos[k] = c
			order = append(order, k)
		}
		c.VagonCount++
	}
	out := make([]MarkaMissCombo, 0, len(order))
	for _, k := range order {
		out = append(out, *combos[k])
	}
	return out
}

// NotifyMarkaMissing — дыры справочника marka («Наши грузы»): аудитория dicts,
// deep-link в модалку «Без атрибуции» (роль уведомления — позвать к экрану,
// сам экран не дублируем). Дедуп по комбинации ключа.
func (s *NotificationService) NotifyMarkaMissing(ctx context.Context, combos []MarkaMissCombo) {
	if s == nil {
		return
	}
	for _, c := range combos {
		s.Notify(ctx, domain.Notification{
			Type: domain.NotifyTypeService, Audience: domain.AudienceDicts,
			Title: "Отсутствует запись в справочнике «Наши грузы»",
			Message: fmt.Sprintf("Не найдена запись для комбинации: ОКПО=%s, Станция=%s, "+
				"Группа груза=%s (вагонов: %d). Назначить атрибуцию — экран «Без атрибуции».",
				notifOrDash(c.Okpo), notifOrDash(c.Station), notifOrDash(c.CargoGroup), c.VagonCount),
			ActionComponent: domain.NotifyActionUnmatched,
			ActionParams: notifParams(map[string]any{
				"okpo": c.Okpo, "station": c.Station, "cargo_group": c.CargoGroup,
			}),
			DedupKey: fmt.Sprintf("marka_%s_%s_%s", c.Okpo, c.Station, c.CargoGroup),
		})
	}
}

// brosDurationDays — длительность бросания в сутках (как gtport
// calculateBrosDurationDays); нет одной из дат → 0.
func brosDurationDays(dateBr, datePod *domain.LocalTime) int {
	if dateBr == nil || datePod == nil || dateBr.IsZero() || datePod.IsZero() {
		return 0
	}
	return int(datePod.Time().Sub(dateBr.Time()).Hours() / 24)
}

// notifDate — дата для текста уведомления (формат gtport «02.01.06»).
func notifDate(t *domain.LocalTime) string {
	if t == nil || t.IsZero() {
		return "дата не указана"
	}
	return t.Time().Format("02.01.06")
}

// notifMinute — штамп до минуты для dedup-ключей; nil → пусто.
func notifMinute(t *domain.LocalTime) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.Time().Format("2006-01-02T15:04")
}

// notifOrDash — пустое значение в тексте показываем прочерком.
func notifOrDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// notifParams — параметры deep-link в json (ошибок не бывает: map строк и чисел).
func notifParams(m map[string]any) json.RawMessage {
	b, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	return b
}
