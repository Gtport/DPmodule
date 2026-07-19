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
