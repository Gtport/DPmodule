package planmatch

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/parser/plan"
)

func targetSet(names ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(names))
	for _, n := range names {
		m[n] = struct{}{}
	}
	return m
}

func TestBaseIndex(t *testing.T) {
	assert.Equal(t, "7438-011-12", baseIndex("7438-011-1234")) // первые 11 из 13
	assert.Equal(t, "7438-011-12", baseIndex("7438-011-1299")) // отличаются 2 последних
	assert.Equal(t, "7438-011", baseIndex("7438-011"))         // короче 11 → как есть
	assert.Equal(t, "", baseIndex(""))
}

func TestScore(t *testing.T) {
	tgt := targetSet("АЭ")
	_ = tgt

	// Точное совпадение, 1 подгруппа: 50 + 30*(30/75) + 20 = 82.
	assert.InDelta(t, 82.0, score(30, 1, 30), 1e-9)

	// Недобор (поезд меньше плана): exact = 50 * (15/30) = 25; size = 30*(15/75)=6; sg=20 → 51.
	assert.InDelta(t, 51.0, score(15, 1, 30), 1e-9)

	// Избыток ≤50%: activ=20, count=30 → excess=0.5 → exact=40*(1-0.5)=20; size=30*(30/75)=12; sg=20 → 52.
	assert.InDelta(t, 52.0, score(30, 1, 20), 1e-9)

	// Избыток обрезается: activ=20, count=40 → excess=1.0→0.5 → exact=20; size=30*(40/75)=16; sg=20 → 56.
	assert.InDelta(t, 56.0, score(40, 1, 20), 1e-9)

	// Без плана (activ=0): TotalCount/75*100.
	assert.InDelta(t, 40.0, score(30, 3, 0), 1e-9)

	// Штраф за подгруппы: 1→20, 3→15, 5→10, >5→5.
	assert.InDelta(t, 50.0+12.0+15.0, score(30, 3, 30), 1e-9)
	assert.InDelta(t, 50.0+12.0+10.0, score(30, 5, 30), 1e-9)
	assert.InDelta(t, 50.0+12.0+5.0, score(30, 6, 30), 1e-9)
}

func TestIsValid(t *testing.T) {
	tgt := targetSet("АЭ")
	mk := func(count int) Aggregation {
		return Aggregation{SubGroups: []SubGroup{{Naznach: "АЭ", GruzpolS: "АЭ", Quantity: count}}}
	}

	assert.False(t, isValid(mk(76), 10, tgt), "больше 75 — брак")
	assert.True(t, isValid(mk(75), 10, tgt), "ровно 75 — годно")

	// activValue ≥ 15 → нужно ≥ 15.
	assert.False(t, isValid(mk(14), 15, tgt))
	assert.True(t, isValid(mk(15), 15, tgt))

	// activValue < 15 → достаточно ≥ 1.
	assert.True(t, isValid(mk(1), 5, tgt))
	assert.False(t, isValid(mk(0), 5, tgt), "нет наших вагонов — брак")

	// Подгруппа не целевая → нет наших вагонов → брак.
	notOurs := Aggregation{SubGroups: []SubGroup{{Naznach: "ЧУЖОЙ", GruzpolS: "ЧУЖОЙ", Quantity: 30}}}
	assert.False(t, isValid(notOurs, 10, tgt))
}

func disl(vagon, index, indexMain, naznach string) domain.Dislocation {
	return domain.Dislocation{
		Vagon: vagon, Index: index, IndexMain: indexMain,
		Naznach: naznach, GruzpolS: naznach, IdDisl: "D1",
	}
}

func TestMatch_BaseIndexAndVagons(t *testing.T) {
	tgt := targetSet("АЭ")
	records := []domain.Dislocation{
		disl("V1", "7438-011-1234", "7438-011-1234", "АЭ"),
		disl("V2", "7438-011-1234", "7438-011-1234", "АЭ"),
		disl("V3", "7438-011-1234", "7438-011-1234", "АЭ"),
		// Чужой поезд (другой базовый индекс) — не должен матчиться.
		{Vagon: "X1", Index: "9999-999-9999", IndexMain: "9999-999-9999", Naznach: "АЭ", GruzpolS: "АЭ", IdDisl: "D9"},
	}
	agg := Aggregate(records, tgt)

	// Нитка с тем же базовым индексом (последние 2 символа отличаются), Activ=3.
	nitki := []plan.PlanNitka{{Index: "7438-011-1299", IndexPp: "7438-011-1299", Activ: 3}}
	res := Match(nitki, agg, false, nil)

	require.Len(t, res, 1)
	m := res[0]
	assert.True(t, m.Matched)
	assert.Equal(t, "by_index", m.Source)
	assert.Equal(t, "7438-011-1234", m.Index)
	assert.Equal(t, 3, m.MaWagons)
	sort.Strings(m.Vagons)
	assert.Equal(t, []string{"V1", "V2", "V3"}, m.Vagons)
}

func TestMatch_NoMatchWhenBaseDiffers(t *testing.T) {
	tgt := targetSet("АЭ")
	agg := Aggregate([]domain.Dislocation{disl("V1", "7438-011-1234", "7438-011-1234", "АЭ")}, tgt)
	nitki := []plan.PlanNitka{{Index: "1111-011-1234", IndexPp: "1111-011-1234", Activ: 1}}
	res := Match(nitki, agg, false, nil)
	require.Len(t, res, 1)
	assert.False(t, res[0].Matched)
	assert.Empty(t, res[0].Vagons)
}

func TestMatch_Status10Excluded(t *testing.T) {
	tgt := targetSet("АЭ")
	st10 := 10
	records := []domain.Dislocation{
		disl("V1", "7438-011-1234", "7438-011-1234", "АЭ"),
		{Vagon: "V2", Index: "7438-011-1234", IndexMain: "7438-011-1234", Naznach: "АЭ", GruzpolS: "АЭ", IdDisl: "D1", Status: &st10},
	}
	agg := Aggregate(records, tgt)
	nitki := []plan.PlanNitka{{Index: "7438-011-1234", IndexPp: "7438-011-1234", Activ: 2}}
	res := Match(nitki, agg, false, nil)

	require.True(t, res[0].Matched)
	assert.Equal(t, 2, res[0].MaWagons, "статус 10 считается в агрегации (эталон)")
	assert.Equal(t, []string{"V1"}, res[0].Vagons, "но статус 10 не застолблён")
}

// NK-специфика: write-back сужает по Naznach. MA берёт вагоны шире (по IdDisl+IndexMain).
func TestMatch_RequiresNaznach_NKvsMA(t *testing.T) {
	tgt := targetSet("УТ-1", "ТА-Н")
	records := []domain.Dislocation{
		// Победная агрегация нитки (Index I1, Naznach УТ-1).
		{Vagon: "V1", Index: "7438-011-1111", IndexMain: "M", Naznach: "УТ-1", GruzpolS: "УТ-1", IdDisl: "D"},
		{Vagon: "V2", Index: "7438-011-1111", IndexMain: "M", Naznach: "УТ-1", GruzpolS: "УТ-1", IdDisl: "D"},
		// Тот же IdDisl+IndexMain, но другой индекс (I2, другой базовый) и Naznach ТА-Н.
		{Vagon: "V3", Index: "9999-011-1111", IndexMain: "M", Naznach: "ТА-Н", GruzpolS: "ТА-Н", IdDisl: "D"},
	}
	agg := Aggregate(records, tgt)
	nitki := []plan.PlanNitka{{Index: "7438-011-1150", IndexPp: "7438-011-1150", Activ: 2}}

	ma := Match(nitki, agg, false, nil)[0]
	require.True(t, ma.Matched)
	sort.Strings(ma.Vagons)
	assert.Equal(t, []string{"V1", "V2", "V3"}, ma.Vagons, "MA: по IdDisl+IndexMain, Naznach не сверяется")

	nk := Match(nitki, agg, true, nil)[0]
	require.True(t, nk.Matched)
	sort.Strings(nk.Vagons)
	assert.Equal(t, []string{"V1", "V2"}, nk.Vagons, "NK: только Naznach совпадающие с подгруппой")
}

// Ручная привязка (forced): нитка ждёт много (Activ ≥ порога), приехал недокомплект
// — автоматика отбраковывает по activThreshold, но выбор оператора матчит в обход
// фильтра. Кандидаты для выбора отдаёт CandidatesFor (включая отбракованных).
func TestMatch_ForcedBind(t *testing.T) {
	tgt := targetSet("АЭ")
	var records []domain.Dislocation
	for i := 0; i < 9; i++ { // наших только 9 — меньше activThreshold (15)
		records = append(records, disl("V"+string(rune('0'+i)), "9722-421-9838", "9722-421-9838", "АЭ"))
	}
	agg := Aggregate(records, tgt)
	nitki := []plan.PlanNitka{{Index: "9722-421-9838", IndexPp: "9722-421-9838", Activ: 17}}

	// Автоматика — пусто (порог), но кандидат виден.
	auto := Match(nitki, agg, false, nil)
	require.False(t, auto[0].Matched, "автоматика должна отбраковать недокомплект")
	cands := CandidatesFor("9722-421-9838", agg, 17)
	require.Len(t, cands, 1)
	assert.Equal(t, 9, cands[0].MaWagons)

	// Оператор привязал — матч в обход фильтра, вагоны собраны.
	forcedRes := Match(nitki, agg, false, map[int]string{0: cands[0].Key})
	require.True(t, forcedRes[0].Matched)
	assert.Equal(t, "forced_by_index", forcedRes[0].Source)
	assert.Equal(t, 9, forcedRes[0].MaWagons)
	assert.Len(t, forcedRes[0].Vagons, 9)

	// Неизвестный ключ (снимок сменился) — падаем в обычный матч (пусто), не в панику.
	gone := Match(nitki, agg, false, map[int]string{0: "НЕТ|ТАКОГО"})
	assert.False(t, gone[0].Matched)
}
