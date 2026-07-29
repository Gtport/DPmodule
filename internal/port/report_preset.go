package port

import (
	"context"

	"github.com/Gtport/DPmodule/internal/domain"
)

// ReportPresetRepository — чтение пресетов отчётных форм (таблица report_preset).
// Данные мелкие и меняются редко (правка — через админ-редактор), кэш не нужен.
type ReportPresetRepository interface {
	// List — включённые пресеты формы report, по sort_order.
	List(ctx context.Context, report string) ([]domain.ReportPreset, error)
}
