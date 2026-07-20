package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/service"
)

// Подтверждение прибытия кандидата-9: в снимке появляется статус 10 с date_prib
// (дальше держится sticky-10), не-кандидаты не трогаются.
func TestConfirmArrival(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC))
	defer restore()

	s9, s10 := 9, 10
	opJd := domain.NewLocalTime(time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC))
	repo := &fakeDislRepo{current: []domain.Dislocation{
		{ID: "C1", Vagon: "111", Status: &s9, Naznach: "АЭ", DateOpJd: opJd},
		{ID: "C2", Vagon: "222", Status: &s9, Naznach: "АЭ", DateOpJd: opJd},
		{ID: "A1", Vagon: "333", Status: &s10, Naznach: "АЭ"}, // уже прибыл — не кандидат
	}}
	proc, _ := newProcessor(t, repo)
	intake, _ := newIntake(t)
	_ = intake

	hist := newFakeHistory()
	svc := service.NewArrivalsService(hist, nil, proc)

	prib := domain.NewLocalTime(time.Date(2026, 7, 20, 9, 30, 0, 0, time.UTC))
	res, err := svc.ConfirmArrival(context.Background(), service.ConfirmArrivalRequest{
		VagonIDs: []string{"C1", "A1"}, // A1 не кандидат — должен быть пропущен
		DatePrib: prib,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, res.Updated)
	assert.Equal(t, 2, res.Selected)

	// Снимок подменён: C1 стал 10 с фактом и date_kon; C2 остался 9.
	byID := map[string]domain.Dislocation{}
	for _, r := range repo.replaced {
		byID[r.ID] = r
	}
	c1 := byID["C1"]
	require.NotNil(t, c1.Status)
	assert.Equal(t, 10, *c1.Status)
	require.NotNil(t, c1.DatePrib)
	assert.Equal(t, prib.String(), c1.DatePrib.String())
	assert.Equal(t, opJd.String(), c1.DateKon.String()) // date_kon = date_op_jd (правило статуса 10)
	c2 := byID["C2"]
	assert.Equal(t, 9, *c2.Status)

	// Веха прибытия ушла в историю (батч по C1).
	require.Contains(t, hist.updatedBatch, "C1")
	assert.Equal(t, 10, hist.updatedBatch["C1"]["status"])
	assert.NotNil(t, hist.updatedBatch["C1"]["date_prib"])
}

// Отмена прибытия: снимок 10→9 (вагон снова кандидат), веха истории очищена;
// выгруженный (12) — запрет.
func TestCancelArrival(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC))
	defer restore()

	s10, s12 := 10, 12
	timeOp := domain.NewLocalTime(time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC))
	prib := domain.NewLocalTime(time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC))
	mk := func(id, vagon string, st *int) domain.Dislocation {
		return domain.Dislocation{ID: id, Vagon: vagon, Status: st, DatePrib: prib, TimeOp: timeOp, Naznach: "АЭ"}
	}

	t.Run("10 → 9, веха очищена", func(t *testing.T) {
		repo := &fakeDislRepo{current: []domain.Dislocation{mk("A1", "111", &s10)}}
		proc, _ := newProcessor(t, repo)
		hist := newFakeHistory()
		svc := service.NewArrivalsService(hist, nil, proc)

		res, err := svc.CancelArrival(context.Background(), []string{"A1"})
		require.NoError(t, err)
		assert.Equal(t, 1, res.Updated)

		var a1 domain.Dislocation
		for _, r := range repo.replaced {
			if r.ID == "A1" {
				a1 = r
			}
		}
		require.NotNil(t, a1.Status)
		assert.Equal(t, 9, *a1.Status)
		assert.Nil(t, a1.DatePrib)
		assert.Equal(t, timeOp.String(), a1.DateKon.String()) // date_kon = time_op (правило не-10)

		require.Contains(t, hist.updatedBatch, "A1")
		assert.Nil(t, hist.updatedBatch["A1"]["date_prib"])
		assert.Equal(t, "", hist.updatedBatch["A1"]["otkl"])
	})

	t.Run("выгруженный — запрет", func(t *testing.T) {
		repo := &fakeDislRepo{current: []domain.Dislocation{mk("B1", "222", &s12)}}
		proc, _ := newProcessor(t, repo)
		svc := service.NewArrivalsService(newFakeHistory(), nil, proc)

		_, err := svc.CancelArrival(context.Background(), []string{"B1"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "выгружен")
	})

	t.Run("нет в снимке — понятная ошибка", func(t *testing.T) {
		repo := &fakeDislRepo{current: []domain.Dislocation{mk("A1", "111", &s10)}}
		proc, _ := newProcessor(t, repo)
		svc := service.NewArrivalsService(newFakeHistory(), nil, proc)

		_, err := svc.CancelArrival(context.Background(), []string{"НЕТ_ТАКОГО"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "нет в текущем снимке")
	})
}
