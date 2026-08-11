package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
)

// BrosStats — диагностика reconcile брошенных (Stage 2, после Stage 4).
type BrosStats struct {
	New     int // новых бросков (INSERT)
	Updated int // обновлённых активных (состав/план/индекс)
	Stopped int // поднятых (status_br=false)
	Active  int // активных после пересбора

	// Events — события new/stop для уведомлений (см. NotifyBrosEvents):
	// applyBros остаётся чистым, отправку делает вызывающий ProcessRecords.
	Events []BrosEvent
}

// applyBros — reconcile снимка брошенных (перенос gtport dislocation_status5, но
// без stateful-очереди new/stop). Свежий батч (после Stage 4, с ProgJd) сверяется
// с таблицей bros:
//   - вагоны статуса 5 группируются по id_status5 (brosKey) → активные броски;
//   - новый ключ → INSERT; уже активный → UPDATE (состав/план/индекс);
//   - активный в БД ключ, которого больше нет среди статуса 5 → STOP (поднят):
//     status_br=false, дата/индекс/прогноз подъёма из нового состояния вагона.
//
// Новое состояние поднятого поезда берём из свежего батча по номерам вагонов
// прежнего снимка (там ещё стоял старый ключ бросания — после подъёма станция
// операции меняется и ключ пересчитывается). Идёт ПОСЛЕ Stage 4 и ДО подмены
// снимка (actual = прежний снимок). Только при ingest РЖД — операторские правки
// снимка (MutateSnapshot) статус 5 не меняют.
func applyBros(ctx context.Context, kept []domain.Dislocation, actual *ActualCache, repo port.BrosRepository) (BrosStats, error) {
	var st BrosStats

	fresh := groupBrosStatus5(kept)
	st.Active = len(fresh)

	active, err := repo.Active(ctx)
	if err != nil {
		return BrosStats{}, fmt.Errorf("active bros: %w", err)
	}
	activeByID := make(map[string]domain.Bros, len(active))
	for _, b := range active {
		activeByID[b.ID] = b
	}

	now := clock.Now()

	// Новые + продолжающиеся броски.
	for key, g := range fresh {
		if cur, ok := activeByID[key]; ok {
			fields := brosContinueFields(cur, g, now)
			if len(fields) == 0 {
				continue
			}
			if err := repo.Update(ctx, key, fields); err != nil {
				return BrosStats{}, fmt.Errorf("update bros %s: %w", key, err)
			}
			st.Updated++
			continue
		}
		// Ключа нет среди активных, но он может лежать в таблице ПОДНЯТЫМ:
		// группа снова в статусе 5 с тем же id_status5 (время операции не
		// менялось) — повторное бросание. INSERT ронял пересбор дублем PK
		// (боевой инцидент 27.07.2026, вскрыт эскалацией 4→5) — переоткрываем
		// строку: снова активна, следы прежнего подъёма стёрты, reason/comment
		// оператора не трогаем.
		prev, err := repo.GetByID(ctx, key)
		if err != nil {
			return BrosStats{}, fmt.Errorf("get bros %s: %w", key, err)
		}
		if prev != nil {
			fields := brosContinueFields(*prev, g, now)
			fields["status_br"] = true
			fields["date_pod_fact"] = nil
			fields["prog_1"] = nil
			fields["updated_at"] = now
			if err := repo.Update(ctx, key, fields); err != nil {
				return BrosStats{}, fmt.Errorf("reopen bros %s: %w", key, err)
			}
			st.New++ // снова активный бросок
			st.Events = append(st.Events, brosNewEvent(g))
			continue
		}
		if err := repo.Insert(ctx, buildBrosRecord(g, now)); err != nil {
			return BrosStats{}, fmt.Errorf("insert bros %s: %w", key, err)
		}
		st.New++
		st.Events = append(st.Events, brosNewEvent(g))
	}

	// Подъёмы: активные в БД, которых больше нет среди статуса 5.
	var prevVagons map[string][]string
	var keptByVagon map[string]*domain.Dislocation
	for i := range active {
		cur := active[i]
		if _, still := fresh[cur.ID]; still {
			continue
		}
		if prevVagons == nil { // ленивое построение только когда есть подъёмы
			prevVagons = prevStatus5Vagons(actual)
			keptByVagon = brosIndexByVagon(kept)
		}
		sample := brosStopSample(prevVagons[cur.ID], keptByVagon)
		if err := repo.Update(ctx, cur.ID, brosStopFields(sample, now)); err != nil {
			return BrosStats{}, fmt.Errorf("stop bros %s: %w", cur.ID, err)
		}
		st.Stopped++
		st.Events = append(st.Events, brosStopEvent(cur, sample, now))
	}

	return st, nil
}

// brosNewEvent — событие нового/повторного броска для уведомлений.
func brosNewEvent(g *brosGroup) BrosEvent {
	return BrosEvent{
		Kind: brosEventNew, ID: g.Key, Index: g.Index,
		Station: g.StationOper, Doroga: g.DorogaOper,
		VagonCount: g.VagonCount, Sostav: brosSostav(g),
		DateBr: dateOnly(g.DateKon),
	}
}

// brosStopEvent — событие подъёма: реквизиты — из записи bros (как gtport
// notifyStopBrosToOperators), дата подъёма — из нового состояния вагона либо
// момент фиксации (ровно то, что ушло в date_pod_fact).
func brosStopEvent(cur domain.Bros, sample *domain.Dislocation, now domain.LocalTime) BrosEvent {
	datePod := dateOnly(&now)
	index := cur.Index1
	if sample != nil {
		datePod = dateOnly(sample.DateKon)
		if sample.Index != "" {
			index = sample.Index
		}
	}
	if index == "" {
		index = cur.Index0
	}
	return BrosEvent{
		Kind: brosEventStop, ID: cur.ID, Index: index,
		Station: cur.StationBr, Doroga: cur.DorogaBr,
		VagonCount: cur.VagonCount, DateBr: cur.DateBr, DatePod: datePod,
	}
}

// brosGroup — агрегат вагонов одного брошенного поезда (ключ id_status5).
type brosGroup struct {
	Key         string
	Index       string
	StationOper string
	DorogaOper  string
	GruzpolS    string
	DateKon     *domain.LocalTime
	PlanJd      *domain.LocalTime
	ProgJd      *domain.LocalTime
	ToGo        *float64
	VagonCount  int
	subgroups   map[string]*brosSubgroup
	subOrder    []string // порядок появления подгрупп (детерминированный состав)
}

// brosSubgroup — часть состава с однородными назначением/грузом (для описания).
type brosSubgroup struct {
	StationNach string
	CargoS      string
	GruzpolS    string
	Naznach     string
	VagonCount  int
}

// groupBrosStatus5 группирует вагоны статуса 5 по ключу id_status5.
func groupBrosStatus5(kept []domain.Dislocation) map[string]*brosGroup {
	groups := map[string]*brosGroup{}
	for i := range kept {
		r := &kept[i]
		if r.Status == nil || *r.Status != 5 || r.IdStatus5 == "" {
			continue
		}
		g := groups[r.IdStatus5]
		if g == nil {
			g = &brosGroup{
				Key: r.IdStatus5, Index: r.Index,
				StationOper: r.StationOper, DorogaOper: r.DorogaOper,
				GruzpolS: r.GruzpolS, DateKon: r.DateKon,
				PlanJd: r.PlanJd, ProgJd: r.ProgJd, ToGo: r.ToGo,
				subgroups: map[string]*brosSubgroup{},
			}
			groups[r.IdStatus5] = g
		}
		g.VagonCount++
		subKey := r.StationNach + "|" + r.CargoS + "|" + r.GruzpolS + "|" + r.Naznach
		sg := g.subgroups[subKey]
		if sg == nil {
			sg = &brosSubgroup{StationNach: r.StationNach, CargoS: r.CargoS, GruzpolS: r.GruzpolS, Naznach: r.Naznach}
			g.subgroups[subKey] = sg
			g.subOrder = append(g.subOrder, subKey)
		}
		sg.VagonCount++
	}
	return groups
}

// brosSostav — описание состава «Станция Груз (N) [на Назначение]» через ", »
// (перенос gtport createDName). «на Назначение» — если оно отличается от терминала.
func brosSostav(g *brosGroup) string {
	parts := make([]string, 0, len(g.subOrder))
	for _, k := range g.subOrder {
		sg := g.subgroups[k]
		part := fmt.Sprintf("%s %s (%d)", sg.StationNach, sg.CargoS, sg.VagonCount)
		if sg.GruzpolS != "" && sg.Naznach != "" &&
			strings.TrimSpace(sg.GruzpolS) != strings.TrimSpace(sg.Naznach) {
			part += " на " + sg.Naznach
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ", ")
}

// buildBrosRecord — новый бросок из группы.
func buildBrosRecord(g *brosGroup, now domain.LocalTime) domain.Bros {
	plan := dateOnly(g.PlanJd)
	return domain.Bros{
		ID: g.Key, Index0: g.Index, Index1: g.Index,
		StationBr: g.StationOper, DorogaBr: g.DorogaOper,
		DateBr: dateOnly(g.DateKon), GruzpolS: g.GruzpolS,
		Prog0: g.ProgJd, ToGo: g.ToGo,
		Plan: plan, PlanHistory: brosPlanDateStr(plan),
		StatusBr: true, Sostav: brosSostav(g), VagonCount: g.VagonCount,
		CreatedAt: &now, UpdatedAt: &now,
	}
}

// brosContinueFields — изменения активного броска (только отличия): состав,
// число вагонов, текущий индекс, план подвода (+ дозапись в историю плана).
func brosContinueFields(cur domain.Bros, g *brosGroup, now domain.LocalTime) map[string]any {
	fields := map[string]any{}
	if g.VagonCount != cur.VagonCount {
		fields["vagon_count"] = g.VagonCount
	}
	if s := brosSostav(g); s != cur.Sostav {
		fields["sostav"] = s
	}
	if g.Index != "" && g.Index != cur.Index1 {
		fields["index_1"] = g.Index
	}
	newPlan := dateOnly(g.PlanJd)
	if !brosSameDate(newPlan, cur.Plan) {
		fields["plan"] = newPlan
		if s := brosPlanDateStr(newPlan); s != "" {
			if cur.PlanHistory != "" {
				fields["plan_history"] = cur.PlanHistory + ", " + s
			} else {
				fields["plan_history"] = s
			}
		}
	}
	if len(fields) > 0 {
		fields["updated_at"] = now
	}
	return fields
}

// brosStopFields — поля подъёма броска. sample — новое состояние вагона (из
// свежего батча); nil, если вагоны ушли из поля зрения (фиксируем подъём сейчас).
func brosStopFields(sample *domain.Dislocation, now domain.LocalTime) map[string]any {
	fields := map[string]any{
		"status_br":  false,
		"updated_at": now,
	}
	if sample != nil {
		fields["date_pod_fact"] = dateOnly(sample.DateKon)
		fields["prog_1"] = sample.ProgJd
		fields["plan"] = dateOnly(sample.PlanJd)
		if sample.Index != "" {
			fields["index_1"] = sample.Index
		}
	} else {
		fields["date_pod_fact"] = dateOnly(&now)
	}
	return fields
}

// prevStatus5Vagons — номера вагонов статуса 5 из прежнего снимка, по ключу.
func prevStatus5Vagons(actual *ActualCache) map[string][]string {
	out := map[string][]string{}
	for _, v := range actual.All() {
		if v.Status == nil || *v.Status != 5 || v.IdStatus5 == "" || v.Vagon == "" {
			continue
		}
		out[v.IdStatus5] = append(out[v.IdStatus5], v.Vagon)
	}
	return out
}

// brosIndexByVagon — индекс свежего батча по номеру вагона.
func brosIndexByVagon(kept []domain.Dislocation) map[string]*domain.Dislocation {
	out := make(map[string]*domain.Dislocation, len(kept))
	for i := range kept {
		if kept[i].Vagon != "" {
			out[kept[i].Vagon] = &kept[i]
		}
	}
	return out
}

// brosStopSample — первый из вагонов броска, найденный в свежем батче (его новое
// состояние даёт дату/индекс/прогноз подъёма). nil — ни одного не осталось.
func brosStopSample(vagons []string, keptByVagon map[string]*domain.Dislocation) *domain.Dislocation {
	for _, v := range vagons {
		if r, ok := keptByVagon[v]; ok {
			return r
		}
	}
	return nil
}

// brosPlanDateStr — дата плана как «2006-01-02» (пусто для nil/нулевой).
func brosPlanDateStr(t *domain.LocalTime) string {
	if t == nil || time.Time(*t).IsZero() {
		return ""
	}
	return time.Time(*t).Format("2006-01-02")
}

// brosSameDate — равны ли даты (без времени); nil/нулевые считаются равными между собой.
func brosSameDate(a, b *domain.LocalTime) bool {
	an := a == nil || time.Time(*a).IsZero()
	bn := b == nil || time.Time(*b).IsZero()
	if an || bn {
		return an && bn
	}
	ta, tb := time.Time(*a), time.Time(*b)
	return ta.Year() == tb.Year() && ta.YearDay() == tb.YearDay()
}

// ── Чтение снимка брошенных (API) ───────────────────────────────────────────

// BrosService — чтение снимка брошенных: активные, история, фильтр отчёта, поиск,
// отчёт с разбивкой суток по кодам (агрегация журнала).
type BrosService struct {
	repo    port.BrosRepository
	journal port.BrosJournalRepository
}

func NewBrosService(repo port.BrosRepository, journal port.BrosJournalRepository) *BrosService {
	return &BrosService{repo: repo, journal: journal}
}

func (s *BrosService) Active(ctx context.Context) ([]domain.Bros, error) {
	return s.repo.Active(ctx)
}

func (s *BrosService) History(ctx context.Context, limit, offset int) ([]domain.Bros, int, error) {
	return s.repo.History(ctx, limit, offset)
}

func (s *BrosService) Filter(ctx context.Context, f domain.BrosFilter) ([]domain.Bros, int, error) {
	return s.repo.Filter(ctx, f)
}

func (s *BrosService) Search(ctx context.Context, q string) ([]domain.Bros, error) {
	return s.repo.SearchByIndex(ctx, q)
}
