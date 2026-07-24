package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/domain"
)

// fakeBrosRepo — in-memory port.BrosRepository для тестов reconcile.
type fakeBrosRepo struct {
	active  []domain.Bros
	inserts []domain.Bros
	updates map[string]map[string]any
}

func (f *fakeBrosRepo) Active(context.Context) ([]domain.Bros, error) { return f.active, nil }

func (f *fakeBrosRepo) Insert(_ context.Context, b domain.Bros) error {
	f.inserts = append(f.inserts, b)
	return nil
}

func (f *fakeBrosRepo) Update(_ context.Context, id string, fields map[string]any) error {
	if f.updates == nil {
		f.updates = map[string]map[string]any{}
	}
	f.updates[id] = fields
	return nil
}

func (f *fakeBrosRepo) History(context.Context, int, int) ([]domain.Bros, int, error) {
	return nil, 0, nil
}
func (f *fakeBrosRepo) Filter(context.Context, domain.BrosFilter) ([]domain.Bros, int, error) {
	return nil, 0, nil
}
func (f *fakeBrosRepo) GetByID(_ context.Context, id string) (*domain.Bros, error) {
	for i := range f.active {
		if f.active[i].ID == id {
			b := f.active[i]
			return &b, nil
		}
	}
	return nil, nil
}
func (f *fakeBrosRepo) SearchByIndex(context.Context, string) ([]domain.Bros, error) {
	return nil, nil
}
func (f *fakeBrosRepo) PurgeCompletedOlderThan(context.Context, domain.LocalTime) (int, error) {
	return 0, nil
}

// brosVag — вагон статуса `status` с ключом `key` для тестов.
func brosVag(vagon, key, index, station, statNach, cargo string, status int) domain.Dislocation {
	s := status
	return domain.Dislocation{
		Vagon: vagon, IdStatus5: key, Index: index,
		StationOper: station, DorogaOper: "ДВ",
		StationNach: statNach, CargoS: cargo,
		GruzpolS: "АЭ", Naznach: "АЭ",
		Status: &s,
	}
}

func emptyActual() *ActualCache {
	return &ActualCache{byVagon: map[string]domain.Dislocation{}}
}

// Новый брос: два вагона статуса 5 с одним ключом → одна активная запись,
// vagon_count=2, состав «станция груз (2)».
func TestApplyBros_New(t *testing.T) {
	const key = "IDX-A|770001|2026-07-24 08:00:00"
	kept := []domain.Dislocation{
		brosVag("100", key, "IDX-A", "УЛАК", "СМЫЧКА", "УГОЛЬ", 5),
		brosVag("101", key, "IDX-A", "УЛАК", "СМЫЧКА", "УГОЛЬ", 5),
	}
	repo := &fakeBrosRepo{}

	st, err := applyBros(context.Background(), kept, emptyActual(), repo)
	require.NoError(t, err)

	assert.Equal(t, 1, st.New)
	assert.Equal(t, 0, st.Updated)
	assert.Equal(t, 0, st.Stopped)
	assert.Equal(t, 1, st.Active)

	require.Len(t, repo.inserts, 1)
	b := repo.inserts[0]
	assert.Equal(t, key, b.ID)
	assert.Equal(t, "IDX-A", b.Index0)
	assert.Equal(t, "IDX-A", b.Index1)
	assert.Equal(t, "УЛАК", b.StationBr)
	assert.True(t, b.StatusBr)
	assert.Equal(t, 2, b.VagonCount)
	assert.Equal(t, "СМЫЧКА УГОЛЬ (2)", b.Sostav)
}

// Продолжающийся брос: ключ уже активен, вагонов стало больше → UPDATE
// vagon_count, без INSERT.
func TestApplyBros_Continue(t *testing.T) {
	const key = "IDX-A|770001|2026-07-24 08:00:00"
	repo := &fakeBrosRepo{active: []domain.Bros{{
		ID: key, Index1: "IDX-A", StatusBr: true, VagonCount: 2, Sostav: "СМЫЧКА УГОЛЬ (2)",
	}}}
	kept := []domain.Dislocation{
		brosVag("100", key, "IDX-A", "УЛАК", "СМЫЧКА", "УГОЛЬ", 5),
		brosVag("101", key, "IDX-A", "УЛАК", "СМЫЧКА", "УГОЛЬ", 5),
		brosVag("102", key, "IDX-A", "УЛАК", "СМЫЧКА", "УГОЛЬ", 5),
	}

	st, err := applyBros(context.Background(), kept, emptyActual(), repo)
	require.NoError(t, err)

	assert.Equal(t, 0, st.New)
	assert.Equal(t, 1, st.Updated)
	assert.Empty(t, repo.inserts)
	require.Contains(t, repo.updates, key)
	assert.Equal(t, 3, repo.updates[key]["vagon_count"])
	assert.Equal(t, "СМЫЧКА УГОЛЬ (3)", repo.updates[key]["sostav"])
}

// Без изменений: ключ активен, состав и число вагонов те же → UPDATE не зовётся.
func TestApplyBros_NoChange(t *testing.T) {
	const key = "IDX-A|770001|2026-07-24 08:00:00"
	repo := &fakeBrosRepo{active: []domain.Bros{{
		ID: key, Index1: "IDX-A", StatusBr: true, VagonCount: 2, Sostav: "СМЫЧКА УГОЛЬ (2)",
	}}}
	kept := []domain.Dislocation{
		brosVag("100", key, "IDX-A", "УЛАК", "СМЫЧКА", "УГОЛЬ", 5),
		brosVag("101", key, "IDX-A", "УЛАК", "СМЫЧКА", "УГОЛЬ", 5),
	}

	st, err := applyBros(context.Background(), kept, emptyActual(), repo)
	require.NoError(t, err)

	assert.Equal(t, 0, st.Updated)
	assert.Empty(t, repo.updates)
}

// Подъём: ключ активен в БД, вагон ушёл из статуса 5 → STOP (status_br=false),
// новое состояние берётся из свежего батча по номеру вагона прежнего снимка.
func TestApplyBros_Stop(t *testing.T) {
	const key = "IDX-A|770001|2026-07-24 08:00:00"
	repo := &fakeBrosRepo{active: []domain.Bros{{
		ID: key, Index1: "IDX-A", StatusBr: true, VagonCount: 1,
	}}}
	// В свежем батче вагон уже в пути (статус 2, новый ключ пуст).
	moved := brosVag("100", "", "IDX-A2", "ТАЙШЕТ", "СМЫЧКА", "УГОЛЬ", 2)
	kept := []domain.Dislocation{moved}
	// В прежнем снимке вагон был брошен с этим ключом.
	actual := &ActualCache{byVagon: map[string]domain.Dislocation{
		"100": brosVag("100", key, "IDX-A", "УЛАК", "СМЫЧКА", "УГОЛЬ", 5),
	}}

	st, err := applyBros(context.Background(), kept, actual, repo)
	require.NoError(t, err)

	assert.Equal(t, 0, st.New)
	assert.Equal(t, 0, st.Active)
	assert.Equal(t, 1, st.Stopped)
	require.Contains(t, repo.updates, key)
	assert.Equal(t, false, repo.updates[key]["status_br"])
	assert.Contains(t, repo.updates[key], "date_pod_fact")
	assert.Equal(t, "IDX-A2", repo.updates[key]["index_1"])
}
