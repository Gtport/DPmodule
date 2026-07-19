package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
)

// HistoryStats — диагностика записи бизнес-истории (S2-6).
type HistoryStats struct {
	Inserted int // новых рейсов (INSERT)
	Updated  int // обновлённых строк (точечный UPDATE по переходу)
}

// applyHistory — Stage 2 (S2-6, §3.19): запись вех рейса в vagon_history. INSERT для
// новых id (нет в истории), точечный UPDATE по переходам статуса/накладной для
// существующих (сравнение с actual = пред. снимок). Трейл vagon_operation — отдельно.
// Идёт ПОСЛЕ forecast и ДО подмены снимка (actual ещё прежний). Без заморозки на 10.
func applyHistory(ctx context.Context, kept []domain.Dislocation, actual *ActualCache, repo port.HistoryRepository) (HistoryStats, error) {
	var st HistoryStats
	ids := make([]string, 0, len(kept))
	for i := range kept {
		if kept[i].Vagon != "" && kept[i].ID != "" {
			ids = append(ids, kept[i].ID)
		}
	}
	existing, err := repo.ExistingIDs(ctx, ids)
	if err != nil {
		return HistoryStats{}, fmt.Errorf("existing ids: %w", err)
	}

	now := clock.Now()
	var toInsert []domain.VagonHistory
	for i := range kept {
		r := &kept[i]
		if r.Vagon == "" || r.ID == "" {
			continue
		}
		if _, ok := existing[r.ID]; !ok {
			toInsert = append(toInsert, buildHistoryRow(r, now))
			continue
		}
		prev, ok := actual.FindVagonInActual(r.Vagon)
		if !ok {
			continue // нет прежнего состояния — переходов не детектируем
		}
		fields := historyUpdateFields(&prev, r)
		if len(fields) == 0 {
			continue
		}
		fields["updated_at"] = now
		if err := repo.UpdateFields(ctx, r.ID, fields); err != nil {
			return HistoryStats{}, fmt.Errorf("update %s: %w", r.ID, err)
		}
		st.Updated++
	}
	if err := repo.Insert(ctx, toInsert); err != nil {
		return HistoryStats{}, fmt.Errorf("insert history: %w", err)
	}
	st.Inserted = len(toInsert)
	return st, nil
}

// historyUpdateFields — точечные обновления по переходам (actual → new): накладная,
// статус, index_main (0→другой), выгрузка (→12), прибытие (→10). Пустая карта = нет
// изменений.
func historyUpdateFields(prev, r *domain.Dislocation) map[string]any {
	fields := map[string]any{}
	if prev.Invoice != r.Invoice {
		fields["invoice"] = r.Invoice // текущая накладная; invoice_main не трогаем
	}
	if prev.Owner != r.Owner && r.Owner != "" {
		fields["owner"] = r.Owner // owner вычислился/появился после вставки строки рейса
	}
	ps, ns := derefInt(prev.Status), derefInt(r.Status)
	if ps == ns {
		return fields
	}
	fields["status"] = ns
	if ps == 0 && ns != 0 {
		fields["index_main"] = r.IndexMain
	}
	if ns == 12 {
		fields["date_vigr"] = r.TimeOp
		fields["date_vigr_d"] = dateOnly(r.DateOpJd)
		fields["place_vigr"] = r.Naznach
	}
	if ns == 10 {
		fields["date_prib"] = r.DateKon
		fields["date_prib_d"] = dateOnly(r.DateKon)
		fields["delay"] = calculateHistoryDelay(dateOnly(r.DateKon), r.DateDostav)
		fields["otkl"] = calculateOtkl(r.DateKon, r.PlanMsk)
		fields["plan_msk"] = r.PlanMsk
		fields["plan_jd"] = r.PlanJd
		fields["naznach"] = r.Naznach
		// Индекс поезда на момент прибытия (решение владельца): в историю пишем
		// ТЕКУЩИЙ индекс дислокации (r.Index — фактический поезд, которым вагон
		// приехал), а не метку нитки плана. Строка истории создавалась при первом
		// появлении вагона, когда прибытийного индекса ещё не было.
		if r.Index != "" {
			fields["index_pp"] = r.Index
		}
	}
	return fields
}

// buildHistoryRow — полная строка истории для нового рейса. Поля прибытия/выгрузки
// проставляются, если запись впервые появилась уже со статусом 10/12.
func buildHistoryRow(r *domain.Dislocation, now domain.LocalTime) domain.VagonHistory {
	h := domain.VagonHistory{
		ID: r.ID, Vagon: r.Vagon, InvoiceMain: r.InvoiceMain, Invoice: r.Invoice,
		IndexMain: r.IndexMain, IndexPp: r.IndexPp, DateNachD: dateOnly(r.DateNach),
		StationNach: r.StationNach, Gruzotpr: r.Gruzotpr, Zayavka: r.Zayavka,
		StanNazn: r.StanNazn, GruzpolS: r.GruzpolS, Naznach: r.Naznach,
		CargoS: r.CargoS, CargoGroup: r.CargoGroup,
		FreightExactName: r.FreightExactName, GtdNumber: r.GtdNumber, Ves: r.Ves,
		Client: r.Client, RodVagUch: r.RodVagUch,
		CarOwnerName: r.CarOwnerName, CarOwnerOkpo: r.CarOwnerOkpo,
		CarTenantName: r.CarTenantName, CarTenantOkpo: r.CarTenantOkpo,
		CarTrustedName: r.CarTrustedName, CarTrustedOkpo: r.CarTrustedOkpo,
		Owner:       r.Owner,
		PereadrType: r.PereadrType, PereadrPort: r.PereadrPort,
		Status: r.Status, DateDostav: r.DateDostav, PlanMsk: r.PlanMsk, PlanJd: r.PlanJd,
		Frost: r.Frost, Shipments: r.Shipments, Peregruz: r.Peregruz,
		Info1: r.Info1, Info2: r.Info2, Sms1: r.Sms1, Sms2: r.Sms2, Sms3: r.Sms3,
		Color: r.Color, CreatedAt: &now, UpdatedAt: &now,
	}
	switch derefInt(r.Status) {
	case 10:
		h.DatePrib = r.DateKon
		h.DatePribD = dateOnly(r.DateKon)
		h.Delay = calculateHistoryDelay(dateOnly(r.DateKon), r.DateDostav)
		h.Otkl = calculateOtkl(r.DateKon, r.PlanMsk)
	case 12:
		h.DateVigr = r.TimeOp
		h.DateVigrD = dateOnly(r.DateOpJd)
		h.PlaceVigr = r.Naznach
	}
	return h
}

// calculateHistoryDelay — просрочка доставки в сутках: дата прибытия vs норматив.
// Прибыл раньше срока → 0; нет одной из дат → nil.
func calculateHistoryDelay(pribD, dostav *domain.LocalTime) *int {
	if pribD == nil || dostav == nil {
		return nil
	}
	p, d := time.Time(*pribD), time.Time(*dostav)
	if p.IsZero() || d.IsZero() {
		return nil
	}
	if p.Before(d) {
		z := 0
		return &z
	}
	days := int(p.Sub(d).Hours() / 24)
	if days < 0 {
		days = 0
	}
	return &days
}

// calculateOtkl — отклонение факта прибытия от плана «±hh:mm». Час факта ≥ 18 → факт
// сдвигается на сутки назад (как в gtlogic). Нет плана → пусто (появится со Stage 4).
func calculateOtkl(fact, plan *domain.LocalTime) string {
	if fact == nil || plan == nil {
		return ""
	}
	f, p := time.Time(*fact), time.Time(*plan)
	if f.IsZero() || p.IsZero() {
		return ""
	}
	if f.Hour() >= 18 {
		f = f.Add(-24 * time.Hour)
	}
	diff := f.Sub(p)
	sign := "+"
	if diff < 0 {
		sign = "-"
		diff = -diff
	}
	return fmt.Sprintf("%s%02d:%02d", sign, int(diff.Hours()), int(diff.Minutes())%60)
}

// dateOnly — только дата (H:M:S=0), nil для nil/нулевого времени.
func dateOnly(t *domain.LocalTime) *domain.LocalTime {
	if t == nil || time.Time(*t).IsZero() {
		return nil
	}
	tt := time.Time(*t)
	d := domain.LocalTime(time.Date(tt.Year(), tt.Month(), tt.Day(), 0, 0, 0, 0, time.UTC))
	return &d
}
