package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/domain"
)

func rv(id, vagon, invoice string, npp int) domain.Dislocation {
	return domain.Dislocation{ID: id, Vagon: vagon, Invoice: invoice, NppVag: &npp}
}

// Режим «по родительскому индексу»: группы index_main+станция погрузки,
// подгруппы по станции операции/индексу/naznach, вагоны по npp_vag.
func TestGroupParentIndex(t *testing.T) {
	mk := func(id, im, sn, oper, idx, nz string, npp int) domain.Dislocation {
		r := rv(id, id, "ЭТ"+id, npp)
		r.IndexMain, r.StationNach, r.StationOper, r.Index, r.Naznach = im, sn, oper, idx, nz
		r.GruzpolS = "АЭ"
		return r
	}
	records := []domain.Dislocation{
		mk("1", "IM1", "ЕРУНАКОВО", "УЛАК", "IX1", "АЭ", 2),
		mk("2", "IM1", "ЕРУНАКОВО", "УЛАК", "IX1", "АЭ", 1), // та же подгруппа, npp меньше
		mk("3", "IM1", "ЕРУНАКОВО", "ЧЕГДОМЫН", "IX2", "ГУТ-2", 5),
		mk("4", "IM2", "ТЕРСЬ", "УЛАК", "IX3", "АЭ", 1), // другая группа
	}

	groups := groupParentIndex(records)
	require.Len(t, groups, 2)

	g := groups[0] // IM1|ЕРУНАКОВО (сортировка по ключу)
	assert.Equal(t, "IM1", g.IndexMain)
	assert.Equal(t, "ЕРУНАКОВО", g.StationNach)
	assert.Equal(t, 3, g.VagonCount)
	require.Len(t, g.SubGroups, 2)
	for _, sg := range g.SubGroups {
		if sg.VagonCount == 2 {
			assert.Equal(t, "УЛАК", sg.StationOper)
			assert.Equal(t, "2", sg.Vagons[0].Vagon) // npp 1 раньше npp 2
			assert.Equal(t, "1", sg.Vagons[1].Vagon)
		}
	}
}

// Правило available переадресации: крупнейшая подгруппа ≥ порога; одна
// подгруппа — доступна сразу; пересечение накладных — недоступна.
func TestRedirectAvailable(t *testing.T) {
	sub := func(n int, invPrefix string) RearrSubGroupDTO {
		sg := RearrSubGroupDTO{VagonCount: n}
		for i := 0; i < n; i++ {
			sg.Vagons = append(sg.Vagons, RearrVagonDTO{Vagon: "V", Invoice: invPrefix})
		}
		return sg
	}

	// одна подгруппа ≥ порога → доступна
	g1 := RearrGroupDTO{SubGroups: []RearrSubGroupDTO{sub(20, "A")}}
	assert.True(t, redirectAvailable(&g1, 20))

	// крупнейшая меньше порога → нет
	g2 := RearrGroupDTO{SubGroups: []RearrSubGroupDTO{sub(19, "A")}}
	assert.False(t, redirectAvailable(&g2, 20))

	// две подгруппы, накладные не пересекаются → доступна
	g3 := RearrGroupDTO{SubGroups: []RearrSubGroupDTO{sub(25, "A"), sub(3, "B")}}
	assert.True(t, redirectAvailable(&g3, 20))

	// пересечение накладной с крупнейшей → нет
	g4 := RearrGroupDTO{SubGroups: []RearrSubGroupDTO{sub(25, "A"), sub(3, "A")}}
	assert.False(t, redirectAvailable(&g4, 20))

	// пустых накладных в крупнейшей нет → нет (эталон gtport)
	g5 := RearrGroupDTO{SubGroups: []RearrSubGroupDTO{sub(25, ""), sub(3, "B")}}
	assert.False(t, redirectAvailable(&g5, 20))

	// без подгрупп → нет
	g6 := RearrGroupDTO{}
	assert.False(t, redirectAvailable(&g6, 20))
}

// Сортировка вагонов: npp_vag по возрастанию, nil — в конец, при равенстве — по номеру.
func TestSortVagons(t *testing.T) {
	n1, n2 := 1, 2
	v := []RearrVagonDTO{
		{Vagon: "C"},
		{Vagon: "B", NppVag: &n2},
		{Vagon: "A", NppVag: &n1},
	}
	sortVagons(v)
	assert.Equal(t, "A", v[0].Vagon)
	assert.Equal(t, "B", v[1].Vagon)
	assert.Equal(t, "C", v[2].Vagon) // без npp — в конец
}
