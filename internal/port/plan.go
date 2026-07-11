package port

import (
	"context"

	"github.com/Gtport/DPmodule/internal/domain"
)

// PlanRepository — хранение сетки плана подвода (таблицы plan/plan_nitka).
// История: на одну станцию (plan_code) хранится несколько загрузок; каждая
// SavePlan добавляет новую (не затирает прежние). Фронт берёт список загрузок
// и по умолчанию — самую свежую.
type PlanRepository interface {
	// SavePlan сохраняет новую загрузку плана: INSERT заголовка (возвращает id) +
	// INSERT ниток одной транзакцией. Прежние загрузки станции не трогает.
	SavePlan(ctx context.Context, header domain.Plan, nitki []domain.PlanNitka) (int64, error)
	// ListPlans возвращает загрузки станции (свежие первыми) для выбора на фронте.
	ListPlans(ctx context.Context, planCode string) ([]domain.PlanSummary, error)
	// GetLatestPlan возвращает самую свежую загрузку станции (заголовок + нитки).
	// Нет загрузок → header.PlanCode == "" и пустой срез (без ошибки).
	GetLatestPlan(ctx context.Context, planCode string) (domain.Plan, []domain.PlanNitka, error)
	// GetPlanByID возвращает конкретную загрузку по id (заголовок + нитки).
	// Нет такой → header.PlanCode == "" и пустой срез (без ошибки).
	GetPlanByID(ctx context.Context, id int64) (domain.Plan, []domain.PlanNitka, error)
	// ListSF возвращает справочник sf (синонимы станций формирования) для подбора с.ф.
	ListSF(ctx context.Context) ([]domain.SFRecord, error)
}
