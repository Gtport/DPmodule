package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/auth"
	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/service"
)

// «Скрыть» пропавшего (operator+): запись-8 остаётся (не удаляется), но помечается
// dismissed_at и уходит из списков; действие в журнале.
func TestDismissMissing(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC))
	defer restore()

	timeOp := domain.NewLocalTime(time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC))
	s9repo := newFakeStatus9()
	s9repo.vagons["111"], s9repo.vagons["222"] = 8, 8
	s9repo.missing = []domain.Dislocation{
		missing8("M1", "111", "9379-783-9857", "АЭ", timeOp),
		missing8("M2", "222", "9379-783-9857", "АЭ", timeOp),
	}
	proc := newMissingProc(t, &fakeDislRepo{}, s9repo)
	jr := &fakeJournalRepo{}
	proc.SetJournal(service.NewJournal(jr, nil))
	hist := newFakeHistory()
	svc := service.NewArrivalsService(hist, nil, proc)

	res, err := svc.DismissMissing(context.Background(), []string{"M1"})
	require.NoError(t, err)
	assert.Equal(t, 1, res.Updated)

	assert.Equal(t, []string{"111"}, s9repo.dismissedMissing, "помечен только выбранный")
	assert.Empty(t, s9repo.deleted, "запись-8 НЕ удаляется — скрытие временное")
	assert.Empty(t, hist.updatedBatch, "история не трогается")

	require.Len(t, jr.events, 1)
	assert.Equal(t, "dismiss_missing", jr.events[0].Source)
}

// Неизвестный id — честная ошибка (картина устарела), ничего не помечено.
func TestDismissMissingUnknownID(t *testing.T) {
	s9repo := newFakeStatus9()
	proc := newMissingProc(t, &fakeDislRepo{}, s9repo)
	svc := service.NewArrivalsService(newFakeHistory(), nil, proc)

	_, err := svc.DismissMissing(context.Background(), []string{"нет такого"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "уже не в пропавших")
	assert.Empty(t, s9repo.dismissedMissing)
}

// «Удалить» пропавшего (senior/admin): рейс в истории получает пометку
// not_arrived (строки не было — вставлена из записи-8), запись-8 удаляется,
// действие в журнале.
func TestDeleteMissing(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC))
	defer restore()

	timeOp := domain.NewLocalTime(time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC))
	s9repo := newFakeStatus9()
	s9repo.vagons["111"], s9repo.vagons["222"] = 8, 8
	s9repo.missing = []domain.Dislocation{
		missing8("M1", "111", "9379-783-9857", "АЭ", timeOp),
		missing8("M2", "222", "9379-783-9857", "АЭ", timeOp),
	}
	proc := newMissingProc(t, &fakeDislRepo{}, s9repo)
	jr := &fakeJournalRepo{}
	proc.SetJournal(service.NewJournal(jr, nil))
	hist := newFakeHistory()
	svc := service.NewArrivalsService(hist, nil, proc)

	// senior-operator (client-роль) — доступ разрешён.
	ctx := auth.WithClaims(context.Background(),
		&auth.Claims{ClientRoles: []auth.Role{auth.ClientSeniorOperator}})
	res, err := svc.DeleteMissing(ctx, []string{"M1"})
	require.NoError(t, err)
	assert.Equal(t, 1, res.Updated)

	// Строки рейса не было — вставлена из записи-8, пометка поверх.
	require.Len(t, hist.inserted, 1)
	assert.Equal(t, "M1", hist.inserted[0].ID)
	require.Contains(t, hist.updatedBatch, "M1")
	assert.Equal(t, true, hist.updatedBatch["M1"]["not_arrived"])

	// Запись-8 удалена (только выбранная).
	assert.Equal(t, []string{"111"}, s9repo.deleted)
	_, still := s9repo.vagons["222"]
	assert.True(t, still, "чужой пропавший не тронут")

	require.Len(t, jr.events, 1)
	assert.Equal(t, "delete_missing", jr.events[0].Source)
}

// Обычному оператору удаление запрещено (ErrArrivalsAccess → 403 в ручке);
// без claims (auth выключен) — разрешено, как в остальных точечных правилах.
func TestDeleteMissingAccess(t *testing.T) {
	timeOp := domain.NewLocalTime(time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC))
	newSvc := func() (*service.ArrivalsService, *fakeStatus9Repo) {
		s9repo := newFakeStatus9()
		s9repo.vagons["111"] = 8
		s9repo.missing = []domain.Dislocation{missing8("M1", "111", "9379-783-9857", "АЭ", timeOp)}
		proc := newMissingProc(t, &fakeDislRepo{}, s9repo)
		return service.NewArrivalsService(newFakeHistory(), nil, proc), s9repo
	}

	svc, s9repo := newSvc()
	operator := auth.WithClaims(context.Background(),
		&auth.Claims{ClientRoles: []auth.Role{auth.ClientOperator}})
	_, err := svc.DeleteMissing(operator, []string{"M1"})
	require.ErrorIs(t, err, service.ErrArrivalsAccess)
	assert.Empty(t, s9repo.deleted, "отказ до какой-либо записи")

	svc, s9repo = newSvc()
	_, err = svc.DeleteMissing(context.Background(), []string{"M1"})
	require.NoError(t, err, "без claims (auth выключен) — разрешаем")
	assert.Equal(t, []string{"111"}, s9repo.deleted)
}
