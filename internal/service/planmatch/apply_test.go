package planmatch

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/parser/plan"
)

func TestApply(t *testing.T) {
	tgt := targetSet("АЭ")
	now := domain.LocalTime(time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC))
	planMsk := time.Date(2026, 7, 9, 15, 0, 0, 0, time.UTC)
	planJd := time.Date(2026, 7, 9, 15, 0, 0, 0, time.UTC)
	st10 := 10

	old := domain.NewLocalTime(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	records := []domain.Dislocation{
		// матчится → перезаписывается новым планом (был старый)
		{Vagon: "V1", Naznach: "АЭ", GruzpolS: "АЭ", IndexPp: "OLD", PlanMsk: old, PlanJd: old},
		// целевой, не матчится, был план → очищается
		{Vagon: "V2", Naznach: "АЭ", GruzpolS: "АЭ", IndexPp: "STALE", PlanMsk: old},
		// статус 10 → очистка не трогает
		{Vagon: "V3", Naznach: "АЭ", GruzpolS: "АЭ", IndexPp: "KEEP", PlanMsk: old, Status: &st10},
		// чужой (не target) → не трогаем
		{Vagon: "V4", Naznach: "ЧУЖОЙ", GruzpolS: "ЧУЖОЙ", IndexPp: "X"},
		// целевой без плана, матчится → проставляется
		{Vagon: "V5", Naznach: "АЭ", GruzpolS: "АЭ"},
	}
	matches := []NitkaMatch{
		{Matched: true, Vagons: []string{"V1", "V5"},
			Nitka: plan.PlanNitka{IndexPp: "7438-011-1299", PlanMsk: planMsk, PlanJd: planJd}},
		{Matched: false, Vagons: nil, Nitka: plan.PlanNitka{IndexPp: "9999-000-0000"}},
	}

	out, stats := Apply(records, matches, tgt, now)
	require.Len(t, out, 5)

	byVagon := map[string]domain.Dislocation{}
	for _, r := range out {
		byVagon[r.Vagon] = r
	}

	// V1: перезаписан новым планом.
	assert.Equal(t, "7438-011-1299", byVagon["V1"].IndexPp)
	require.NotNil(t, byVagon["V1"].PlanMsk)
	assert.True(t, byVagon["V1"].PlanMsk.Time().Equal(planMsk))
	assert.Equal(t, now, byVagon["V1"].UpdatedAt)

	// V2: очищен.
	assert.Equal(t, "", byVagon["V2"].IndexPp)
	assert.Nil(t, byVagon["V2"].PlanMsk)
	assert.Equal(t, now, byVagon["V2"].UpdatedAt)

	// V3: статус 10 — не тронут.
	assert.Equal(t, "KEEP", byVagon["V3"].IndexPp)
	require.NotNil(t, byVagon["V3"].PlanMsk)
	assert.True(t, byVagon["V3"].PlanMsk.Time().Equal(old.Time()))

	// V4: чужой — не тронут.
	assert.Equal(t, "X", byVagon["V4"].IndexPp)

	// V5: проставлен.
	assert.Equal(t, "7438-011-1299", byVagon["V5"].IndexPp)
	require.NotNil(t, byVagon["V5"].PlanJd)
	assert.True(t, byVagon["V5"].PlanJd.Time().Equal(planJd))

	assert.Equal(t, 2, stats.Stamped, "V1, V5")
	assert.Equal(t, 2, stats.Cleared, "V1 (был OLD), V2 (был STALE)")
}

// Матч без нитки/пустой матч не трогает снимок вовсе.
func TestApply_NoMatchesClearsStaleOnly(t *testing.T) {
	tgt := targetSet("АЭ")
	now := domain.LocalTime(time.Now())
	records := []domain.Dislocation{
		{Vagon: "V1", Naznach: "АЭ", GruzpolS: "АЭ", IndexPp: "STALE"},
		{Vagon: "V2", Naznach: "АЭ", GruzpolS: "АЭ"},
	}
	out, stats := Apply(records, nil, tgt, now)
	assert.Equal(t, "", out[0].IndexPp)
	assert.Equal(t, 0, stats.Stamped)
	assert.Equal(t, 1, stats.Cleared)
}
