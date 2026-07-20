package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/parser/plan"
	"github.com/Gtport/DPmodule/internal/service/planmatch"
)

func TestApplyIndexOverrides(t *testing.T) {
	nitki := []plan.PlanNitka{
		{IndexPp: "с.ф.БИКИН", IsSf: true},               // 0: с.ф. → впишем реальный индекс
		{Index: "0000-000-0000", IndexPp: "0000-000-0000"}, // 1: обычная, исправим опечатку
		{IndexPp: "9999-999-9999"},                         // 2: без правки
	}
	out := applyIndexOverrides(nitki, map[int]string{
		0: "7438-011-1234",
		1: "5510-022-3456",
		2: "",  // пустой — снятие правки, не трогаем
		9: "x", // вне диапазона — пропуск, без паники
	})

	// с.ф. стала обычной ниткой с вписанным индексом.
	assert.False(t, out[0].IsSf, "с.ф. с вписанным индексом должна перестать быть с.ф.")
	assert.Equal(t, "7438-011-1234", out[0].IndexPp)
	assert.Equal(t, "7438-011-1234", out[0].Index)
	// обычная — индекс исправлен.
	assert.Equal(t, "5510-022-3456", out[1].IndexPp)
	// без правки — как было.
	assert.Equal(t, "9999-999-9999", out[2].IndexPp)

	// Исходный срез не мутирован (работаем на копии).
	assert.True(t, nitki[0].IsSf)
	assert.Equal(t, "с.ф.БИКИН", nitki[0].IndexPp)
	assert.Equal(t, "0000-000-0000", nitki[1].IndexPp)
}

func TestApplyIndexOverrides_Empty(t *testing.T) {
	nitki := []plan.PlanNitka{{IndexPp: "a"}}
	// Пустая карта → тот же срез без копирования.
	out := applyIndexOverrides(nitki, nil)
	assert.Equal(t, "a", out[0].IndexPp)
}

func TestProblemRows(t *testing.T) {
	nitki := []plan.PlanNitka{
		{IndexPp: "A", Activ: 12},                 // 0: обычная, Activ>0, не сматчилась → проблема
		{IndexPp: "B", Activ: 8},                  // 1: обычная, Activ>0, сматчилась → НЕ проблема
		{IndexPp: "C", Activ: 0},                  // 2: Activ=0 → НЕ проблема
		{IndexPp: "с.ф.X", Activ: 5, IsSf: true},  // 3: с.ф. → НЕ проблема (свой поток)
		{IndexPp: "D", Activ: 5, IsOstatok: true}, // 4: «Остаток» → НЕ проблема
	}
	matches := []planmatch.NitkaMatch{
		{Matched: false},
		{Matched: true},
		{Matched: false},
		{Matched: false},
		{Matched: false},
	}
	probs := problemRows(nitki, matches, planmatch.AggResult{})

	require.Len(t, probs, 1)
	assert.Equal(t, 0, probs[0].Ord)
	assert.Equal(t, "A", probs[0].IndexPp)
	assert.Equal(t, 12, probs[0].Activ)
}

func TestStoredNitkiToPlan(t *testing.T) {
	msk := lt(8, 49) // *domain.LocalTime
	nitki := []domain.PlanNitka{
		{
			Index: "7438-011-1234", IndexPp: "7438-011-1234", Activ: 12, Wagons: 20,
			IsSf: false, PlanMsk: msk,
			Ports: []domain.PortCell{{Label: "T1", Count: 5, IsOur: true}},
		},
		{IndexPp: "с.ф.НАХОДКА", IsSf: true, Activ: 24, PlanMsk: nil}, // с.ф. без времени/портов
	}
	out := storedNitkiToPlan(nitki)

	require.Len(t, out, 2)
	// обычная нитка: индекс/activ/is_sf/время/порты восстановлены для повторного матча.
	assert.Equal(t, "7438-011-1234", out[0].IndexPp)
	assert.Equal(t, 12, out[0].Activ)
	assert.False(t, out[0].IsSf)
	assert.False(t, out[0].PlanMsk.IsZero())
	assert.Equal(t, msk.Time(), out[0].PlanMsk) // время восстановлено 1:1
	require.Len(t, out[0].Ports, 1)
	assert.Equal(t, "T1", out[0].Ports[0].Label)
	assert.True(t, out[0].Ports[0].IsOur)
	// с.ф.: флаг сохранён, нулевое время → zero, портов нет.
	assert.True(t, out[1].IsSf)
	assert.True(t, out[1].PlanMsk.IsZero())
	assert.Nil(t, out[1].Ports)
}

func TestRecalcSourceName(t *testing.T) {
	assert.Equal(t, "ma.xlsx (пересчёт)", recalcSourceName("ma.xlsx"))
	// повторный пересчёт не накапливает пометку.
	assert.Equal(t, "ma.xlsx (пересчёт)", recalcSourceName("ma.xlsx (пересчёт)"))
	assert.Equal(t, "ma.xlsx (пересчёт)", recalcSourceName("  ma.xlsx (пересчёт)  "))
}

func TestPendingStore_Peek(t *testing.T) {
	s := newPendingStore(time.Minute)
	tok := s.put(pendingPlan{planCode: "ma", filename: "ma.xlsx", doc: &plan.PlanDoc{}})

	// peek не расходует токен — можно звать многократно.
	p1, ok := s.peek(tok)
	require.True(t, ok)
	assert.Equal(t, "ma", p1.planCode)
	_, ok = s.peek(tok)
	require.True(t, ok, "peek не должен удалять токен")

	// take забирает окончательно.
	_, ok = s.take(tok)
	require.True(t, ok)
	_, ok = s.peek(tok)
	assert.False(t, ok, "после take токена нет")
}
