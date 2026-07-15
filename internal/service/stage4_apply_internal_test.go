package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/domain"
)

func s4lt(y, mo, d, h, mi int) *domain.LocalTime {
	v := domain.LocalTime(time.Date(y, time.Month(mo), d, h, mi, 0, 0, time.UTC))
	return &v
}
func s4ip(n int) *int { return &n }

func TestCargoRodAndPc(t *testing.T) {
	assert.Equal(t, "coal", cargoRod("УГОЛЬ"))
	assert.Equal(t, "metal", cargoRod("МЕТАЛЛ"))
	assert.Equal(t, "other", cargoRod("ПРОЧЕЕ"))
	assert.Equal(t, "other", cargoRod(""))

	p := domain.Ports{PcCoal: s4ip(170), PcMetal: s4ip(90)} // PcOther nil
	assert.Equal(t, 170, pcForRod(p, "УГОЛЬ"))
	assert.Equal(t, 90, pcForRod(p, "МЕТАЛЛ"))
	assert.Equal(t, 0, pcForRod(p, "ПРОЧЕЕ"), "род без pc → 0")
}

func TestComputeProgDerived(t *testing.T) {
	// ProgMsk 20:00 (час ≥ 18) → ProgJd +сутки.
	r := domain.Dislocation{
		ProgMsk:    s4lt(2026, 7, 14, 20, 0),
		RaschMsk:   s4lt(2026, 7, 14, 8, 0),
		DateDostav: s4lt(2026, 7, 13, 0, 0),
	}
	computeProgDerived(&r, 72*time.Hour, 18)

	require.NotNil(t, r.ProgJd)
	assert.Equal(t, "2026-07-15T20:00:00", r.ProgJd.String(), "час≥18 → ProgJd +сутки")
	require.NotNil(t, r.DelayProg)
	assert.Equal(t, 1, *r.DelayProg, "ProgMsk−DateDostav ≈1.8 сут → 1 (вниз, ≥0)")
	require.NotNil(t, r.Mistake)
	assert.InDelta(t, 0.5, *r.Mistake, 0.001, "20:00−08:00 = 12ч = 0.5 сут")

	// брошенный (статус 5): база прибытия +72ч → Mistake уменьшается (может стать <0).
	st5 := 5
	r.Status = &st5
	computeProgDerived(&r, 72*time.Hour, 18)
	require.NotNil(t, r.Mistake)
	assert.InDelta(t, 0.5-3.0, *r.Mistake, 0.001, "eff = 08:00+72ч → mistake = 0.5 − 3 сут")
}

// минимальные стабы для сборки кэшей во внутреннем тесте.
type s4Dir struct{ ports []domain.Ports }

func (s4Dir) LoadStations(context.Context) ([]domain.Station, error)               { return nil, nil }
func (s4Dir) LoadCargoOperations(context.Context) ([]domain.CargoOperation, error) { return nil, nil }
func (s4Dir) LoadCargo(context.Context) ([]domain.Cargo, error)                    { return nil, nil }
func (s4Dir) LoadMarka(context.Context) ([]domain.Marka, error)                    { return nil, nil }
func (s s4Dir) LoadPorts(context.Context) ([]domain.Ports, error)                  { return s.ports, nil }
func (s4Dir) LoadRouteSpeed(context.Context) ([]domain.RouteSpeed, error)          { return nil, nil }
func (s4Dir) LoadNaznachStation(context.Context) ([]domain.NaznachStation, error)  { return nil, nil }

type s4Cfg struct {
	profiles []domain.PlanProfile
	slots    []domain.NitkaSlot
	settings domain.ClientSettings
}

func (s4Cfg) LoadDataSources(context.Context) ([]domain.DataSource, error) { return nil, nil }
func (s s4Cfg) LoadClientSettings(context.Context) (domain.ClientSettings, error) {
	return s.settings, nil
}
func (s s4Cfg) LoadPlanProfiles(context.Context) ([]domain.PlanProfile, error) { return s.profiles, nil }
func (s s4Cfg) LoadNitkaSchedule(context.Context) ([]domain.NitkaSlot, error)  { return s.slots, nil }

func TestApplyStage4_Integration(t *testing.T) {
	ctx := context.Background()
	dir := NewDirectoryCache(s4Dir{ports: []domain.Ports{
		{NameS: "АЭ", StationCode: "985702", PcCoal: s4ip(144)},
	}})
	require.NoError(t, dir.Load(ctx))
	cfg := NewConfigCache(s4Cfg{
		slots:    []domain.NitkaSlot{{StationCode: "985702", Hour: 6, Minute: 0, SortOrder: 1}, {StationCode: "985702", Hour: 18, Minute: 0, SortOrder: 2}},
		settings: domain.ClientSettings{Stage4: domain.Stage4Policy{MinVagonCount: 20, MinVagonBros: 10, BrosPenaltyH: 72}},
	})
	require.NoError(t, cfg.Load(ctx))

	st2 := 2
	var recs []domain.Dislocation
	// плановый поезд (5 вагонов): ProgMsk = PlanMsk.
	for i := 0; i < 5; i++ {
		recs = append(recs, domain.Dislocation{
			IdDisl: "P", StanNazn: "МЫС", Naznach: "АЭ", CargoGroup: "УГОЛЬ", Status: &st2,
			PlanMsk: s4lt(2026, 7, 15, 6, 0), RaschMsk: s4lt(2026, 7, 14, 3, 0),
		})
	}
	// беспланный поезд (25 вагонов, есть RaschMsk): получит слот.
	for i := 0; i < 25; i++ {
		recs = append(recs, domain.Dislocation{
			IdDisl: "N", StanNazn: "МЫС", Naznach: "АЭ", CargoGroup: "УГОЛЬ", Status: &st2,
			RaschMsk: s4lt(2026, 7, 14, 3, 0),
		})
	}

	n := applyStage4(recs, dir, cfg, 18)
	assert.Equal(t, 30, n, "все 30 записей получили ProgMsk")

	// плановый: ProgMsk = PlanMsk.
	assert.Equal(t, "2026-07-15T06:00:00", recs[0].ProgMsk.String())
	// беспланный: попал на слот станции (06:00 или 18:00), не пустой.
	require.NotNil(t, recs[5].ProgMsk)
	assert.Contains(t, []string{"06:00:00", "18:00:00"}, recs[5].ProgMsk.String()[11:], "беспланный на слот расписания")
}
