package service

import (
	"strings"

	"github.com/Gtport/DPmodule/internal/parser/plan"
)

// ApplyPlanProfiles переносит настроечные профили станций из ConfigCache в реестр
// парсера плана (plan.SetProfiles). Берём только ПЛАНОВЫЕ станции с непустым
// plan_code — у бесплановых профиля парсинга нет (для них Stage 4 работает по
// перерабатывающей способности, парсер плана не задействован). Возвращает число
// переданных профилей (для лога старта). Пусто → SetProfiles игнорирует, у парсера
// остаётся builtin-fallback (offline-утилиты/тесты).
func ApplyPlanProfiles(c *ConfigCache) int {
	if c == nil {
		return 0
	}
	m := make(map[string]plan.Profile)
	for _, p := range c.PlanProfiles() {
		code := strings.ToLower(strings.TrimSpace(p.PlanCode))
		if code == "" {
			continue
		}
		m[code] = plan.Profile{
			PlanCode:             code,
			OurTerminals:         p.OurTerminals,
			MatchRequiresNaznach: p.MatchRequiresNaznach,
		}
	}
	plan.SetProfiles(m)
	return len(m)
}
