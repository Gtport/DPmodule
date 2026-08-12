package port

import (
	"context"
	"time"

	"github.com/Gtport/DPmodule/internal/domain"
)

// NmtpRepository — справочники раскладки НМТП-отчёта (миграция 000053).
// Читаются при каждом построении отчёта: частота низкая, кэш не нужен —
// правки в Админе видны сразу, без «Обновить справочники».
type NmtpRepository interface {
	// Columns — включённые колонки терминала в порядке sort_order.
	Columns(ctx context.Context, terminal string) ([]domain.NmtpColumn, error)
	// Marks — словарь марок груза для нормализатора (канонические имена).
	Marks(ctx context.Context) ([]string, error)

	// Привязки «вагон → колонка» (nmtp_vagon_column, миграция 000055):
	// указание грузовладельца сильнее правил раскладки, живёт по номерам
	// вагонов и гаснет, когда вагон выпал из подхода, сменил рейс (дата приёма
	// позже привязки) или привязка старше страховочного срока.
	// VagonColumns — все привязки: vagon → колонка + момент назначения.
	VagonColumns(ctx context.Context) (map[string]domain.NmtpVagonBinding, error)
	// SetVagonColumns — назначить/переназначить привязку вагонам (upsert).
	SetVagonColumns(ctx context.Context, vagons []string, columnID int64, who string) error
	// DeleteVagonColumns — снять привязку (ручной возврат к правилам либо гашение).
	DeleteVagonColumns(ctx context.Context, vagons []string) error
	// DeleteVagonColumnsBefore — страховочная чистка привязок старше порога.
	DeleteVagonColumnsBefore(ctx context.Context, before time.Time) error
}
