package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/domain"
)

// fakeJournalRepo — in-memory port.BrosJournalRepository.
type fakeJournalRepo struct {
	upserts []domain.BrosJournalEntry
	latest  map[string]*domain.BrosJournalEntry
	nextID  int64
}

func (f *fakeJournalRepo) Upsert(_ context.Context, e domain.BrosJournalEntry) (int64, error) {
	f.nextID++
	e.ID = f.nextID
	f.upserts = append(f.upserts, e)
	return f.nextID, nil
}
func (f *fakeJournalRepo) ByBrosID(context.Context, string) ([]domain.BrosJournalEntry, error) {
	return nil, nil
}
func (f *fakeJournalRepo) Latest(_ context.Context, id string) (*domain.BrosJournalEntry, error) {
	if f.latest == nil {
		return nil, nil
	}
	return f.latest[id], nil
}

// fakeCodesRepo — in-memory port.BrosReasonCodesRepository.
type fakeCodesRepo struct{ codes []domain.BrosReasonCode }

func (f fakeCodesRepo) ReasonCodes(context.Context) ([]domain.BrosReasonCode, error) {
	return f.codes, nil
}

func journalSvc(active []domain.Bros, jrepo *fakeJournalRepo) (*BrosJournalService, *fakeBrosRepo) {
	brepo := &fakeBrosRepo{active: active}
	codes := fakeCodesRepo{codes: []domain.BrosReasonCode{
		{Code: "22", Description: "Ожидание локомотива"},
	}}
	return NewBrosJournalService(jrepo, brepo, codes), brepo
}

// Обычный код: сохраняется, reason_text автоподставляется из справочника,
// bros.reason синхронизируется.
func TestBrosJournal_Create_SimpleCode(t *testing.T) {
	jrepo := &fakeJournalRepo{}
	svc, brepo := journalSvc([]domain.Bros{{ID: "K1", StatusBr: true}}, jrepo)

	entry, err := svc.Create(context.Background(), BrosJournalCreate{
		BrosID: "K1", Reason: "22", Comment: "стоим",
	})
	require.NoError(t, err)
	assert.Equal(t, "22", entry.Reason)
	require.NotNil(t, entry.ReasonText)
	assert.Equal(t, "Ожидание локомотива", *entry.ReasonText)
	require.Len(t, jrepo.upserts, 1)
	assert.Equal(t, "22", brepo.updates["K1"]["reason"])
}

// Код 05: без реквизитов заявки — ошибка; с полным набором — сохраняется и
// date_pod синхронизируется в снимок.
func TestBrosJournal_Create_Code05(t *testing.T) {
	jrepo := &fakeJournalRepo{}
	svc, brepo := journalSvc([]domain.Bros{{ID: "K1", StatusBr: true}}, jrepo)
	ctx := context.Background()

	_, err := svc.Create(ctx, BrosJournalCreate{BrosID: "K1", Reason: "5"}) // "5" → "05"
	assert.Error(t, err)

	entry, err := svc.Create(ctx, BrosJournalCreate{
		BrosID: "K1", Reason: "05",
		ZayavkaNomer: "З-1", ZayavkaDate: "2026-07-20",
		DatePod: "2026-07-25", ReasonText: "размещение по договору",
	})
	require.NoError(t, err)
	require.NotNil(t, entry.DatePod)
	assert.NotNil(t, brepo.updates["K1"]["date_pod"])
}

// Код 01: без is_agreed — ошибка; согласованный без письма — ошибка;
// несогласованный — сохраняется.
func TestBrosJournal_Create_Code01(t *testing.T) {
	jrepo := &fakeJournalRepo{}
	svc, _ := journalSvc([]domain.Bros{{ID: "K1", StatusBr: true}}, jrepo)
	ctx := context.Background()

	_, err := svc.Create(ctx, BrosJournalCreate{BrosID: "K1", Reason: "01"})
	assert.Error(t, err, "нет is_agreed")

	agreed := true
	_, err = svc.Create(ctx, BrosJournalCreate{BrosID: "K1", Reason: "01", IsAgreed: &agreed})
	assert.Error(t, err, "согласованный без реквизитов письма")

	notAgreed := false
	entry, err := svc.Create(ctx, BrosJournalCreate{BrosID: "K1", Reason: "01", IsAgreed: &notAgreed})
	require.NoError(t, err)
	require.NotNil(t, entry.IsAgreed)
	assert.False(t, *entry.IsAgreed)
}

// Несуществующий бросок — ошибка.
func TestBrosJournal_Create_UnknownBros(t *testing.T) {
	jrepo := &fakeJournalRepo{}
	svc, _ := journalSvc(nil, jrepo)
	_, err := svc.Create(context.Background(), BrosJournalCreate{BrosID: "NOPE", Reason: "22"})
	assert.Error(t, err)
}

// BulkSave: без истории — первая запись из полей bros (created_by=system);
// с историей — копия последней (сохраняется автор).
func TestBrosJournal_BulkSave(t *testing.T) {
	ctx := context.Background()

	// Без истории.
	jrepo := &fakeJournalRepo{}
	svc, _ := journalSvc([]domain.Bros{{ID: "K1", StatusBr: true, Reason: "22"}}, jrepo)
	res, err := svc.BulkSave(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Total)
	assert.Equal(t, 1, res.Saved)
	require.Len(t, jrepo.upserts, 1)
	assert.Equal(t, "system", jrepo.upserts[0].CreatedBy)
	assert.Equal(t, "22", jrepo.upserts[0].Reason)

	// С историей — копируется автор и код.
	jrepo2 := &fakeJournalRepo{latest: map[string]*domain.BrosJournalEntry{
		"K1": {BrosID: "K1", Reason: "05", CreatedBy: "ivan"},
	}}
	svc2, _ := journalSvc([]domain.Bros{{ID: "K1", StatusBr: true, Reason: "22"}}, jrepo2)
	_, err = svc2.BulkSave(ctx)
	require.NoError(t, err)
	require.Len(t, jrepo2.upserts, 1)
	assert.Equal(t, "ivan", jrepo2.upserts[0].CreatedBy)
	assert.Equal(t, "05", jrepo2.upserts[0].Reason)
}
