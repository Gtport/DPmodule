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

// newMissingProc — процессор с СОБСТВЕННЫМ fake-status9 (записи-8 для подтверждения
// пропавших читаются через LoadMissing, штатный newProcessor их не наполняет).
func newMissingProc(t *testing.T, repo *fakeDislRepo, s9 *fakeStatus9Repo) *service.LKProcessor {
	t.Helper()
	intake, _ := newIntake(t)
	actual := service.NewActualCache(repo)
	require.NoError(t, actual.Load(context.Background()))
	return service.NewLKProcessor(intake, repo, actual, s9c(t, s9), s6c(t, newFakeStatus6()), newFakeHistory())
}

// missing8 — запись-8 (пропавший) для fake-status9.
func missing8(id, vagon, index, naznach string, timeOp *domain.LocalTime) domain.Dislocation {
	s8 := 8
	return domain.Dislocation{
		ID: id, Vagon: vagon, Index: index, IndexMain: index,
		StanNazn: "Мыс А.", Naznach: naznach, GruzpolS: naznach,
		StationOper: "Тайшет", TimeOp: timeOp, Status: &s8,
	}
}

// Подтверждение прибытия пропавшего: веха в историю (статус 10 + поля прибытия;
// строки рейса не было — вставлена фолбэком), запись-8 снята, снимок не тронут,
// действие в журнале.
func TestConfirmMissing(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC))
	defer restore()

	timeOp := domain.NewLocalTime(time.Date(2026, 7, 25, 8, 0, 0, 0, time.UTC))
	s9repo := newFakeStatus9()
	s9repo.vagons["111"], s9repo.vagons["222"] = 8, 8
	s9repo.missing = []domain.Dislocation{
		missing8("M1", "111", "9379-783-9857", "АЭ", timeOp),
		missing8("M2", "222", "9379-783-9857", "АЭ", timeOp),
	}
	repo := &fakeDislRepo{}
	proc := newMissingProc(t, repo, s9repo)
	jr := &fakeJournalRepo{}
	proc.SetJournal(service.NewJournal(jr, nil))

	hist := newFakeHistory()
	svc := service.NewArrivalsService(hist, nil, proc)

	prib := domain.NewLocalTime(time.Date(2026, 7, 26, 9, 30, 0, 0, time.UTC))
	res, err := svc.ConfirmMissing(context.Background(), service.ConfirmMissingRequest{
		VagonIDs: []string{"M1"}, DatePrib: prib,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, res.Updated)

	// Строки рейса в истории не было — вставлена из записи-8, веха поверх.
	require.Len(t, hist.inserted, 1)
	assert.Equal(t, "M1", hist.inserted[0].ID)
	require.Contains(t, hist.updatedBatch, "M1")
	f := hist.updatedBatch["M1"]
	assert.Equal(t, 10, f["status"])
	assert.Equal(t, prib, f["date_prib"])
	assert.Equal(t, "АЭ", f["naznach"])
	assert.Equal(t, "9379-783-9857", f["index_pp"])
	assert.Nil(t, f["date_vigr"], "выгрузка не запрошена")

	// Запись-8 снята (только выбранная), снимок не подменялся.
	assert.Equal(t, []string{"111"}, s9repo.deleted)
	_, still := s9repo.vagons["222"]
	assert.True(t, still, "чужой пропавший не тронут")
	assert.Equal(t, 0, repo.calls, "снимок дислокации не трогаем")

	// Журнал: кто/что восстановимо.
	require.Len(t, jr.events, 1)
	assert.Equal(t, domain.EventArrivalsEdit, jr.events[0].EventType)
	assert.Equal(t, "confirm_missing", jr.events[0].Source)
}

// Подтверждение с выгрузкой: статус 12, ЖД-сутки выгрузки по правилу «час ≥ 18 →
// +сутки», место по умолчанию — терминал назначения записи-8.
func TestConfirmMissingWithUnload(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC))
	defer restore()

	timeOp := domain.NewLocalTime(time.Date(2026, 7, 25, 8, 0, 0, 0, time.UTC))
	s9repo := newFakeStatus9()
	s9repo.vagons["111"] = 8
	s9repo.missing = []domain.Dislocation{missing8("M1", "111", "9379-783-9857", "АЭ", timeOp)}
	proc := newMissingProc(t, &fakeDislRepo{}, s9repo)
	hist := newFakeHistory()
	hist.existing["M1"] = struct{}{} // строка рейса уже есть — фолбэк не нужен
	svc := service.NewArrivalsService(hist, nil, proc)

	prib := domain.NewLocalTime(time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC))
	vigr := domain.NewLocalTime(time.Date(2026, 7, 26, 18, 30, 0, 0, time.UTC))
	_, err := svc.ConfirmMissing(context.Background(), service.ConfirmMissingRequest{
		VagonIDs: []string{"M1"}, DatePrib: prib, DateVigr: vigr,
	})
	require.NoError(t, err)

	assert.Empty(t, hist.inserted, "строка была — вставки нет")
	f := hist.updatedBatch["M1"]
	assert.Equal(t, 12, f["status"])
	assert.Equal(t, vigr, f["date_vigr"])
	vigrD, ok := f["date_vigr_d"].(*domain.LocalTime)
	require.True(t, ok)
	assert.Equal(t, "2026-07-27", vigrD.String()[:10], "вечерняя выгрузка — следующие ЖД-сутки")
	assert.Equal(t, "АЭ", f["place_vigr"], "место по умолчанию — назначение записи")
}

// Валидации и защита от гонки: понятные ошибки до какой-либо записи.
func TestConfirmMissingValidation(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC))
	defer restore()

	timeOp := domain.NewLocalTime(time.Date(2026, 7, 25, 8, 0, 0, 0, time.UTC))
	s9repo := newFakeStatus9()
	s9repo.vagons["111"] = 8
	s9repo.missing = []domain.Dislocation{missing8("M1", "111", "9379-783-9857", "АЭ", timeOp)}
	proc := newMissingProc(t, &fakeDislRepo{}, s9repo)
	hist := newFakeHistory()
	svc := service.NewArrivalsService(hist, nil, proc)

	lt := func(s string) *domain.LocalTime {
		tt, err := time.Parse("2006-01-02T15:04:05", s)
		require.NoError(t, err)
		return domain.NewLocalTime(tt)
	}
	cases := []struct {
		name string
		req  service.ConfirmMissingRequest
		want string
	}{
		{"без вагонов", service.ConfirmMissingRequest{DatePrib: lt("2026-07-26T09:00:00")}, "не выбраны вагоны"},
		{"без времени прибытия", service.ConfirmMissingRequest{VagonIDs: []string{"M1"}}, "не указано время прибытия"},
		{"прибытие в будущем", service.ConfirmMissingRequest{
			VagonIDs: []string{"M1"}, DatePrib: lt("2026-08-03T09:00:00")}, "в будущем"},
		{"прибытие раньше последней операции", service.ConfirmMissingRequest{
			VagonIDs: []string{"M1"}, DatePrib: lt("2026-07-24T09:00:00")}, "раньше последней операции"},
		{"выгрузка раньше прибытия", service.ConfirmMissingRequest{
			VagonIDs: []string{"M1"}, DatePrib: lt("2026-07-26T09:00:00"),
			DateVigr: lt("2026-07-26T08:00:00")}, "выгрузка раньше прибытия"},
		{"место без времени выгрузки", service.ConfirmMissingRequest{
			VagonIDs: []string{"M1"}, DatePrib: lt("2026-07-26T09:00:00"),
			PlaceVigr: "АЭ"}, "без времени выгрузки"},
		{"чужой id", service.ConfirmMissingRequest{
			VagonIDs: []string{"НЕТ"}, DatePrib: lt("2026-07-26T09:00:00")}, "уже не в пропавших"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.ConfirmMissing(context.Background(), tc.req)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
	assert.Empty(t, hist.updatedBatch, "ни одна веха не записана")
	assert.Empty(t, s9repo.deleted, "записи-8 целы")
}

// Неизвестный терминал выгрузки отвергается реестром портов.
func TestConfirmMissingUnknownTerminal(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC))
	defer restore()

	timeOp := domain.NewLocalTime(time.Date(2026, 7, 25, 8, 0, 0, 0, time.UTC))
	s9repo := newFakeStatus9()
	s9repo.vagons["111"] = 8
	s9repo.missing = []domain.Dislocation{missing8("M1", "111", "9379-783-9857", "АЭ", timeOp)}
	proc := newMissingProc(t, &fakeDislRepo{}, s9repo)
	dir := service.NewDirectoryCache(&stubDirRepo{ports: []domain.Ports{
		{Okpo: 1, NameS: "АЭ", Enabled: true},
	}})
	require.NoError(t, dir.Load(context.Background()))
	svc := service.NewArrivalsService(newFakeHistory(), dir, proc)

	prib := domain.NewLocalTime(time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC))
	vigr := domain.NewLocalTime(time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC))
	_, err := svc.ConfirmMissing(context.Background(), service.ConfirmMissingRequest{
		VagonIDs: []string{"M1"}, DatePrib: prib, DateVigr: vigr, PlaceVigr: "НЕТ-ТАКОГО",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "неизвестный терминал")
}
