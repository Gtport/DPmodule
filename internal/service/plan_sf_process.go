package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/parser/plan"
	"github.com/Gtport/DPmodule/internal/service/planmatch"
)

// ── Двухфазная обработка плана с выбором групп с.ф. пользователем ───────────
//
// Фаза A (Prepare): разобрать план, сматчить обычные нитки, посчитать кандидатов
// для каждой с.ф. Снимок НЕ трогаем, кладём файл в pending по токену.
// Фаза B (Confirm): по токену заново разбираем и матчим против ТЕКУЩЕГО снимка,
// заполняем выбранные группы с.ф., применяем всё одним свопом. Так окно
// рассогласования (снимок мог пересобраться между фазами) закрывается на confirm.

// SFCandidateDTO — группа-кандидат вагонов для с.ф. (для диалога выбора на фронте).
type SFCandidateDTO struct {
	IdDisl   string   `json:"id_disl"`
	Station  string   `json:"station"`
	Index    string   `json:"index"`
	Date     string   `json:"date"`
	Quantity int      `json:"quantity"`
	Sostav   string   `json:"sostav"` // «Состав» группы — как у обычных ниток (FormatSostav)
	Vagons   []string `json:"vagons"`
}

// SFRowDTO — одна с.ф.-нитка плана с её кандидатами.
type SFRowDTO struct {
	Ord        int               `json:"ord"`
	IndexPp    string            `json:"index_pp"`
	PlanMsk    *domain.LocalTime `json:"plan_msk"`
	Candidates []SFCandidateDTO  `json:"candidates"`
}

// PreparePlanResult — ответ prepare: токен + с.ф.-строки с кандидатами + превью.
type PreparePlanResult struct {
	Token    string     `json:"token"`
	PlanCode string     `json:"plan_code"`
	Filename string     `json:"filename"`
	SF       []SFRowDTO `json:"sf"`
	Nitki    int        `json:"nitki"`
	Matched  int        `json:"matched"`
}

// Prepare — фаза A. Возвращает токен (для confirm) и с.ф.-строки с кандидатами.
// Снимок не изменяется. Нет с.ф. → SF пустой (фронт сразу зовёт confirm без диалога).
func (p *PlanProcessor) Prepare(ctx context.Context, planCode, filename string, data []byte) (PreparePlanResult, error) {
	prof, err := plan.ResolveProfile(planCode)
	if err != nil {
		return PreparePlanResult{}, err
	}
	target := p.dir.TargetNaznach(planCode)
	if len(target) == 0 {
		return PreparePlanResult{}, fmt.Errorf("для плана %q нет целевых площадок в ports (plan_code)", planCode)
	}
	path, err := p.save(planCode, data)
	if err != nil {
		return PreparePlanResult{}, err
	}
	doc, err := plan.ParseFile(path, planCode)
	if err != nil {
		return PreparePlanResult{}, fmt.Errorf("разбор плана: %w", err)
	}

	records := p.actual.All()
	agg := planmatch.Aggregate(records, target)
	matches := planmatch.Match(doc.Nitki, agg, prof.MatchRequiresNaznach)
	used := planmatch.UsedIdDisl(matches)

	sf, err := p.loadSF(ctx)
	if err != nil {
		return PreparePlanResult{}, err
	}

	var sfRows []SFRowDTO
	for i, n := range doc.Nitki {
		if !n.IsSf {
			continue
		}
		cands := planmatch.SFCandidates(synonymOf(n.IndexPp), sf, records, target, used)
		sfRows = append(sfRows, SFRowDTO{
			Ord:        i,
			IndexPp:    n.IndexPp,
			PlanMsk:    localPtr(n.PlanMsk),
			Candidates: toCandidateDTO(cands),
		})
	}

	matched, trains := countPlan(doc, matches)
	tok := p.pending.put(pendingPlan{planCode: planCode, filename: filename, data: data})
	return PreparePlanResult{
		Token: tok, PlanCode: planCode, Filename: filename,
		SF: sfRows, Nitki: trains, Matched: matched,
	}, nil
}

// Confirm — фаза B. selections: ord с.ф.-нитки → выбранные id_disl. Пустой выбор для
// с.ф. → остаётся пустой (решение A). Ре-валидация против текущего снимка; исчезнувшие
// группы пропускаются; один id_disl не уходит в две с.ф.
func (p *PlanProcessor) Confirm(ctx context.Context, token string, selections map[int][]string) (PlanProcessResult, error) {
	pend, ok := p.pending.take(token)
	if !ok {
		return PlanProcessResult{}, fmt.Errorf("токен подготовки не найден или истёк — перезагрузите план")
	}
	planCode := pend.planCode
	prof, err := plan.ResolveProfile(planCode)
	if err != nil {
		return PlanProcessResult{}, err
	}
	target := p.dir.TargetNaznach(planCode)
	if len(target) == 0 {
		return PlanProcessResult{}, fmt.Errorf("для плана %q нет целевых площадок", planCode)
	}
	path, err := p.save(planCode, pend.data)
	if err != nil {
		return PlanProcessResult{}, err
	}
	doc, err := plan.ParseFile(path, planCode)
	if err != nil {
		return PlanProcessResult{}, fmt.Errorf("разбор плана: %w", err)
	}

	records := p.actual.All()
	agg := planmatch.Aggregate(records, target)
	matches := planmatch.Match(doc.Nitki, agg, prof.MatchRequiresNaznach)

	used := planmatch.UsedIdDisl(matches)
	sf, err := p.loadSF(ctx)
	if err != nil {
		return PlanProcessResult{}, err
	}
	for i, n := range doc.Nitki {
		if !n.IsSf {
			continue
		}
		sel := selections[i]
		if len(sel) == 0 {
			continue // отмена/без выбора
		}
		byID := map[string]planmatch.SFGroup{}
		for _, g := range planmatch.SFCandidates(synonymOf(n.IndexPp), sf, records, target, used) {
			byID[g.IdDisl] = g
		}
		var vagons []string
		var subs []planmatch.SubGroup
		for _, id := range sel {
			g, ok := byID[id]
			if !ok {
				continue // группа исчезла/занята — пропускаем (окно рассогласования)
			}
			vagons = append(vagons, g.Vagons...)
			subs = append(subs, g.SubGroups...)
			used[id] = struct{}{} // без двойного назначения между с.ф.
		}
		if len(vagons) > 0 {
			matches[i].Matched = true
			matches[i].Vagons = vagons
			matches[i].SubGroups = subs // «Состав» и станция нитки с.ф. в сетке
		}
	}

	stats, err := p.applyAndSwap(ctx, records, matches, target)
	if err != nil {
		return PlanProcessResult{}, err
	}
	matched, trains := countPlan(doc, matches)
	if err := p.saveGrid(ctx, planCode, pend.filename, doc, matches, stats.Stamped); err != nil {
		return PlanProcessResult{}, err
	}
	return PlanProcessResult{
		Filename: pend.filename, PlanCode: planCode,
		Nitki: trains, Matched: matched, Stamped: stats.Stamped, Cleared: stats.Cleared,
	}, nil
}

// ── Общие хелперы (используют и ProcessFile, и Confirm) ─────────────────────

// applyAndSwap применяет матч к снимку и атомарно подменяет (вариант Б) + перечитывает кэш.
func (p *PlanProcessor) applyAndSwap(ctx context.Context, records []domain.Dislocation, matches []planmatch.NitkaMatch, target map[string]struct{}) (planmatch.ApplyStats, error) {
	out, stats := planmatch.Apply(records, matches, target, clock.Now())
	if err := p.repo.ReplaceActual(ctx, out); err != nil {
		return planmatch.ApplyStats{}, fmt.Errorf("замена снимка: %w", err)
	}
	if p.actual != nil {
		if err := p.actual.Load(ctx); err != nil {
			return planmatch.ApplyStats{}, fmt.Errorf("перечитывание актуальной мапы: %w", err)
		}
	}
	return stats, nil
}

// countPlan считает ниток-поездов (без «Остатка») и сколько сопоставлено.
func countPlan(doc *plan.PlanDoc, matches []planmatch.NitkaMatch) (matched, trains int) {
	for i, m := range matches {
		if !doc.Nitki[i].IsOstatok {
			trains++
		}
		if m.Matched {
			matched++
		}
	}
	return matched, trains
}

// buildGridNitki собирает доменные нитки сетки плана из разбора + результата матча.
func buildGridNitki(planCode string, doc *plan.PlanDoc, matches []planmatch.NitkaMatch) []domain.PlanNitka {
	nitki := make([]domain.PlanNitka, len(doc.Nitki))
	for i, n := range doc.Nitki {
		nitki[i] = domain.PlanNitka{
			PlanCode:      planCode,
			Ord:           i,
			Index:         n.Index,
			IndexPp:       n.IndexPp,
			StationOper:   planmatch.StationOperOf(matches[i].SubGroups),
			PlanMsk:       localPtr(n.PlanMsk),
			PlanJd:        localPtr(n.PlanJd),
			FactMsk:       localPtr(n.FactMsk),
			Otkl:          n.Otkl,
			Wagons:        n.Wagons,
			Activ:         n.Activ,
			Ports:         toDomainPorts(n.Ports),
			Sostav:        planmatch.FormatSostav(matches[i].SubGroups),
			Comment:       n.Comment,
			Matched:       matches[i].Matched,
			MatchedWagons: len(matches[i].Vagons),
			IsOstatok:     n.IsOstatok,
			IsSf:          n.IsSf,
		}
	}
	return nitki
}

// saveGrid сохраняет сетку плана (заголовок + нитки) для фронта; nil planRepo → no-op.
func (p *PlanProcessor) saveGrid(ctx context.Context, planCode, filename string, doc *plan.PlanDoc, matches []planmatch.NitkaMatch, stamped int) error {
	if p.planRepo == nil {
		return nil
	}
	matched, trains := countPlan(doc, matches)
	now := clock.Now()
	header := domain.Plan{
		PlanCode: planCode, SourceFile: filename, LoadedAt: &now,
		Nitki: trains, Matched: matched, Stamped: stamped,
	}
	if _, err := p.planRepo.SavePlan(ctx, header, buildGridNitki(planCode, doc, matches)); err != nil {
		return fmt.Errorf("сохранение сетки плана: %w", err)
	}
	return nil
}

// loadSF грузит справочник sf и конвертирует в тип движка кандидатов.
func (p *PlanProcessor) loadSF(ctx context.Context) ([]planmatch.SFRecord, error) {
	if p.planRepo == nil {
		return nil, nil
	}
	recs, err := p.planRepo.ListSF(ctx)
	if err != nil {
		return nil, fmt.Errorf("выборка sf: %w", err)
	}
	out := make([]planmatch.SFRecord, len(recs))
	for i, r := range recs {
		out[i] = planmatch.SFRecord{Sinonim: r.Sinonim, Station: r.Station, Quantity: r.Quantity}
	}
	return out, nil
}

// synonymOf извлекает синоним (ВЕРХНИЙ регистр) из index_pp с.ф.: «с.ф.БИКИН» → «БИКИН».
func synonymOf(indexPp string) string {
	up := strings.ToUpper(strings.TrimSpace(indexPp))
	return strings.TrimSpace(strings.TrimPrefix(strings.ReplaceAll(up, ".", ""), "СФ"))
}

// toCandidateDTO переводит группы-кандидаты в DTO ответа prepare.
func toCandidateDTO(gs []planmatch.SFGroup) []SFCandidateDTO {
	out := make([]SFCandidateDTO, len(gs))
	for i, g := range gs {
		date := ""
		if g.DateOp != nil && !g.DateOp.IsZero() {
			date = g.DateOp.Time().Format("2006-01-02")
		}
		out[i] = SFCandidateDTO{
			IdDisl: g.IdDisl, Station: g.StationOper, Index: g.Index,
			Date: date, Quantity: g.Quantity, Sostav: planmatch.FormatSostav(g.SubGroups), Vagons: g.Vagons,
		}
	}
	return out
}
