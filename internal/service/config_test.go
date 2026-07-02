package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/service"
)

// stubConfigRepo — in-memory реализация port.ConfigRepository для тестов.
type stubConfigRepo struct {
	sources  []domain.DataSource
	settings domain.ClientSettings
}

func (s *stubConfigRepo) LoadDataSources(context.Context) ([]domain.DataSource, error) {
	return s.sources, nil
}
func (s *stubConfigRepo) LoadClientSettings(context.Context) (domain.ClientSettings, error) {
	return s.settings, nil
}

func sampleConfig() *stubConfigRepo {
	return &stubConfigRepo{
		sources: []domain.DataSource{
			{
				ID: "lk", Name: "Дислокация из ЛК РЖД", Enabled: true,
				Ingest: domain.IngestUpload, Category: domain.CategoryDislocation,
				CoArrivalGroup: "dislocation",
				Config: domain.DataSourceConfig{
					Detect:         []string{"Личный кабинет"},
					AllowedExt:     []string{"xlsx", "xls"},
					MaxMB:          10,
					HeaderMarker:   "Номер вагона",
					DateCutoffHour: 18,
				},
			},
			{
				ID: "plan_ma", Name: "План Мыс Астафьева", Enabled: false,
				Ingest: domain.IngestUpload, Category: domain.CategoryPlan,
			},
		},
		settings: domain.ClientSettings{
			ClientName: "GTport (3 порта)",
			IngestPolicy: domain.IngestPolicy{
				Dislocation: domain.CategoryPolicy{
					MaxGapMinutes: 15, MaxStalenessMinutes: 60,
					RejectOlderThanCurrent: true, RejectOlderRoleExempt: "administrator",
					MaxDataLossPct: 30,
				},
				Plan: domain.CategoryPolicy{PlanMaxLagHours: 1},
			},
		},
	}
}

func TestConfigCache_LoadAndLookup(t *testing.T) {
	c := service.NewConfigCache(sampleConfig())
	require.NoError(t, c.Load(context.Background()))

	total, enabled := c.Counts()
	assert.Equal(t, 2, total)
	assert.Equal(t, 1, enabled) // plan_ma выключен

	t.Run("data source by id + config", func(t *testing.T) {
		ds, ok := c.DataSource("lk")
		require.True(t, ok)
		assert.Equal(t, domain.CategoryDislocation, ds.Category)
		assert.Equal(t, 18, ds.Config.DateCutoffHour)
		assert.Equal(t, "Номер вагона", ds.Config.HeaderMarker)

		_, ok = c.DataSource("nope")
		assert.False(t, ok)
	})

	t.Run("enabled by category (выключенные не попадают)", func(t *testing.T) {
		disl := c.EnabledByCategory(domain.CategoryDislocation)
		require.Len(t, disl, 1)
		assert.Equal(t, "lk", disl[0].ID)

		plans := c.EnabledByCategory(domain.CategoryPlan)
		assert.Empty(t, plans) // plan_ma выключен
	})

	t.Run("пороги приёма", func(t *testing.T) {
		p := c.Settings().IngestPolicy.Dislocation
		assert.Equal(t, 15, p.MaxGapMinutes)
		assert.Equal(t, 30, p.MaxDataLossPct)
		assert.True(t, p.RejectOlderThanCurrent)
		assert.Equal(t, 1, c.Settings().IngestPolicy.Plan.PlanMaxLagHours)
	})
}
