package service_test

import (
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"strconv"
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

// fakeStatus9Repo — in-memory port.Status9Repository.
type fakeStatus9Repo struct {
	vagons   map[string]struct{}
	inserted []domain.Dislocation
	deleted  []string
}

func newFakeStatus9() *fakeStatus9Repo { return &fakeStatus9Repo{vagons: map[string]struct{}{}} }

func (f *fakeStatus9Repo) Vagons(context.Context) (map[string]struct{}, error) {
	out := make(map[string]struct{}, len(f.vagons))
	for k := range f.vagons {
		out[k] = struct{}{}
	}
	return out, nil
}
func (f *fakeStatus9Repo) InsertNew(_ context.Context, items []domain.Dislocation) (int, error) {
	n := 0
	for _, it := range items {
		if _, ok := f.vagons[it.Vagon]; ok {
			continue // конфликт по vagon → DoNothing
		}
		f.vagons[it.Vagon] = struct{}{}
		f.inserted = append(f.inserted, it)
		n++
	}
	return n, nil
}
func (f *fakeStatus9Repo) DeleteByVagons(_ context.Context, vagons []string) (int, error) {
	n := 0
	for _, v := range vagons {
		if _, ok := f.vagons[v]; ok {
			delete(f.vagons, v)
			f.deleted = append(f.deleted, v)
			n++
		}
	}
	return n, nil
}

// newProcessor собирает процессор поверх newIntake (тот же ConfigCache/DirectoryCache
// с активными портами 10230304 и 1126022), fake-репозитория снимка, ActualCache
// (пустой) и fake-таблицы кандидатов.
func newProcessor(t *testing.T, repo *fakeDislRepo) (*service.LKProcessor, string) {
	t.Helper()
	intake, dir := newIntake(t)
	actual := service.NewActualCache(repo)
	require.NoError(t, actual.Load(context.Background()))
	return service.NewLKProcessor(intake, repo, actual, newFakeStatus9()), dir
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

// Реальные выгрузки ЛК (НМТП + Аттис) сквозь весь конвейер 1a→2→1b — если локально
// доступны и файлы фикстур, и seed-справочник станций (нужен для обогащения имён и
// идентификации порта). 4816 записей полностью резолвятся во включённые порты.
func TestLKProcess_RealFixtures(t *testing.T) {
	nmtp := "/home/alex/projects/new_go/114_03.07.2026 01_20.xlsx"
	attis := "/home/alex/projects/new_go/114_03.07.2026 01_21.xlsx"
	stations, okSeed := loadSeedStations(t)
	if _, err := os.Stat(nmtp); err != nil || !okSeed {
		t.Skip("реальные фикстуры/seed недоступны")
	}
	restore := clock.SetForTest(time.Date(2026, 7, 3, 1, 30, 0, 0, time.UTC))
	defer restore()

	cc := service.NewConfigCache(sampleConfig())
	require.NoError(t, cc.Load(context.Background()))
	dc := service.NewDirectoryCache(&stubDirRepo{stations: stations, ports: realPorts()})
	require.NoError(t, dc.Load(context.Background()))

	repo := &fakeDislRepo{}
	actual := service.NewActualCache(repo)
	require.NoError(t, actual.Load(context.Background()))
	dir := t.TempDir()
	proc := service.NewLKProcessor(service.NewLKIntake(cc, dc, dir), repo, actual, newFakeStatus9())
	copyAsStaged(t, dir, "1126022", "03.07.2026 01:20", nmtp)
	copyAsStaged(t, dir, "10230304", "03.07.2026 01:21", attis)

	res, err := proc.Process(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, res.Files)
	assert.Equal(t, 4437+379, res.Count) // все 4816 резолвятся во включённые порты
	assert.Equal(t, 0, res.PortDisabled)
	// gruzpol_s заполнен идентификацией
	for _, r := range repo.replaced {
		require.Contains(t, []string{"ГУТ-2", "УТ-1", "АЭ"}, r.GruzpolS)
	}
}

func realPorts() []domain.Ports {
	return []domain.Ports{
		{Okpo: 10230304, Location: "МЫС АСТАФЬЕВА", Organisation: `ООО КОМПАНИЯ "АТТИС ЭНТЕРПРАЙС"`, NameS: "АЭ", Enabled: true},
		{Okpo: 1126022, Location: "МЫС АСТАФЬЕВА", Organisation: `АО "НАХОДКИНСКИЙ МТП"`, NameS: "ГУТ-2", Enabled: true},
		{Okpo: 1126022, Location: "НАХОДКА", Organisation: `АО "НАХОДКИНСКИЙ МТП"`, NameS: "УТ-1", Enabled: true},
	}
}

// loadSeedStations читает _reference/seed/stations.csv (вне git). false, если нет.
func loadSeedStations(t *testing.T) ([]domain.Station, bool) {
	t.Helper()
	f, err := os.Open("../../_reference/seed/stations.csv")
	if err != nil {
		return nil, false
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	require.NoError(t, err)
	out := make([]domain.Station, 0, len(rows))
	for i, row := range rows {
		if i == 0 || len(row) < 3 {
			continue
		}
		kod, _ := strconv.Atoi(row[0])
		kod4, _ := strconv.Atoi(row[1])
		out = append(out, domain.Station{Kod: kod, Kod4: kod4, Name: row[2], Road: row[3]})
	}
	return out, true
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
