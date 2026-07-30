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

	// Привязки «вагон → колонка» (nmtp_vagon_column, миграция 000055):
	// указание грузовладельца сильнее правил раскладки, живёт по номерам
	// вагонов и гаснет, когда вагон выпал из подхода.
	// VagonColumns — все привязки: vagon → nmtp_column.id.
	VagonColumns(ctx context.Context) (map[string]int64, error)
	// SetVagonColumns — назначить/переназначить привязку вагонам (upsert).
	SetVagonColumns(ctx context.Context, vagons []string, columnID int64, who string) error
	// DeleteVagonColumns — снять привязку (ручной возврат к правилам либо гашение).
	DeleteVagonColumns(ctx context.Context, vagons []string) error
}
