package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/parser/plan"
	"github.com/Gtport/DPmodule/internal/service/planmatch"
)

// ErrPendingNotFound — токен подготовки неизвестен/просрочен (диалог висел дольше TTL,
// либо бэкенд перезапускался — pending в памяти). Хендлер отдаёт по нему 410 Gone,
// фронт закрывает окно и прозрачно перезагружает план.
var ErrPendingNotFound = errors.New("токен подготовки не найден или истёк — перезагрузите план")

// ── Двухфазная обработка плана с выбором групп с.ф. пользователем ───────────
//
// Фаза A (Prepare): разобрать план, сматчить обычные нитки, посчитать кандидатов
// для каждой с.ф. Снимок НЕ трогаем, кладём файл в pending по токену.
// Фаза B (Confirm): по токену заново разбираем и матчим против ТЕКУЩЕГО снимка,
// заполняем выбранные группы с.ф., применяем всё одним свопом. Так окно
// рассогласования (снимок мог пересобраться между фазами) закрывается на confirm.

// SFCandidateDTO — группа-кандидат вагонов для с.ф. (для диалога выбора на фронте).
type SFCandidateDTO struct {
	Key      string   `json:"key"`     // уникальный идентификатор группы для выбора (id_disl не уникален)
	IdDisl   string   `json:"id_disl"` // справочно
	Station  string   `json:"station"`
	Index    string   `json:"index"`
	Date     string   `json:"date"`
	Quantity int      `json:"quantity"`
	Sostav   string   `json:"sostav"` // «Состав» группы — как у обычных ниток (FormatSostav)
	Vagons   []string `json:"vagons"`
}

// SFRowDTO — одна с.ф.-нитка плана с её кандидатами. Ports — столбцы терминалов из
// строки плана (сколько вагонов запланировано на наши причалы) для заголовка диалога.
type SFRowDTO struct {
	Ord        int               `json:"ord"`
	IndexPp    string            `json:"index_pp"`
	PlanMsk    *domain.LocalTime `json:"plan_msk"`
	Ports      []domain.PortCell `json:"ports"`
	Candidates []SFCandidateDTO  `json:"candidates"`
}

// ProblemRowDTO — обычная нитка плана (не с.ф., не «Остаток») с Activ>0, которой матч
// не нашёл ни одного вагона: вероятна опечатка в индексе, поезд ещё не в дислокации,
// либо уже прибыл. Оператору предлагается вписать корректный индекс (форма 4-3-4) и
// перепроверить через Revalidate. Ports/Activ — контекст для оператора в диалоге.
type ProblemRowDTO struct {
	Ord     int               `json:"ord"`
	IndexPp string            `json:"index_pp"`
	PlanMsk *domain.LocalTime `json:"plan_msk"`
	Activ   int               `json:"activ"`
	Ports   []domain.PortCell `json:"ports"`
}

// PreparePlanResult — ответ prepare/revalidate: токен + с.ф.-строки с кандидатами +
// проблемные нитки (Activ>0 без вагонов) + счётчики превью. Снимок не изменяется.
type PreparePlanResult struct {
	Token    string          `json:"token"`
	PlanCode string          `json:"plan_code"`
	Filename string          `json:"filename"`
	SF       []SFRowDTO      `json:"sf"`
	Problems []ProblemRowDTO `json:"problems"`
	Nitki    int             `json:"nitki"`
	Matched  int             `json:"matched"`
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
	if err := p.ensureDislFresh(ctx); err != nil {
		return PreparePlanResult{}, err
	}
	path, err := p.save(planCode, data)
	if err != nil {
		return PreparePlanResult{}, err
	}
	doc, err := plan.ParseFile(path, planCode)
	if err != nil {
		return PreparePlanResult{}, fmt.Errorf("разбор плана: %w", err)
	}

	prev, err := p.buildPreview(ctx, doc.Nitki, prof.MatchRequiresNaznach, target)
	if err != nil {
		return PreparePlanResult{}, err
	}

	tok := p.pending.put(pendingPlan{planCode: planCode, filename: filename, doc: doc})
	prev.Token = tok
	prev.PlanCode = planCode
	prev.Filename = filename
	return prev, nil
}

// Revalidate — сухой прогон между Prepare и Confirm: пересобирает превью с учётом
// вписанных оператором индексов (overrides: ord→индекс; с.ф. с вписанным индексом
// становится обычной ниткой и матчится по индексу). Снимок НЕ изменяется, токен НЕ
// расходуется (peek продлевает TTL). Возвращает обновлённые с.ф.-строки и проблемные
// нитки — оператор видит результат правок и правит дальше, пока не закоммитит Confirm.
func (p *PlanProcessor) Revalidate(ctx context.Context, token string, overrides map[int]string) (PreparePlanResult, error) {
	pend, ok := p.pending.peek(token)
	if !ok {
		return PreparePlanResult{}, ErrPendingNotFound
	}
	prof, err := plan.ResolveProfile(pend.planCode)
	if err != nil {
		return PreparePlanResult{}, err
	}
	target := p.dir.TargetNaznach(pend.planCode)
	if len(target) == 0 {
		return PreparePlanResult{}, fmt.Errorf("для плана %q нет целевых площадок", pend.planCode)
	}

	nitki := applyIndexOverrides(pend.doc.Nitki, overrides)
	prev, err := p.buildPreview(ctx, nitki, prof.MatchRequiresNaznach, target)
	if err != nil {
		return PreparePlanResult{}, err
	}
	prev.Token = token
	prev.PlanCode = pend.planCode
	prev.Filename = pend.filename
	return prev, nil
}

// buildPreview матчит нитки против ТЕКУЩЕГО снимка и собирает превью: с.ф.-строки с
// группами-кандидатами и проблемные обычные нитки (Activ>0 без вагонов). Снимок не
// изменяется. Общий код Prepare и Revalidate (Token/PlanCode/Filename проставляет
// вызывающий). nitki — уже с применёнными overrides (или исходные — для Prepare).
func (p *PlanProcessor) buildPreview(ctx context.Context, nitki []plan.PlanNitka, requiresNaznach bool, target map[string]struct{}) (PreparePlanResult, error) {
	records := p.actual.All()
	agg := planmatch.Aggregate(records, target)
	matches := planmatch.Match(nitki, agg, requiresNaznach)
	used := planmatch.UsedIdDisl(matches)

	sf, err := p.loadSF(ctx)
	if err != nil {
		return PreparePlanResult{}, err
	}

	var sfRows []SFRowDTO
	for i, n := range nitki {
		if !n.IsSf {
			continue
		}
		cands := planmatch.SFCandidates(synonymOf(n.IndexPp), sf, records, target, used)
		sfRows = append(sfRows, SFRowDTO{
			Ord:        i,
			IndexPp:    n.IndexPp,
			PlanMsk:    localPtr(n.PlanMsk),
			Ports:      toDomainPorts(n.Ports), // столбцы терминалов из строки плана
			Candidates: toCandidateDTO(cands),
		})
	}

	matched, trains := countPlanNitki(nitki, matches)
	return PreparePlanResult{
		SF: sfRows, Problems: problemRows(nitki, matches),
		Nitki: trains, Matched: matched,
	}, nil
}

// applyIndexOverrides возвращает копию ниток с переопределёнными индексами: для ord из
// overrides ставит вписанный оператором индекс (Index и IndexPp) и снимает флаг с.ф.
// (теперь нитка матчится по индексу, как обычная). Пустые/вне диапазона ord — пропуск.
// Исходные нитки (в pending.doc) НЕ мутируются — работаем на копии.
func applyIndexOverrides(nitki []plan.PlanNitka, overrides map[int]string) []plan.PlanNitka {
	if len(overrides) == 0 {
		return nitki
	}
	out := make([]plan.PlanNitka, len(nitki))
	copy(out, nitki)
	for ord, idx := range overrides {
		if ord < 0 || ord >= len(out) {
			continue
		}
		idx = strings.TrimSpace(idx)
		if idx == "" {
			continue
		}
		out[ord].Index = idx
		out[ord].IndexPp = idx
		out[ord].IsSf = false
	}
	return out
}

// problemRows собирает обычные нитки (не с.ф., не «Остаток») с Activ>0, которым матч
// не нашёл ни одного вагона — кандидаты на исправление индекса оператором.
func problemRows(nitki []plan.PlanNitka, matches []planmatch.NitkaMatch) []ProblemRowDTO {
	var out []ProblemRowDTO
	for i, n := range nitki {
		if n.IsSf || n.IsOstatok || n.Activ <= 0 {
			continue
		}
		if matches[i].Matched {
			continue
		}
		out = append(out, ProblemRowDTO{
			Ord: i, IndexPp: n.IndexPp, PlanMsk: localPtr(n.PlanMsk),
			Activ: n.Activ, Ports: toDomainPorts(n.Ports),
		})
	}
	return out
}

// Touch продлевает TTL токена подготовки, пока открыт диалог выбора с.ф. (heartbeat
// с фронта). Возвращает false, если токен уже неизвестен/просрочен.
func (p *PlanProcessor) Touch(token string) bool {
	return p.pending.touch(token)
}

// Confirm — фаза B. overrides: ord нитки → вписанный оператором индекс (с.ф.→реальный
// либо исправление опечатки; применяются ДО матча). selections: ord с.ф.-нитки →
// выбранные id_disl. Пустой выбор для с.ф. → остаётся пустой (решение A). Ре-валидация
// против текущего снимка; исчезнувшие группы пропускаются; один id_disl не уходит в две с.ф.
func (p *PlanProcessor) Confirm(ctx context.Context, token string, overrides map[int]string, selections map[int][]string) (PlanProcessResult, error) {
	pend, ok := p.pending.take(token)
	if !ok {
		return PlanProcessResult{}, ErrPendingNotFound
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
	// Эффективный документ: разобран на prepare (файл там же сохранён — повторно не
	// разбираем), нитки — с применёнными ручными правками индексов. pend.doc не мутируем.
	effDoc := *pend.doc
	effDoc.Nitki = applyIndexOverrides(pend.doc.Nitki, overrides)

	records := p.actual.All()
	agg := planmatch.Aggregate(records, target)
	matches := planmatch.Match(effDoc.Nitki, agg, prof.MatchRequiresNaznach)

	used := planmatch.UsedIdDisl(matches)
	sf, err := p.loadSF(ctx)
	if err != nil {
		return PlanProcessResult{}, err
	}
	for i, n := range effDoc.Nitki {
		if !n.IsSf {
			continue // в т.ч. бывшая с.ф. с вписанным индексом — уже сматчена по индексу
		}
		sel := selections[i]
		if len(sel) == 0 {
			continue // отмена/без выбора
		}
		byKey := map[string]planmatch.SFGroup{}
		for _, g := range planmatch.SFCandidates(synonymOf(n.IndexPp), sf, records, target, used) {
			byKey[g.Key] = g // ключ уникален; id_disl может совпадать у разных групп
		}
		var vagons []string
		var subs []planmatch.SubGroup
		for _, key := range sel {
			g, ok := byKey[key]
			if !ok {
				continue // группа исчезла/занята — пропускаем (окно рассогласования)
			}
			vagons = append(vagons, g.Vagons...)
			subs = append(subs, g.SubGroups...)
			if g.IdDisl != "" {
				used[g.IdDisl] = struct{}{} // исключить операцию из кандидатов следующих с.ф.
			}
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
	matched, trains := countPlan(&effDoc, matches)
	if err := p.saveGrid(ctx, planCode, pend.filename, &effDoc, matches, stats.Stamped); err != nil {
		return PlanProcessResult{}, err
	}

	p.journal.RecordPlanUpload(ctx, planCode, pend.filename, planDocDate(&effDoc), trains, matched, stats.Stamped, overrides)

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
	return countPlanNitki(doc.Nitki, matches)
}

// countPlanNitki — то же по срезу ниток (для превью с применёнными overrides, где
// отдельного PlanDoc нет).
func countPlanNitki(nitki []plan.PlanNitka, matches []planmatch.NitkaMatch) (matched, trains int) {
	for i, m := range matches {
		if !nitki[i].IsOstatok {
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
			Key: g.Key, IdDisl: g.IdDisl, Station: g.StationOper, Index: g.Index,
			Date: date, Quantity: g.Quantity, Sostav: planmatch.FormatSostav(g.SubGroups),
			Vagons: g.Vagons,
		}
	}
	return out
}
