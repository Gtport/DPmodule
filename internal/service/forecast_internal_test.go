package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/domain"
)

func fcDir(t *testing.T, stations []domain.Station, rs []domain.RouteSpeed) *DirectoryCache {
	t.Helper()
	c := NewDirectoryCache(markaStubRepo{stations: stations, routeSpeed: rs})
	require.NoError(t, c.Load(context.Background()))
	return c
}

func ltm(y, mo, d, h, mi int) *domain.LocalTime {
	v := domain.LocalTime(time.Date(y, time.Month(mo), d, h, mi, 0, 0, time.UTC))
	return &v
}

var fcStations = []domain.Station{
	{Kod: 100, Name: "СТ100", IsBam: false},
	{Kod: 200, Name: "УЛАК", IsBam: false},
	{Kod: 300, Name: "BAMST", IsBam: true},
}

var fcRouteSpeed = []domain.RouteSpeed{
	{StationNach: "*", IsBam: false, FromKm: 1364, Speed: 34},
	{StationNach: "*", IsBam: false, FromKm: 911, Speed: 30},
	{StationNach: "*", IsBam: false, FromKm: 0, Speed: 27},
	{StationNach: "УЛАК", IsBam: false, FromKm: 1364, Speed: 20},
	{StationNach: "УЛАК", IsBam: false, FromKm: 911, Speed: 20},
	{StationNach: "УЛАК", IsBam: false, FromKm: 0, Speed: 27},
	{StationNach: "*", IsBam: true, FromKm: 0, Speed: 50}, // БАМ-профиль по умолчанию
}

func ptrInt(v int) *int { return &v }

func TestComputeToGo(t *testing.T) {
	dir := fcDir(t, fcStations, fcRouteSpeed)

	t.Run("профиль по умолчанию (три участка)", func(t *testing.T) {
		// AlternativeMove=0 → isBam=false; профиль по имени StationNach
		r := &domain.Dislocation{StationNach: "СТ100", RasstStanNazn: ptrInt(2000)}
		computeToGo(r, dir)
		require.NotNil(t, r.ToGo)
		// 636/34 + 453/30 + 911/27
		want := 636.0/34 + 453.0/30 + 911.0/27
		assert.InDelta(t, want, *r.ToGo, 1e-6)
	})

	t.Run("переопределение станции (УЛАК)", func(t *testing.T) {
		r := &domain.Dislocation{StationNach: "УЛАК", RasstStanNazn: ptrInt(2000)}
		computeToGo(r, dir)
		require.NotNil(t, r.ToGo)
		want := 636.0/20 + 453.0/20 + 911.0/27
		assert.InDelta(t, want, *r.ToGo, 1e-6)
	})

	t.Run("маркер AlternativeMove → БАМ-профиль", func(t *testing.T) {
		// alternative_move ≠ 0 (проставлен на Stage 1 по станции операции) → (*,true)
		r := &domain.Dislocation{StationNach: "СТ100", RasstStanNazn: ptrInt(1000), AlternativeMove: 1}
		computeToGo(r, dir)
		require.NotNil(t, r.ToGo)
		assert.InDelta(t, 1000.0/50, *r.ToGo, 1e-6) // единый участок 50 км/ч
	})

	t.Run("расстояние 0 / nil → дефолт 72", func(t *testing.T) {
		r := &domain.Dislocation{StationNach: "СТ100", RasstStanNazn: ptrInt(0)}
		computeToGo(r, dir)
		require.NotNil(t, r.ToGo)
		assert.Equal(t, 72.0, *r.ToGo)

		r2 := &domain.Dislocation{StationNach: "СТ100"}
		computeToGo(r2, dir)
		assert.Equal(t, 72.0, *r2.ToGo)
	})
}

// Stage 1 проставляет alternative_move по стации ОПЕРАЦИИ (stations.is_bam).
func TestEnrichStations_AlternativeMoveFromBam(t *testing.T) {
	dir := fcDir(t, fcStations, fcRouteSpeed)
	e := NewEnricher(dir)
	nf := map[int]struct{}{}

	bam := &domain.Dislocation{CodeStationOper: "300"} // BAMST, is_bam=true
	e.enrichStations(bam, nf)
	assert.Equal(t, "BAMST", bam.StationOper)
	assert.Equal(t, 1, bam.AlternativeMove)

	plain := &domain.Dislocation{CodeStationOper: "100"} // СТ100, is_bam=false
	e.enrichStations(plain, nf)
	assert.Equal(t, 0, plain.AlternativeMove)
}

func TestComputeRaschMskJd(t *testing.T) {
	t.Run("RaschMsk = TimeOp + ToGo + простои + буфер статуса 0", func(t *testing.T) {
		togo := 10.0
		r := &domain.Dislocation{
			TimeOp: ltm(2026, 7, 1, 8, 0), ToGo: &togo,
			ProstDn: ptrInt(1), ProstCh: ptrInt(2), Status: ptrInt(0),
		}
		computeRaschMsk(r)
		require.NotNil(t, r.RaschMsk)
		// 08:00 +10ч = 18:00; +1 сут = 02 18:00; +2ч = 02 20:00; +12ч(статус0) = 03 08:00
		assert.Equal(t, "2026-07-03T08:00:00", time.Time(*r.RaschMsk).Format("2006-01-02T15:04:05"))
	})

	t.Run("без буфера для статуса ≠ 0", func(t *testing.T) {
		togo := 4.0
		r := &domain.Dislocation{TimeOp: ltm(2026, 7, 1, 8, 0), ToGo: &togo, Status: ptrInt(2)}
		computeRaschMsk(r)
		require.NotNil(t, r.RaschMsk)
		assert.Equal(t, "2026-07-01T12:00:00", time.Time(*r.RaschMsk).Format("2006-01-02T15:04:05"))
	})

	t.Run("пустой TimeOp → RaschMsk не считается", func(t *testing.T) {
		r := &domain.Dislocation{Status: ptrInt(2)}
		computeRaschMsk(r)
		assert.Nil(t, r.RaschMsk)
	})

	t.Run("RaschJd: час ≥ cutoff → +сутки", func(t *testing.T) {
		r := &domain.Dislocation{RaschMsk: ltm(2026, 7, 1, 19, 0)}
		computeRaschJd(r, 18)
		require.NotNil(t, r.RaschJd)
		assert.Equal(t, "2026-07-02T19:00:00", time.Time(*r.RaschJd).Format("2006-01-02T15:04:05"))

		r2 := &domain.Dislocation{RaschMsk: ltm(2026, 7, 1, 10, 0)}
		computeRaschJd(r2, 18)
		assert.Equal(t, "2026-07-01T10:00:00", time.Time(*r2.RaschJd).Format("2006-01-02T15:04:05"))
	})
}

func TestApplyForecast_SkipsArrived(t *testing.T) {
	dir := fcDir(t, fcStations, fcRouteSpeed)
	kept := []domain.Dislocation{
		{Vagon: "V1", Status: ptrInt(2), CodeStationNach: "100", StationNach: "СТ100", RasstStanNazn: ptrInt(1000), TimeOp: ltm(2026, 7, 1, 8, 0)},
		{Vagon: "V2", Status: ptrInt(9), CodeStationNach: "100", StationNach: "СТ100", RasstStanNazn: ptrInt(1000), TimeOp: ltm(2026, 7, 1, 8, 0)},  // кандидат — пропуск
		{Vagon: "V3", Status: ptrInt(10), CodeStationNach: "100", StationNach: "СТ100", RasstStanNazn: ptrInt(1000), TimeOp: ltm(2026, 7, 1, 8, 0)}, // прибыл — пропуск
		{Vagon: "V4", Status: ptrInt(12), CodeStationNach: "100", StationNach: "СТ100", RasstStanNazn: ptrInt(1000), TimeOp: ltm(2026, 7, 1, 8, 0)}, // порожний в порту — пропуск
	}
	n := applyForecast(kept, dir, 18)

	assert.Equal(t, 1, n) // только V1
	assert.NotNil(t, kept[0].ToGo)
	assert.NotNil(t, kept[0].RaschMsk)
	assert.Nil(t, kept[1].ToGo)
	assert.Nil(t, kept[2].ToGo)
	assert.Nil(t, kept[3].RaschMsk)
}
