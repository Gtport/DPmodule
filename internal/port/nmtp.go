package port

import "context"

import "github.com/Gtport/DPmodule/internal/domain"

// NmtpRepository — справочники раскладки НМТП-отчёта (миграция 000053).
// Читаются при каждом построении отчёта: частота низкая, кэш не нужен —
// правки в Админе видны сразу, без «Обновить справочники».
type NmtpRepository interface {
	// Columns — включённые колонки терминала в порядке sort_order.
	Columns(ctx context.Context, terminal string) ([]domain.NmtpColumn, error)
	// Marks — словарь марок груза для нормализатора (канонические имена).
	Marks(ctx context.Context) ([]string, error)
	// Terminals — терминалы с настроенной раскладкой (для кнопок карточки).
	Terminals(ctx context.Context) ([]string, error)
}
