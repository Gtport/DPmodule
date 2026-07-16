package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

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
	Key      string   `json:"key"`      // уникальный идентификатор группы для выбора (id_disl не уникален)
	IdDisl   string   `json:"id_disl"`  // справочно
	Station  string   `json:"station"`  // станция текущей операции (для уехавших — где поезд сейчас)
	Departed bool     `json:"departed"` // true: покинул станцию формирования (найден по префиксу индекса)
	Formed   bool     `json:"formed"`   // true: сформирован (реальный индекс), но ещё на станции формирования
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

// PrepareRecalc запускает пересчёт плановых данных на ТЕКУЩЕМ снимке дислокации по
// сохранённой сетке плана (planID) — без повторной загрузки/разбора Excel. Нитки
// восстанавливаются из БД (с уже «запечёнными» ручными правками индексов), матч
// гоняется заново; дальше — тот же Revalidate/Confirm по токену. Возвращает токен +
// превью (с.ф.-строки + проблемные нитки). Снимок не изменяется.
func (p *PlanProcessor) PrepareRecalc(ctx context.Context, planID int64) (PreparePlanResult, error) {
	if p.planRepo == nil {
		return PreparePlanResult{}, fmt.Errorf("хранение плана не подключено")
	}
	header, storedNitki, err := p.planRepo.GetPlanByID(ctx, planID)
	if err != nil {
		return PreparePlanResult{}, err
	}
	if header.PlanCode == "" {
		return PreparePlanResult{}, fmt.Errorf("сохранённый план не найден (id=%d)", planID)
	}
	planCode := header.PlanCode
	prof, err := plan.ResolveProfile(planCode)
	if err != nil {
		return PreparePlanResult{}, err
	}
	target := p.dir.TargetNaznach(planCode)
	if len(target) == 0 {
		return PreparePlanResult{}, fmt.Errorf("для плана %q нет целевых площадок", planCode)
	}
	if err := p.ensureDislFresh(ctx); err != nil {
		return PreparePlanResult{}, err
	}

	doc := &plan.PlanDoc{
		PlanCode:   planCode,
		SourceFile: recalcSourceName(header.SourceFile), // пометка «(пересчёт)» → видна в истории/журнале
		Nitki:      storedNitkiToPlan(storedNitki),
	}
	prev, err := p.buildPreview(ctx, doc.Nitki, prof.MatchRequiresNaznach, target)
	if err != nil {
		return PreparePlanResult{}, err
	}
	tok := p.pending.put(pendingPlan{planCode: planCode, filename: doc.SourceFile, doc: doc})
	prev.Token = tok
	prev.PlanCode = planCode
	prev.Filename = doc.SourceFile
	return prev, nil
}

const recalcSuffix = "(пересчёт)"

// recalcSourceName формирует имя источника пересчитанной сетки: базовое имя + пометка
// «(пересчёт)», без накопления при повторных пересчётах.
func recalcSourceName(src string) string {
	base := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(src), recalcSuffix))
	return strings.TrimSpace(base + " " + recalcSuffix)
}

// storedNitkiToPlan восстанавливает нитки парсера из сохранённой сетки плана (БД) для
// пересчёта без повторного разбора Excel. Берём «сырьё» нитки (индекс/activ/is_sf/
// время/порты); состав и вагоны матч пересчитает заново на текущем снимке.
func storedNitkiToPlan(nitki []domain.PlanNitka) []plan.PlanNitka {
	out := make([]plan.PlanNitka, len(nitki))
	for i, n := range nitki {
		out[i] = plan.PlanNitka{
			Index:     n.Index,
			IndexPp:   n.IndexPp,
			PlanJd:    localToTime(n.PlanJd),
			PlanMsk:   localToTime(n.PlanMsk),
			FactMsk:   localToTime(n.FactMsk),
			Otkl:      n.Otkl,
			PlanRaw:   n.PlanRaw,
			Wagons:    n.Wagons,
			Activ:     n.Activ,
			Ports:     planPortsFromDomain(n.Ports),
			Comment:   n.Comment,
			IsOstatok: n.IsOstatok,
			IsSf:      n.IsSf,
		}
	}
	return out
}

// localToTime разворачивает *domain.LocalTime в naive time.Time (nil → нулевое).
func localToTime(t *domain.LocalTime) time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.Time()
}

// planPortsFromDomain переводит доменные ячейки портов в ячейки парсера (обратная
// сторона toDomainPorts) — для восстановления нитки из сохранённой сетки.
func planPortsFromDomain(cells []domain.PortCell) []plan.PortCell {
	if len(cells) == 0 {
		return nil
	}
	out := make([]plan.PortCell, len(cells))
	for i, c := range cells {
		out[i] = plan.PortCell{Label: c.Label, Count: c.Count, IsOur: c.IsOur}
	}
	return out
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

	kod4 := p.sfKod4(sf)
	sfRows := []SFRowDTO{} // не nil — фронт ждёт массив (JSON [] вместо null)
	for i, n := range nitki {
		if !n.IsSf {
			continue
		}
		cands := planmatch.SFCandidates(synonymOf(n.IndexPp), sf, records, target, used, kod4)
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
	out := []ProblemRowDTO{} // не nil — фронт ждёт массив (JSON [] вместо null)
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
	kod4 := p.sfKod4(sf)
	for i, n := range effDoc.Nitki {
		if !n.IsSf {
			continue // в т.ч. бывшая с.ф. с вписанным индексом — уже сматчена по индексу
		}
		sel := selections[i]
		if len(sel) == 0 {
			continue // отмена/без выбора
		}
		byKey := map[string]planmatch.SFGroup{}
		for _, g := range planmatch.SFCandidates(synonymOf(n.IndexPp), sf, records, target, used, kod4) {
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
	applyStage4(out, p.dir, p.cfg, 0) // план поставил новый PlanMsk → пересчёт прогноза ProgMsk
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
			PlanRaw:       n.PlanRaw,
			Wagons:        n.Wagons,
			Activ:         n.Activ,
			Ports:         toDomainPorts(n.Ports),
			Sostav:        planmatch.FormatSostav(matches[i].SubGroups),
			Comment:       n.Comment,
			Matched:       matches[i].Matched,
			MatchedWagons: matchedWagons(matches[i]),
			IsOstatok:     n.IsOstatok,
			IsSf:          n.IsSf,
		}
	}
	return nitki
}

// matchedWagons — сопоставлено вагонов нитке для сетки («Кол-во»): сумма подгрупп
// победителя — согласовано с «Составом» и включает ПРИБЫВШИЕ вагоны (им план не
// штампуется — collectVagons их исключает, поэтому len(Vagons) для прибывшего
// поезда 0, хотя сопоставление есть). Fallback — вагоны к простановке (с.ф. и пр.).
func matchedWagons(m planmatch.NitkaMatch) int {
	sum := 0
	for _, sg := range m.SubGroups {
		sum += sg.Quantity
	}
	if sum == 0 {
		return len(m.Vagons)
	}
	return sum
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
		PlanDate: planDateOnly(doc), // «на какую дату план» — для списка/фильтра
		Nitki:    trains, Matched: matched, Stamped: stamped,
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

// sfKod4 строит мапу «станция формирования → все её kod_4 строками» для поиска
// уехавших сборных по префиксу индекса (АААА-БББ-ВВВВ). Кодов может быть несколько
// (парки крупной станции с одним именем). Станции не в справочнике пропускаются
// (для них ловятся только стоящие кандидаты).
func (p *PlanProcessor) sfKod4(sf []planmatch.SFRecord) map[string][]string {
	out := map[string][]string{}
	for _, r := range sf {
		name := strings.TrimSpace(r.Station)
		if name == "" {
			continue
		}
		if _, ok := out[name]; ok {
			continue
		}
		for _, k4 := range p.dir.Kod4sByStationName(name) {
			out[name] = append(out[name], strconv.Itoa(k4))
		}
	}
	return out
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
			Key: g.Key, IdDisl: g.IdDisl, Station: g.StationOper,
			Departed: g.Departed, Formed: g.Formed,
			Index: g.Index, Date: date, Quantity: g.Quantity,
			Sostav: planmatch.FormatSostav(g.SubGroups), Vagons: g.Vagons,
		}
	}
	return out
}
