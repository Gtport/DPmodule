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

// fakeDelayRepo — in-memory port.VagonDelayRepository для тестов reconcile
// и отчёта (period — что вернёт ByPeriod).
type fakeDelayRepo struct {
	open    []domain.VagonDelay
	period  []domain.VagonDelayRow
	inserts []domain.VagonDelay
	updates map[int64]map[string]any
}

func (f *fakeDelayRepo) Open(context.Context) ([]domain.VagonDelay, error) { return f.open, nil }

func (f *fakeDelayRepo) Insert(_ context.Context, d domain.VagonDelay) error {
	f.inserts = append(f.inserts, d)
	return nil
}

func (f *fakeDelayRepo) Update(_ context.Context, id int64, fields map[string]any) error {
	if f.updates == nil {
		f.updates = map[int64]map[string]any{}
	}
	f.updates[id] = fields
	return nil
}

func (f *fakeDelayRepo) PurgeClosedOlderThan(context.Context, domain.LocalTime) (int, error) {
	return 0, nil
}

func (f *fakeDelayRepo) ByTrip(context.Context, string, domain.LocalTime) ([]domain.VagonDelay, error) {
	return nil, nil
}

func (f *fakeDelayRepo) ByPeriod(context.Context, domain.LocalTime, domain.LocalTime, string) ([]domain.VagonDelayRow, error) {
	return f.period, nil
}

func (f *fakeDelayRepo) Current(context.Context) ([]domain.VagonDelayRow, error) {
	return f.period, nil
}

// delayVag — вагон статуса `status` на станции `stCode` для тестов задержек.
func delayVag(vagon, stCode string, status int, timeOp *domain.LocalTime) domain.Dislocation {
	s := status
	r := domain.Dislocation{
		Vagon: vagon, Index: "1234-567-8901", IndexMain: "1234-567-8901",
		CodeStationOper: stCode, StationOper: "СТ-" + stCode, DorogaOper: "ДВ",
		TimeOp: timeOp, DateNach: domain.NewLocalTime(time.Date(2026, 7, 10, 14, 0, 0, 0, time.UTC)),
		Status: &s,
	}
	key := "1234-567-8901|" + stCode + "|op"
	switch status {
	case 4:
		r.IdStatus4 = key
	case 5:
		r.IdStatus5 = key
	}
	return r
}

// Вход в статус 4 — новый эпизод: date_from = time_op (начало стоянки),
// kind=4, group_key из id_status4, индексы и станция заполнены.
func TestApplyDelays_Open(t *testing.T) {
	timeOp := domain.NewLocalTime(time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC))
	kept := []domain.Dislocation{delayVag("100", "770001", 4, timeOp)}
	repo := &fakeDelayRepo{}

	st, err := applyDelays(context.Background(), kept, repo)
	require.NoError(t, err)

	assert.Equal(t, DelayStats{Opened: 1, Active: 1}, st)
	require.Len(t, repo.inserts, 1)
	d := repo.inserts[0]
	assert.Equal(t, "100", d.Vagon)
	assert.Equal(t, domain.DelayKindProstoi, d.Kind)
	assert.Equal(t, "1234-567-8901|770001|op", d.GroupKey)
	assert.Equal(t, "1234-567-8901", d.Index)
	assert.Equal(t, "1234-567-8901", d.IndexMain)
	assert.Equal(t, "770001", d.StationCode)
	assert.Equal(t, "СТ-770001", d.StationName)
	require.NotNil(t, d.DateFrom)
	assert.Equal(t, *timeOp, *d.DateFrom)
	require.NotNil(t, d.DateNachD) // привязка к рейсу: дата без времени
	assert.Equal(t, time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC), time.Time(*d.DateNachD))
	assert.Nil(t, d.DateTo)
}

// Без time_op начало стоянки неизвестно — date_from = момент пересбора.
func TestApplyDelays_OpenNoTimeOp(t *testing.T) {
	now := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	restore := clock.SetForTest(now)
	defer restore()

	kept := []domain.Dislocation{delayVag("100", "770001", 4, nil)}
	repo := &fakeDelayRepo{}

	_, err := applyDelays(context.Background(), kept, repo)
	require.NoError(t, err)

	require.Len(t, repo.inserts, 1)
	require.NotNil(t, repo.inserts[0].DateFrom)
	assert.Equal(t, now, time.Time(*repo.inserts[0].DateFrom))
}

// Стоит на той же станции без изменений — эпизод продолжается: ни INSERT, ни UPDATE.
func TestApplyDelays_Continue(t *testing.T) {
	from := domain.NewLocalTime(time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC))
	repo := &fakeDelayRepo{open: []domain.VagonDelay{{
		ID: 1, Vagon: "100", Kind: 4, GroupKey: "1234-567-8901|770001|op",
		Index: "1234-567-8901", IndexMain: "1234-567-8901",
		StationCode: "770001", DateFrom: from,
	}}}
	kept := []domain.Dislocation{delayVag("100", "770001", 4, from)}

	st, err := applyDelays(context.Background(), kept, repo)
	require.NoError(t, err)

	assert.Equal(t, DelayStats{Active: 1}, st)
	assert.Empty(t, repo.inserts)
	assert.Empty(t, repo.updates)
}

// Эскалация: простаивающий (kind=4) брошен на той же станции → kind затирается
// 5 (решение владельца), эпизод не закрывается, date_from не трогается.
func TestApplyDelays_Escalate4to5(t *testing.T) {
	from := domain.NewLocalTime(time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC))
	repo := &fakeDelayRepo{open: []domain.VagonDelay{{
		ID: 1, Vagon: "100", Kind: 4, GroupKey: "old-key",
		Index: "1234-567-8901", IndexMain: "1234-567-8901",
		StationCode: "770001", DateFrom: from,
	}}}
	kept := []domain.Dislocation{delayVag("100", "770001", 5, from)}

	st, err := applyDelays(context.Background(), kept, repo)
	require.NoError(t, err)

	assert.Equal(t, DelayStats{Escalated: 1, Active: 1}, st)
	assert.Empty(t, repo.inserts)
	require.Contains(t, repo.updates, int64(1))
	fields := repo.updates[1]
	assert.Equal(t, domain.DelayKindBros, fields["kind"])
	assert.Equal(t, "1234-567-8901|770001|op", fields["group_key"]) // ключ теперь id_status5
	assert.NotContains(t, fields, "date_from")
	assert.NotContains(t, fields, "date_to")
}

// Обратного понижения нет: брошенный (kind=5) со свежим статусом 4 на той же
// станции остаётся kind=5.
func TestApplyDelays_NoDowngrade5to4(t *testing.T) {
	from := domain.NewLocalTime(time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC))
	repo := &fakeDelayRepo{open: []domain.VagonDelay{{
		ID: 1, Vagon: "100", Kind: 5, GroupKey: "1234-567-8901|770001|op",
		Index: "1234-567-8901", IndexMain: "1234-567-8901",
		StationCode: "770001", DateFrom: from,
	}}}
	kept := []domain.Dislocation{delayVag("100", "770001", 4, from)}

	st, err := applyDelays(context.Background(), kept, repo)
	require.NoError(t, err)

	assert.Equal(t, DelayStats{Active: 1}, st)
	if fields, ok := repo.updates[1]; ok {
		assert.NotContains(t, fields, "kind")
	}
}

// Уехал и снова стоит на другой станции: старый эпизод закрывается time_op'ом
// новой записи (hours от date_from), открывается новый по новой станции.
func TestApplyDelays_MoveToNewStation(t *testing.T) {
	from := domain.NewLocalTime(time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC))
	newOp := domain.NewLocalTime(time.Date(2026, 7, 22, 9, 30, 0, 0, time.UTC))
	repo := &fakeDelayRepo{open: []domain.VagonDelay{{
		ID: 1, Vagon: "100", Kind: 4, StationCode: "770001", DateFrom: from,
	}}}
	kept := []domain.Dislocation{delayVag("100", "880002", 4, newOp)}

	st, err := applyDelays(context.Background(), kept, repo)
	require.NoError(t, err)

	assert.Equal(t, DelayStats{Opened: 1, Closed: 1, Active: 1}, st)
	require.Contains(t, repo.updates, int64(1))
	fields := repo.updates[1]
	assert.Equal(t, newOp, fields["date_to"])
	assert.Equal(t, 49.5, fields["hours"]) // 2 суток 1.5 часа
	require.Len(t, repo.inserts, 1)
	assert.Equal(t, "880002", repo.inserts[0].StationCode)
	assert.Equal(t, newOp, repo.inserts[0].DateFrom)
}

// Вагон ушёл из статуса 4/5 (поехал) — эпизод закрывается time_op'ом увёзшей операции.
func TestApplyDelays_CloseOnStatusLeft(t *testing.T) {
	from := domain.NewLocalTime(time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC))
	newOp := domain.NewLocalTime(time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC))
	repo := &fakeDelayRepo{open: []domain.VagonDelay{{
		ID: 1, Vagon: "100", Kind: 4, StationCode: "770001", DateFrom: from,
	}}}
	kept := []domain.Dislocation{delayVag("100", "880002", 2, newOp)} // едет дальше

	st, err := applyDelays(context.Background(), kept, repo)
	require.NoError(t, err)

	assert.Equal(t, DelayStats{Closed: 1}, st)
	assert.Empty(t, repo.inserts)
	require.Contains(t, repo.updates, int64(1))
	fields := repo.updates[1]
	assert.Equal(t, newOp, fields["date_to"])
	assert.Equal(t, 24.0, fields["hours"])
}

// Вагон пропал из выгрузки — эпизод закрывается моментом пересбора.
func TestApplyDelays_CloseOnMissing(t *testing.T) {
	now := time.Date(2026, 7, 22, 20, 0, 0, 0, time.UTC)
	restore := clock.SetForTest(now)
	defer restore()

	from := domain.NewLocalTime(time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC))
	repo := &fakeDelayRepo{open: []domain.VagonDelay{{
		ID: 1, Vagon: "100", Kind: 5, StationCode: "770001", DateFrom: from,
	}}}

	st, err := applyDelays(context.Background(), nil, repo)
	require.NoError(t, err)

	assert.Equal(t, DelayStats{Closed: 1}, st)
	require.Contains(t, repo.updates, int64(1))
	fields := repo.updates[1]
	require.NotNil(t, fields["date_to"])
	assert.Equal(t, now, time.Time(*fields["date_to"].(*domain.LocalTime)))
	assert.Equal(t, 60.0, fields["hours"])
}

// Повторный простой того же вагона после закрытия прошлого эпизода — новый
// эпизод (память накапливается: прошлые закрытые записи не мешают).
func TestApplyDelays_SecondEpisode(t *testing.T) {
	// Прошлый эпизод закрыт — в open его уже нет.
	timeOp := domain.NewLocalTime(time.Date(2026, 7, 23, 6, 0, 0, 0, time.UTC))
	repo := &fakeDelayRepo{}
	kept := []domain.Dislocation{delayVag("100", "990003", 4, timeOp)}

	st, err := applyDelays(context.Background(), kept, repo)
	require.NoError(t, err)

	assert.Equal(t, DelayStats{Opened: 1, Active: 1}, st)
	require.Len(t, repo.inserts, 1)
	assert.Equal(t, "990003", repo.inserts[0].StationCode)
}

// При sticky-удержании ключи id_status4/5 в снимке пусты — накопленный
// group_key пустым значением не затирается.
func TestApplyDelays_EmptyKeyKeepsGroupKey(t *testing.T) {
	from := domain.NewLocalTime(time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC))
	repo := &fakeDelayRepo{open: []domain.VagonDelay{{
		ID: 1, Vagon: "100", Kind: 4, GroupKey: "1234-567-8901|770001|op",
		Index: "1234-567-8901", IndexMain: "1234-567-8901",
		StationCode: "770001", DateFrom: from,
	}}}
	r := delayVag("100", "770001", 4, from)
	r.IdStatus4 = "" // sticky: свежий расчёт статус 4 не дал, ключ пуст

	st, err := applyDelays(context.Background(), []domain.Dislocation{r}, repo)
	require.NoError(t, err)

	assert.Equal(t, DelayStats{Active: 1}, st)
	assert.Empty(t, repo.updates)
}
