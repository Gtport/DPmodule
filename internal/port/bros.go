package port

import (
	"context"

	"github.com/Gtport/DPmodule/internal/domain"
)

// BrosReasonCodesRepository — чтение справочника кодов бросания (bros_reason_codes).
type BrosReasonCodesRepository interface {
	// ReasonCodes возвращает весь справочник, отсортированный по коду.
	ReasonCodes(ctx context.Context) ([]domain.BrosReasonCode, error)
}

// BrosRepository — снимок брошенных поездов (таблица bros). Кэш в RAM не держим:
// активных бросков немного, reconcile-слой читает их пакетом каждый пересбор.
// Update — динамический UPDATE только затронутых колонок (канон GORM-гибрида).
type BrosRepository interface {
	// Active — активные броски (status_br=true), новые первыми.
	Active(ctx context.Context) ([]domain.Bros, error)
	// Insert вставляет новый бросок.
	Insert(ctx context.Context, b domain.Bros) error
	// Update точечно обновляет колонки броска по id (ключи — имена колонок).
	Update(ctx context.Context, id string, fields map[string]any) error
	// History — завершённые броски (status_br=false) с пагинацией; второй
	// результат — общее число.
	History(ctx context.Context, limit, offset int) ([]domain.Bros, int, error)
	// Filter — броски по фильтру (терминалы/период/статус) с пагинацией;
	// второй результат — общее число (без limit/offset).
	Filter(ctx context.Context, f domain.BrosFilter) ([]domain.Bros, int, error)
	// GetByID — бросок по ключу (nil, nil — не найден).
	GetByID(ctx context.Context, id string) (*domain.Bros, error)
	// SearchByIndex — поиск по подстроке index_0/index_1 (активные и завершённые).
	SearchByIndex(ctx context.Context, q string) ([]domain.Bros, error)
}

// BrosJournalRepository — журнал бросков (таблица bros_journal). Одна запись в
// сутки на поезд (UPSERT по bros_id+date), история накапливается.
type BrosJournalRepository interface {
	// Upsert сохраняет запись за её сутки (INSERT или перезапись существующей на
	// эти сутки), возвращает id.
	Upsert(ctx context.Context, e domain.BrosJournalEntry) (int64, error)
	// ByBrosID — вся история записей броска, новые первыми.
	ByBrosID(ctx context.Context, brosID string) ([]domain.BrosJournalEntry, error)
	// Latest — последняя запись броска (nil, nil — записей нет).
	Latest(ctx context.Context, brosID string) (*domain.BrosJournalEntry, error)
}
