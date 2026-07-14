package port

import (
	"context"

	"github.com/Gtport/DPmodule/internal/domain"
)

// ConfigRepository — порт загрузки настроечной таблицы (data_source,
// client_settings) из хранилища. Реализация на GORM — internal/repository/gorm.
// Кэш в RAM строит internal/service.ConfigCache, он зависит от этого интерфейса,
// а не от GORM.
type ConfigRepository interface {
	LoadDataSources(ctx context.Context) ([]domain.DataSource, error)
	LoadClientSettings(ctx context.Context) (domain.ClientSettings, error)
	LoadPlanProfiles(ctx context.Context) ([]domain.PlanProfile, error)
	LoadNitkaSchedule(ctx context.Context) ([]domain.NitkaSlot, error)
}
