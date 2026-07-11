// Package planmatch — ядро сопоставления вагонов дислокации с нитками плана
// подвода. Чистая доменная логика: на вход — записи дислокации, целевой набор
// площадок и нитки плана; на выход — какой нитке какая агрегация вагонов
// соответствует и каким вагонам проставить плановое прибытие (IndexPp/PlanMsk).
//
// Перенос 1:1 из эталона GTport (gtlogic .../plan_utils.go + dislocation_plan.go):
// агрегация в три карты по индексам × IdDisl с подгруппами, выбор лучшей агрегации
// по базовому индексу + скоринг + количественная валидация. Порт-специфику (набор
// «наших» площадок) движок получает параметром — не хардкодит.
//
// Без БД и кэшей: пакет тестируется на синтетических данных и переиспользуется как
// print-only проверкой (cmd/planrun), так и боевым write-back (отдельный слой).
package planmatch

import (
	"fmt"
	"strings"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/parser/plan"
)

// Эталонные пороги количественной валидации (GTport, без изменений).
const (
	maxTrainSize    = 75 // максимальный разумный размер поезда, вагонов
	activThreshold  = 15 // при Activ ≥ порога поезд должен быть не меньше порога
	unknownFallback = "UNKNOWN"
)

// SubGroup — подгруппа второго уровня внутри агрегации: вагоны одного
// IndexMain/Naznach/Sms1/GruzpolS. Quantity — сколько вагонов в подгруппе.
type SubGroup struct {
	Key         string
	IndexMain   string
	Naznach     string
	Sms1        string
	GruzpolS    string
	Quantity    int
	StationOper string
	DateOp      *domain.LocalTime
	IdDisl      string
}

// Aggregation — «поезд»: все вагоны одной операции (IdDisl) под одним индексом,
// сгруппированные в подгруппы. TotalCount — суммарное число вагонов.
type Aggregation struct {
	Index      string
	IdDisl     string
	TotalCount int
	SubGroups  []SubGroup
}

// AggResult — три карты агрегации (ключ "<index>|<IdDisl>") плюс отфильтрованные по
// принадлежности плану записи (нужны для выборки вагонов при записи результата).
type AggResult struct {
	ByIndex     map[string]Aggregation // ключ = "Index|IdDisl"
	ByIndexLast map[string]Aggregation // ключ = "IndexLast|IdDisl"
	ByIndexMain map[string]Aggregation // ключ = "IndexMain|IdDisl"

	records []domain.Dislocation // отфильтрованные «наши» записи (для write-back)
	target  map[string]struct{}  // набор целевых площадок (NameS)
}

// NitkaMatch — результат сопоставления одной нитки.
type NitkaMatch struct {
	Nitka     plan.PlanNitka
	Matched   bool
	Source    string // "by_index" | "by_index_last" | "by_index_main"
	Index     string // индекс победившей агрегации
	IdDisl    string
	MaWagons  int     // число «наших» вагонов победителя
	Score     float64 // балл победителя
	SubGroups []SubGroup
	Vagons    []string // вагоны к простановке IndexPp/PlanMsk/PlanJd
}

// isTarget — принадлежит ли запись плану: Naznach ИЛИ GruzpolS входит в целевой
// набор площадок. Замена эталонного isMaTargetNaznachOrGruzpolS (набор — из данных).
func isTarget(naznach, gruzpolS string, target map[string]struct{}) bool {
	if _, ok := target[strings.TrimSpace(naznach)]; ok {
		return true
	}
	if _, ok := target[strings.TrimSpace(gruzpolS)]; ok {
		return true
	}
	return false
}

func emptyToDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

// baseIndex — базовый индекс: первые 11 из 13 символов ("7438-011-1234" → "7438-011-12").
// По нему нитка сопоставляется с агрегацией (эталонный getBaseIndex).
func baseIndex(index string) string {
	if len(index) >= 11 {
		return index[:11]
	}
	return index
}

// Aggregate строит три карты агрегации из записей дислокации, отобранных по
// принадлежности плану (target). Перенос aggregateDislocationDataWithHierarchy.
func Aggregate(records []domain.Dislocation, target map[string]struct{}) AggResult {
	res := AggResult{
		ByIndex:     map[string]Aggregation{},
		ByIndexLast: map[string]Aggregation{},
		ByIndexMain: map[string]Aggregation{},
		target:      target,
	}
	// Оставляем только «наши» записи — на них работает и агрегация, и запись результата.
	res.records = make([]domain.Dislocation, 0, len(records))
	for _, r := range records {
		if isTarget(r.Naznach, r.GruzpolS, target) {
			res.records = append(res.records, r)
		}
	}

	// 1. По Index + IdDisl.
	for _, r := range res.records {
		if r.IdDisl == "" || r.Index == "" {
			continue
		}
		addToAggregation(res.ByIndex, r.Index+"|"+r.IdDisl, r, r.Index, r.IdDisl)
	}
	// 2. По IndexLast + IdDisl (только если IndexLast ≠ Index).
	for _, r := range res.records {
		if r.IdDisl == "" || r.IndexLast == "" || r.Index == r.IndexLast {
			continue
		}
		addToAggregation(res.ByIndexLast, r.IndexLast+"|"+r.IdDisl, r, r.IndexLast, r.IdDisl)
	}
	// 3. По IndexMain + IdDisl (только если IndexMain ∉ {Index, IndexLast}).
	for _, r := range res.records {
		if r.IdDisl == "" || r.IndexMain == "" || r.IndexMain == r.Index || r.IndexMain == r.IndexLast {
			continue
		}
		addToAggregation(res.ByIndexMain, r.IndexMain+"|"+r.IdDisl, r, r.IndexMain, r.IdDisl)
	}
	return res
}

// addToAggregation добавляет запись в агрегацию с подгруппами (aggregateWithSubGroups).
func addToAggregation(m map[string]Aggregation, key string, r domain.Dislocation, index, idDisl string) {
	agg, ok := m[key]
	if !ok {
		agg = Aggregation{Index: index, IdDisl: idDisl}
	}

	sgKey := fmt.Sprintf("%s|%s|%s|%s",
		emptyToDefault(r.IndexMain, unknownFallback),
		emptyToDefault(r.Naznach, unknownFallback),
		emptyToDefault(r.Sms1, unknownFallback),
		emptyToDefault(r.GruzpolS, unknownFallback))

	found := -1
	for i := range agg.SubGroups {
		if agg.SubGroups[i].Key == sgKey {
			found = i
			break
		}
	}
	if found == -1 {
		agg.SubGroups = append(agg.SubGroups, SubGroup{
			Key:         sgKey,
			IndexMain:   emptyToDefault(r.IndexMain, unknownFallback),
			Naznach:     emptyToDefault(r.Naznach, unknownFallback),
			Sms1:        emptyToDefault(r.Sms1, unknownFallback),
			GruzpolS:    emptyToDefault(r.GruzpolS, unknownFallback),
			Quantity:    1,
			StationOper: emptyToDefault(r.StationOper, unknownFallback),
			DateOp:      r.DateOp,
			IdDisl:      idDisl,
		})
	} else {
		agg.SubGroups[found].Quantity++
	}
	agg.TotalCount++
	m[key] = agg
}

// maWagons — число «наших» вагонов агрегации (сумма Quantity целевых подгрупп).
// Так как в агрегацию попадают только «наши» записи, это совпадает с TotalCount,
// но считаем явно — как в эталоне (устойчиво к смене правила отбора).
func (a Aggregation) maWagons(target map[string]struct{}) int {
	n := 0
	for _, sg := range a.SubGroups {
		if isTarget(sg.Naznach, sg.GruzpolS, target) {
			n += sg.Quantity
		}
	}
	return n
}

// isValid — годна ли агрегация: есть «наши» подгруппы и число «наших» вагонов
// проходит количественный фильтр (isValidAggregation + isValidAggregationByQuantity).
func isValid(a Aggregation, activValue int, target map[string]struct{}) bool {
	ma := a.maWagons(target)
	if ma == 0 {
		return false
	}
	if ma > maxTrainSize {
		return false
	}
	if activValue >= activThreshold {
		return ma >= activThreshold
	}
	return ma >= 1
}

// score — балл агрегации (calculateTrainScore). maCount — «наши» вагоны.
func score(maCount, subGroupCount, activValue int) float64 {
	if activValue == 0 {
		// Без плана приоритет у больших поездов.
		return float64(maCount) / 75.0 * 100.0
	}
	// 1. Точность к плановому Activ (до 50).
	var exact float64
	switch {
	case maCount == activValue:
		exact = 50.0
	case maCount > activValue:
		excess := float64(maCount-activValue) / float64(activValue)
		if excess > 0.5 {
			excess = 0.5
		}
		exact = 40.0 * (1.0 - excess)
	default:
		exact = 50.0 * (float64(maCount) / float64(activValue))
	}
	// 2. Размер поезда (до 30).
	sizeScore := float64(maCount) / 75.0 * 30.0
	// 3. Мало подгрупп = лучше (до 20).
	var sg float64
	switch {
	case subGroupCount == 1:
		sg = 20.0
	case subGroupCount <= 3:
		sg = 15.0
	case subGroupCount <= 5:
		sg = 10.0
	default:
		sg = 5.0
	}
	return exact + sizeScore + sg
}

// Match сопоставляет каждую нитку с лучшей агрегацией и вычисляет вагоны к записи.
// requiresNaznach — доп. условие NK (совпадение Naznach при выборке вагонов).
func Match(nitki []plan.PlanNitka, agg AggResult, requiresNaznach bool) []NitkaMatch {
	out := make([]NitkaMatch, 0, len(nitki))
	for _, n := range nitki {
		m := NitkaMatch{Nitka: n}
		if n.IsSf {
			// с.ф. индексно не матчатся — группы выбирает пользователь (отдельный поток);
			// пустой матч сохраняет соответствие 1:1 с nitki.
			out = append(out, m)
			continue
		}

		best, src := findBest(n.IndexPp, agg, n.Activ)
		if best != nil {
			m.Matched = true
			m.Source = src
			m.Index = best.Index
			m.IdDisl = best.IdDisl
			m.MaWagons = best.maWagons(agg.target)
			m.Score = score(m.MaWagons, len(best.SubGroups), n.Activ)
			m.SubGroups = best.SubGroups
			m.Vagons = collectVagons(agg, *best, requiresNaznach)
		}
		out = append(out, m)
	}
	return out
}

// findBest перебирает карты ByIndex→ByIndexLast→ByIndexMain до первого успеха и в
// каждой выбирает агрегацию с максимальным баллом среди годных по базовому индексу.
func findBest(indexPp string, agg AggResult, activValue int) (*Aggregation, string) {
	order := []struct {
		m   map[string]Aggregation
		src string
	}{
		{agg.ByIndex, "by_index"},
		{agg.ByIndexLast, "by_index_last"},
		{agg.ByIndexMain, "by_index_main"},
	}
	for _, o := range order {
		if best := findBestInMap(indexPp, o.m, activValue, agg.target); best != nil {
			return best, o.src
		}
	}
	return nil, ""
}

func findBestInMap(indexPp string, m map[string]Aggregation, activValue int, target map[string]struct{}) *Aggregation {
	base := baseIndex(indexPp)
	var best *Aggregation
	var bestScore float64 = -1
	for key, agg := range m {
		parts := strings.SplitN(key, "|", 2)
		if len(parts) < 2 {
			continue
		}
		if baseIndex(parts[0]) != base {
			continue
		}
		if !isValid(agg, activValue, target) {
			continue
		}
		s := score(agg.maWagons(target), len(agg.SubGroups), activValue)
		if best == nil || s > bestScore {
			a := agg // копия для взятия адреса
			best = &a
			bestScore = s
		}
	}
	return best
}

// collectVagons выбирает вагоны победившей агрегации для простановки плана
// (updateWagonsForSubGroup + shouldUpdateWagon): Status≠10, площадка «наша» по
// Naznach (эталонный isTargetNaznachForPlan — write-back сверяет именно Naznach,
// тогда как агрегация брала Naznach ИЛИ GruzpolS), совпадение IdDisl+IndexMain с
// подгруппой; для NK дополнительно совпадение Naznach с подгруппой.
func collectVagons(agg AggResult, best Aggregation, requiresNaznach bool) []string {
	seen := map[string]struct{}{}
	var vagons []string
	for _, r := range agg.records {
		if r.Status != nil && *r.Status == 10 {
			continue
		}
		if r.Vagon == "" {
			continue
		}
		if _, ok := agg.target[strings.TrimSpace(r.Naznach)]; !ok {
			continue // Naznach не целевой — write-back эталона такой вагон не трогает
		}
		for _, sg := range best.SubGroups {
			if r.IdDisl != sg.IdDisl || r.IndexMain != sg.IndexMain {
				continue
			}
			if requiresNaznach && r.Naznach != sg.Naznach {
				continue
			}
			if _, dup := seen[r.Vagon]; dup {
				break
			}
			seen[r.Vagon] = struct{}{}
			vagons = append(vagons, r.Vagon)
			break
		}
	}
	return vagons
}
