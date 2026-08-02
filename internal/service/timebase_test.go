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

// Шкала ввода МСК: вечернее прибытие пропавшего (час ≥ 18) хранится ЖД-штампом
// со сдвигом +сутки; вечерняя выгрузка — МСК как есть, ЖД-сутки производные.
// Правка индекса оператором сильнее индекса записи-8.
func TestConfirmMissingMskBase(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 8, 2, 23, 0, 0, 0, time.UTC))
	defer restore()

	timeOp := domain.NewLocalTime(time.Date(2026, 7, 25, 8, 0, 0, 0, time.UTC))
	s9repo := newFakeStatus9()
	s9repo.vagons["111"] = 8
	s9repo.missing = []domain.Dislocation{missing8("M1", "111", "9379-783-9857", "АЭ", timeOp)}
	proc := newMissingProc(t, &fakeDislRepo{}, s9repo)
	hist := newFakeHistory()
	hist.existing["M1"] = struct{}{}
	svc := service.NewArrivalsService(hist, nil, proc)

	prib := domain.NewLocalTime(time.Date(2026, 8, 2, 18, 30, 0, 0, time.UTC)) // реальное МСК, сегодня вечером
	vigr := domain.NewLocalTime(time.Date(2026, 8, 2, 19, 15, 0, 0, time.UTC))
	_, err := svc.ConfirmMissing(context.Background(), service.ConfirmMissingRequest{
		VagonIDs: []string{"M1"}, DatePrib: prib, DateVigr: vigr,
		Index: "9999-111-9857", TimeBase: domain.TimeBaseMSK,
	})
	require.NoError(t, err)

	f := hist.updatedBatch["M1"]
	pribJd, _ := f["date_prib"].(*domain.LocalTime)
	require.NotNil(t, pribJd)
	assert.Equal(t, "2026-08-03T18:30:00", pribJd.String(), "МСК-вечер → ЖД-штамп +сутки")
	pribD, _ := f["date_prib_d"].(*domain.LocalTime)
	assert.Equal(t, "2026-08-03", pribD.String()[:10])
	vigrMsk, _ := f["date_vigr"].(*domain.LocalTime)
	assert.Equal(t, "2026-08-02T19:15:00", vigrMsk.String(), "МСК хранится как введено")
	vigrD, _ := f["date_vigr_d"].(*domain.LocalTime)
	assert.Equal(t, "2026-08-03", vigrD.String()[:10], "ЖД-сутки выгрузки производные")
	assert.Equal(t, "9999-111-9857", f["index_pp"], "правка индекса оператором")
}

// Шкала ввода ЖД: прибытие хранится как есть, выгрузка пересчитывается в МСК
// (−сутки при часе ≥ 18); вечернее ЖД-время «завтрашних» суток не считается будущим.
func TestConfirmMissingJdBase(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 8, 2, 23, 0, 0, 0, time.UTC))
	defer restore()

	timeOp := domain.NewLocalTime(time.Date(2026, 7, 25, 8, 0, 0, 0, time.UTC))
	s9repo := newFakeStatus9()
	s9repo.vagons["111"] = 8
	s9repo.missing = []domain.Dislocation{missing8("M1", "111", "9379-783-9857", "АЭ", timeOp)}
	proc := newMissingProc(t, &fakeDislRepo{}, s9repo)
	hist := newFakeHistory()
	hist.existing["M1"] = struct{}{}
	svc := service.NewArrivalsService(hist, nil, proc)

	// ЖД 03.08 18:30/19:15 = реальный вечер 02.08 МСК — не «в будущем».
	prib := domain.NewLocalTime(time.Date(2026, 8, 3, 18, 30, 0, 0, time.UTC))
	vigr := domain.NewLocalTime(time.Date(2026, 8, 3, 19, 15, 0, 0, time.UTC))
	_, err := svc.ConfirmMissing(context.Background(), service.ConfirmMissingRequest{
		VagonIDs: []string{"M1"}, DatePrib: prib, DateVigr: vigr, TimeBase: domain.TimeBaseJD,
	})
	require.NoError(t, err)

	f := hist.updatedBatch["M1"]
	pribJd, _ := f["date_prib"].(*domain.LocalTime)
	assert.Equal(t, "2026-08-03T18:30:00", pribJd.String(), "ЖД хранится как введено")
	vigrMsk, _ := f["date_vigr"].(*domain.LocalTime)
	assert.Equal(t, "2026-08-02T19:15:00", vigrMsk.String(), "ЖД-вечер → МСК −сутки")
	vigrD, _ := f["date_vigr_d"].(*domain.LocalTime)
	assert.Equal(t, "2026-08-03", vigrD.String()[:10])
}

// Неизвестная шкала отвергается до какой-либо записи.
func TestConfirmMissingBadTimeBase(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC))
	defer restore()

	s9repo := newFakeStatus9()
	proc := newMissingProc(t, &fakeDislRepo{}, s9repo)
	svc := service.NewArrivalsService(newFakeHistory(), nil, proc)

	prib := domain.NewLocalTime(time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC))
	_, err := svc.ConfirmMissing(context.Background(), service.ConfirmMissingRequest{
		VagonIDs: []string{"M1"}, DatePrib: prib, TimeBase: "utc",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "шкала времени")
}

// «Изменить прибытие»/«Выгрузить» в истории прибывших: шкала МСК сдвигает
// вечернее прибытие в ЖД-штамп; шкала ЖД пересчитывает вечернюю выгрузку в МСК.
func TestUpdateVagonsTimeBase(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC))
	defer restore()

	mkSvc := func() (*service.ArrivalsService, *fakeHistoryRepo) {
		repo := &fakeDislRepo{}
		proc, _ := newProcessor(t, repo)
		hist := newFakeHistory()
		hist.rows["A1"] = domain.VagonHistory{ID: "A1", Vagon: "111",
			DatePribD: domain.NewLocalTime(time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC))}
		return service.NewArrivalsService(hist, nil, proc), hist
	}

	t.Run("msk: вечернее прибытие → ЖД +сутки", func(t *testing.T) {
		svc, hist := mkSvc()
		prib := domain.NewLocalTime(time.Date(2026, 8, 1, 19, 0, 0, 0, time.UTC))
		_, err := svc.UpdateVagons(context.Background(), service.ArrivalsUpdateRequest{
			VagonIDs: []string{"A1"}, DatePrib: prib, TimeBase: domain.TimeBaseMSK,
		})
		require.NoError(t, err)
		got, _ := hist.updatedBatch["A1"]["date_prib"].(*domain.LocalTime)
		assert.Equal(t, "2026-08-02T19:00:00", got.String())
	})

	t.Run("jd: вечерняя выгрузка → МСК −сутки, ЖД-сутки прежние", func(t *testing.T) {
		svc, hist := mkSvc()
		vigr := domain.NewLocalTime(time.Date(2026, 8, 2, 18, 30, 0, 0, time.UTC))
		_, err := svc.UpdateVagons(context.Background(), service.ArrivalsUpdateRequest{
			VagonIDs: []string{"A1"}, DateVigr: vigr, TimeBase: domain.TimeBaseJD,
		})
		require.NoError(t, err)
		got, _ := hist.updatedBatch["A1"]["date_vigr"].(*domain.LocalTime)
		assert.Equal(t, "2026-08-01T18:30:00", got.String())
		gotD, _ := hist.updatedBatch["A1"]["date_vigr_d"].(*domain.LocalTime)
		assert.Equal(t, "2026-08-02", gotD.String()[:10])
	})
}

// Подтверждение кандидата в шкале МСК: вечернее время уходит в снимок и веху
// ЖД-штампом (+сутки), как записала бы автоматика.
func TestConfirmArrivalMskBase(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC))
	defer restore()

	s9 := 9
	opJd := domain.NewLocalTime(time.Date(2026, 8, 1, 20, 0, 0, 0, time.UTC))
	repo := &fakeDislRepo{current: []domain.Dislocation{
		{ID: "C1", Vagon: "111", Status: &s9, Naznach: "АЭ", DateOpJd: opJd},
	}}
	proc, _ := newProcessor(t, repo)
	hist := newFakeHistory()
	svc := service.NewArrivalsService(hist, nil, proc)

	prib := domain.NewLocalTime(time.Date(2026, 8, 1, 19, 0, 0, 0, time.UTC)) // реальный вечер МСК
	_, err := svc.ConfirmArrival(context.Background(), service.ConfirmArrivalRequest{
		VagonIDs: []string{"C1"}, DatePrib: prib, TimeBase: domain.TimeBaseMSK,
	})
	require.NoError(t, err)

	var c1 domain.Dislocation
	for _, r := range repo.replaced {
		if r.ID == "C1" {
			c1 = r
		}
	}
	require.NotNil(t, c1.DatePrib)
	assert.Equal(t, "2026-08-02T19:00:00", c1.DatePrib.String(), "снимок — ЖД-штамп")
	got, _ := hist.updatedBatch["C1"]["date_prib"].(*domain.LocalTime)
	assert.Equal(t, "2026-08-02T19:00:00", got.String(), "веха — тот же ЖД-штамп")
}
