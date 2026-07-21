package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

type vagonOpStubRepo struct {
	enqueued []domain.VagonOpRequest
	replaced map[int64][]domain.VagonOperation
}

func (r *vagonOpStubRepo) ReplaceForTrip(_ context.Context, key int64, ops []domain.VagonOperation) error {
	if r.replaced == nil {
		r.replaced = map[int64][]domain.VagonOperation{}
	}
	r.replaced[key] = ops
	return nil
}
func (r *vagonOpStubRepo) OperationsByTrip(_ context.Context, key int64) ([]domain.VagonOperation, error) {
	return r.replaced[key], nil
}
func (r *vagonOpStubRepo) Enqueue(_ context.Context, reqs []domain.VagonOpRequest) error {
	r.enqueued = append(r.enqueued, reqs...)
	return nil
}
func (r *vagonOpStubRepo) NextBatch(_ context.Context, _ int) ([]domain.VagonOpRequest, error) {
	return nil, nil
}
func (r *vagonOpStubRepo) Complete(_ context.Context, _ int64) error { return nil }
func (r *vagonOpStubRepo) Fail(_ context.Context, _ int64, _ string, _ int, _ domain.LocalTime) error {
	return nil
}
func (r *vagonOpStubRepo) QueueSize(_ context.Context) (int, error) { return len(r.enqueued), nil }

type vagonOpStubClient struct{ raw []byte }

func (c *vagonOpStubClient) PullWagonHistory(_ context.Context, _, _, _, _ string) ([]byte, error) {
	return c.raw, nil
}

// TestEnqueueTransitions — заявки 601 по трём переходам (решение владельца):
// прибытие (→10), пропажа незавершённого рейса, выбытие прибывшего. Штатное
// выбытие 6/12 и вагоны без клиента провайдера — не в очереди.
func TestEnqueueTransitions(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC))
	defer restore()

	dir := NewDirectoryCache(&unplDirStub{ports: []domain.Ports{
		{Okpo: 10230304, Location: "АЭ", NameS: "АЭ", ProviderClient: "attis", Enabled: true},
	}})
	require.NoError(t, dir.Load(context.Background()))

	nach := domain.NewLocalTime(time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC))
	st2, st5, st10, st12 := 2, 5, 10, 12
	mk := func(id, vagon string, st *int) domain.Dislocation {
		return domain.Dislocation{ID: id, Vagon: vagon, Status: st, DateNach: nach, GruzpolOkpo: "10230304"}
	}
	actual := &ActualCache{byVagon: map[string]domain.Dislocation{
		"111": mk("A", "111", &st2),  // в батче станет 10 → arrival
		"222": mk("B", "222", &st5),  // исчезнет → missing
		"333": mk("C", "333", &st10), // исчезнет → departed
		"444": mk("D", "444", &st12), // исчезнет со статусом 12 → штатно, без заявки
		"555": mk("E", "555", &st2),  // остаётся в пути → без заявки
	}}
	kept := []domain.Dislocation{
		mk("A", "111", &st10),
		mk("E", "555", &st2),
	}

	repo := &vagonOpStubRepo{}
	svc := NewVagonOpService(repo, &vagonOpStubClient{}, dir, actual, nil)
	n, err := svc.EnqueueTransitions(context.Background(), kept, actual)
	require.NoError(t, err)
	assert.Equal(t, 3, n)

	byVagon := map[string]domain.VagonOpRequest{}
	for _, q := range repo.enqueued {
		byVagon[q.Vagon] = q
	}
	require.Len(t, byVagon, 3)
	assert.Equal(t, VagonOpReasonArrival, byVagon["111"].Reason)
	assert.Equal(t, VagonOpReasonMissing, byVagon["222"].Reason)
	assert.Equal(t, VagonOpReasonDeparted, byVagon["333"].Reason)
	assert.Equal(t, "attis", byVagon["111"].Client)
	// trip_key = vagon*100000 + дни эпохи date_nach (совпадает с GENERATED-колонкой)
	wantKey, ok := domain.TripKeyOf("111", nach)
	require.True(t, ok)
	assert.Equal(t, wantKey, byVagon["111"].TripKey)
	assert.Equal(t, "2026-07-15T00:00:00", byVagon["111"].DateNachD.String(), "дата начала — только дата")
}

// TestVagonOpFetchStore — интервал запроса date_nach−1…сегодня и перезапись трейла.
func TestVagonOpFetchStore(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC))
	defer restore()

	raw := []byte(`{"status":"success","data":{"operations":[
		{"KOP_VMD":"2","DATE_OP":"2026-07-16 05:00:00","STAN_OP":"917207","INDEX_POEZD":"917207289967808"}]}}`)
	repo := &vagonOpStubRepo{}
	svc := NewVagonOpService(repo, &vagonOpStubClient{raw: raw}, nil, nil, nil)

	q := domain.VagonOpRequest{
		TripKey: 11100000 + 20649, Vagon: "111", Client: "attis",
		DateNachD: domain.LocalTime(time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)),
	}
	require.NoError(t, svc.fetchStore(context.Background(), q))
	ops := repo.replaced[q.TripKey]
	require.Len(t, ops, 1)
	assert.Equal(t, "917207", ops[0].StanOp)
}
