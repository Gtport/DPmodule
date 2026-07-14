package service

import (
	"context"
	"fmt"
	"sync"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
)

// ConfigCache — настроечная таблица (data_source, client_settings) в оперативной
// памяти. Грузится один раз при старте (Load), читается слоем приёма. Доступ под
// RWMutex — задел под горячую перезагрузку. Зеркалит подход DirectoryCache.
type ConfigCache struct {
	repo port.ConfigRepository

	mu           sync.RWMutex
	byID         map[string]domain.DataSource
	ordered      []domain.DataSource
	settings     domain.ClientSettings
	planProfiles []domain.PlanProfile // настроечные портреты станций плана (plan_profile)
}

func NewConfigCache(repo port.ConfigRepository) *ConfigCache {
	return &ConfigCache{
		repo:    repo,
		byID:    map[string]domain.DataSource{},
		ordered: nil,
	}
}

// Load загружает настроечную таблицу и атомарно заменяет содержимое кэша.
func (c *ConfigCache) Load(ctx context.Context) error {
	sources, err := c.repo.LoadDataSources(ctx)
	if err != nil {
		return fmt.Errorf("load data_source: %w", err)
	}
	settings, err := c.repo.LoadClientSettings(ctx)
	if err != nil {
		return fmt.Errorf("load client_settings: %w", err)
	}
	profiles, err := c.repo.LoadPlanProfiles(ctx)
	if err != nil {
		return fmt.Errorf("load plan_profile: %w", err)
	}

	byID := make(map[string]domain.DataSource, len(sources))
	for _, s := range sources {
		byID[s.ID] = s
	}

	c.mu.Lock()
	c.byID = byID
	c.ordered = sources
	c.settings = settings
	c.planProfiles = profiles
	c.mu.Unlock()
	return nil
}

// DataSource возвращает источник по id.
func (c *ConfigCache) DataSource(id string) (domain.DataSource, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.byID[id]
	return s, ok
}

// EnabledByCategory возвращает включённые источники категории в порядке sort_order.
func (c *ConfigCache) EnabledByCategory(category string) []domain.DataSource {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var out []domain.DataSource
	for _, s := range c.ordered {
		if s.Enabled && s.Category == category {
			out = append(out, s)
		}
	}
	return out
}

// Settings возвращает клиентские параметры (пороги приёма и пр.).
func (c *ConfigCache) Settings() domain.ClientSettings {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.settings
}

// PlanProfiles возвращает настроечные портреты станций плана (копия среза).
func (c *ConfigCache) PlanProfiles() []domain.PlanProfile {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]domain.PlanProfile, len(c.planProfiles))
	copy(out, c.planProfiles)
	return out
}

// Counts — сводка для логов после загрузки: всего источников и из них включённых.
func (c *ConfigCache) Counts() (total, enabled int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, s := range c.ordered {
		if s.Enabled {
			enabled++
		}
	}
	return len(c.ordered), enabled
}
