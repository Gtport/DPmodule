package port

import (
	"context"

	"github.com/Gtport/DPmodule/internal/domain"
)

// CargoWorkRepository — хранение суточных учётных листов «Грузовой работы»
// и чтение справочника линий учёта терминалов.
//
// Upsert по естественному ключу (date_jd, terminal, cargo_key): пересчёт суток
// и правка оператора попадают в одну и ту же строку, отдельного «создать»
// не требуется (в gtport для этого была развилка isNewRecord с разным
// поведением, из-за которой аналитика замораживалась при создании).
type CargoWorkRepository interface {
	// Lines — справочник линий учёта (enabled), порядок: терминал, вид, sort_order.
	Lines(ctx context.Context) ([]domain.PortCargoLine, error)

	// Rows — учётные листы выгрузки за диапазон ЖД-суток (включительно),
	// опционально по одному терминалу (пусто — все).
	Rows(ctx context.Context, from, to domain.LocalTime, terminal string) ([]domain.CargoWorkRow, error)
	// LoadRows — то же для строк погрузки.
	LoadRows(ctx context.Context, from, to domain.LocalTime, terminal string) ([]domain.CargoWorkLoadRow, error)

	// UpsertRows сохраняет учётные листы выгрузки (по ключу), одной транзакцией.
	UpsertRows(ctx context.Context, rows []domain.CargoWorkRow) error
	// UpsertLoadRows — то же для погрузки.
	UpsertLoadRows(ctx context.Context, rows []domain.CargoWorkLoadRow) error

	// DeleteDay удаляет учёт за сутки по терминалу (выгрузку и погрузку разом).
	DeleteDay(ctx context.Context, day domain.LocalTime, terminal string) error
}
