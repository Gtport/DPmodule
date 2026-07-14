package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/parser/plan"
	"github.com/Gtport/DPmodule/internal/service"
)

// Golden: профили из настроечной таблицы дают тот же plan.Profile, что был зашит в
// builtinProfiles — поведение матча/парсинга не меняется. Плюс бесплановая станция
// (без plan_code) в реестр парсера не попадает.
func TestApplyPlanProfiles_MatchesBuiltin(t *testing.T) {
	repo := &stubConfigRepo{profiles: []domain.PlanProfile{
		{StationCode: "985702", StationName: "МЫС АСТАФЬЕВА", Mode: domain.PlanModePlanned,
			PlanCode: "ma", OurTerminals: []string{"НАХОДКИНСКИЙ", "НМТП", "АТТИС"}},
		{StationCode: "984700", StationName: "НАХОДКА", Mode: domain.PlanModePlanned,
			PlanCode: "nk", MatchRequiresNaznach: true, OurTerminals: []string{"НАХОДКИНСКИЙ", "НМТП"}},
		{StationCode: "999999", StationName: "БЕЗ ПЛАНА", Mode: domain.PlanModeCapacity,
			PlanCode: "", CorrectionCoef: 0.9}, // бесплановая — в реестр парсера не идёт
	}}
	cache := service.NewConfigCache(repo)
	require.NoError(t, cache.Load(context.Background()))

	n := service.ApplyPlanProfiles(cache)
	assert.Equal(t, 2, n, "в реестр парсера — только плановые ma/nk")

	ma, err := plan.ResolveProfile("ma")
	require.NoError(t, err)
	assert.Equal(t, []string{"НАХОДКИНСКИЙ", "НМТП", "АТТИС"}, ma.OurTerminals)
	assert.False(t, ma.MatchRequiresNaznach)

	nk, err := plan.ResolveProfile("nk")
	require.NoError(t, err)
	assert.Equal(t, []string{"НАХОДКИНСКИЙ", "НМТП"}, nk.OurTerminals)
	assert.True(t, nk.MatchRequiresNaznach)
}
