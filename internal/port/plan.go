package port

import (
	"context"

	"github.com/Gtport/DPmodule/internal/domain"
)

// PlanRepository — хранение сетки плана подвода (таблицы plan/plan_nitka). Модель
// «одна станция плана = один актуальный план»: ReplacePlan атомарно перезаписывает
// заголовок и все нитки для plan_code. GetPlan — чтение для фронта.
type PlanRepository interface {
	// ReplacePlan перезаписывает план станции: upsert заголовка + delete/insert ниток.
	ReplacePlan(ctx context.Context, header domain.Plan, nitki []domain.PlanNitka) error
	// GetPlan возвращает заголовок и нитки плана станции (по возрастанию ord).
	// Если плана нет — header.PlanCode == "" и пустой срез (без ошибки).
	GetPlan(ctx context.Context, planCode string) (domain.Plan, []domain.PlanNitka, error)
}
