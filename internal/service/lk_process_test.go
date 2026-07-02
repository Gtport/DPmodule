package service_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/service"
)

// fakeDislRepo — in-memory port.DislocationRepository для юнит-тестов процессора.
type fakeDislRepo struct {
	current  []domain.Dislocation // «текущий снимок» (для гарда потери данных)
	replaced []domain.Dislocation // что получил ReplaceActual
	calls    int
}

func (f *fakeDislRepo) LoadActual(context.Context) ([]domain.Dislocation, error) {
	return f.current, nil
}
func (f *fakeDislRepo) ReplaceActual(_ context.Context, items []domain.Dislocation) error {
	f.replaced = items
	f.calls++
	return nil
}

// newProcessor собирает процессор поверх newIntake (тот же ConfigCache/DirectoryCache
// с активными портами 10230304 и 1126022) и fake-репозитория.
func newProcessor(t *testing.T, repo *fakeDislRepo) (*service.LKProcessor, string) {
	t.Helper()
	intake, dir := newIntake(t)
	return service.NewLKProcessor(intake, repo), dir
}

// Оба ожидаемых файла на месте, метки близки → снимок заменяется, обе записи в нём.
func TestLKProcess_OK(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 7, 2, 6, 10, 0, 0, time.UTC))
	defer restore()

	repo := &fakeDislRepo{}
	proc, dir := newProcessor(t, repo)
	stageWorkbook(t, dir, "1126022", "02.07.2026 06:05")
	stageWorkbook(t, dir, "10230304", "02.07.2026 06:00")

	res, err := proc.Process(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 2, res.Files)
	assert.Equal(t, 2, res.Count) // по одной записи из каждой книги
	assert.Equal(t, 0, res.PrevSnapshot)
	assert.Equal(t, 1, repo.calls)
	assert.Len(t, repo.replaced, 2)
}

// Нет файла одного из грузополучателей → контроль приёма блокирует, снимок цел.
func TestLKProcess_NotReadyBlocks(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 7, 2, 6, 10, 0, 0, time.UTC))
	defer restore()

	repo := &fakeDislRepo{}
	proc, dir := newProcessor(t, repo)
	stageWorkbook(t, dir, "10230304", "02.07.2026 06:00") // только Аттис

	_, err := proc.Process(context.Background())
	require.ErrorIs(t, err, service.ErrNotReady)
	assert.Equal(t, 0, repo.calls) // снимок не тронут
}

// Новый набор резко меньше текущего снимка → блок по потере данных.
func TestLKProcess_DataLossBlocks(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 7, 2, 6, 10, 0, 0, time.UTC))
	defer restore()

	// текущий снимок — 100 записей; новый будет 2 → потеря 98% ≥ 30%
	repo := &fakeDislRepo{current: make([]domain.Dislocation, 100)}
	proc, dir := newProcessor(t, repo)
	stageWorkbook(t, dir, "1126022", "02.07.2026 06:05")
	stageWorkbook(t, dir, "10230304", "02.07.2026 06:00")

	_, err := proc.Process(context.Background())
	require.ErrorIs(t, err, service.ErrDataLoss)
	assert.Equal(t, 0, repo.calls)
}

// Реальные выгрузки ЛК (НМТП + Аттис) — если файлы доступны локально.
func TestLKProcess_RealFixtures(t *testing.T) {
	nmtp := "/home/alex/projects/new_go/114_03.07.2026 01_20.xlsx"
	attis := "/home/alex/projects/new_go/114_03.07.2026 01_21.xlsx"
	if _, err := os.Stat(nmtp); err != nil {
		t.Skip("реальные фикстуры недоступны")
	}
	restore := clock.SetForTest(time.Date(2026, 7, 3, 1, 30, 0, 0, time.UTC))
	defer restore()

	repo := &fakeDislRepo{}
	proc, dir := newProcessor(t, repo)
	copyAsStaged(t, dir, "1126022", "03.07.2026 01:20", nmtp)
	copyAsStaged(t, dir, "10230304", "03.07.2026 01:21", attis)

	res, err := proc.Process(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, res.Files)
	assert.Equal(t, 4437+379, res.Count) // 4437 НМТП + 379 Аттис
}

// stageWorkbook кладёт синтетическую книгу ЛК под именем приёма <ОКПО>_<метка>.xlsx.
func stageWorkbook(t *testing.T, baseDir, okpo, formation string) {
	t.Helper()
	data := buildLKWorkbook(t, "Личный кабинет", formation, okpo)
	name := okpo + "_" + parseFmt(t, formation) + ".xlsx"
	stageBytes(t, baseDir, name, data)
}

// copyAsStaged кладёт реальный файл под именем приёма.
func copyAsStaged(t *testing.T, baseDir, okpo, formation, src string) {
	t.Helper()
	data, err := os.ReadFile(src)
	require.NoError(t, err)
	stageBytes(t, baseDir, okpo+"_"+parseFmt(t, formation)+".xlsx", data)
}

func parseFmt(t *testing.T, formation string) string {
	t.Helper()
	ts, err := time.Parse("02.01.2006 15:04", formation)
	require.NoError(t, err)
	return ts.Format("020106-1504")
}

func stageBytes(t *testing.T, baseDir, name string, data []byte) {
	t.Helper()
	dir := filepath.Join(baseDir, "lk")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0o644))
}
