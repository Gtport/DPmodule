package port

import (
	"context"

	"github.com/Gtport/DPmodule/internal/domain"
)

// HistoryRepository — запись бизнес-истории рейса (vagon_history, §3.19). Кэш в RAM
// не держим (история растёт безгранично): существование id проверяем пакетным
// запросом по батчу. UpdateFields — динамический UPDATE только затронутых колонок.
type HistoryRepository interface {
	// ExistingIDs возвращает множество id из переданных, которые уже есть в vagon_history.
	ExistingIDs(ctx context.Context, ids []string) (map[string]struct{}, error)
	// Insert вставляет новые строки истории (полные вехи рейса).
	Insert(ctx context.Context, rows []domain.VagonHistory) error
	// UpdateFields точечно обновляет колонки строки по id (ключи — имена колонок).
	UpdateFields(ctx context.Context, id string, fields map[string]any) error
	// TripsForPamyatki — рейсы перечисленных вагонов для движка памяток ГУ-45:
	// только якорь привязки (date_prib) и состояние заполнения памятками.
	// Рейсы без date_prib не возвращаются — привязать памятку к ним не к чему.
	TripsForPamyatki(ctx context.Context, vagons []string) ([]domain.PamyatkaTrip, error)
	// ArrivedRows — строки истории с фактом прибытия: date_prib_d ∈ [from; to]
	// (даты без времени), naznach из набора (пустой набор — все). Для «Истории
	// прибывших» домашней страницы.
	ArrivedRows(ctx context.Context, from, to domain.LocalTime, naznach []string) ([]domain.VagonHistory, error)
	// RowsByIDs — строки истории по id (для правок «Истории прибывших»:
	// проверка доступа по датам и пересчёты по текущим значениям вагона).
	RowsByIDs(ctx context.Context, ids []string) ([]domain.VagonHistory, error)
	// UpdateFieldsBatch — точечные обновления НЕСКОЛЬКИХ строк одной транзакцией
	// (ключ карты — id, значение — колонки как в UpdateFields).
	UpdateFieldsBatch(ctx context.Context, updates map[string]map[string]any) error
	// DailyTerminalCounts — счётчики «Оперативки»: сколько вагонов прибыло
	// (date_prib_d, по naznach) и выгружено (date_vigr_d, по place_vigr) за
	// каждые ЖД-сутки диапазона [from; to]. Ключи карт: "yyyy-MM-dd|терминал".
	DailyTerminalCounts(ctx context.Context, from, to domain.LocalTime) (prib, vigr map[string]int, err error)
	// DailyCargoUnloaded — то же «выгружено», но с разбивкой ПО ГРУППЕ ГРУЗА:
	// «Грузовой работе» нужна отдельная цифра на каждую линию учёта терминала
	// (уголь/металл/чугун), тогда как «Оперативке» достаточно суммы. Ключ карты:
	// "yyyy-MM-dd|терминал|группа" (группа пустая, если у вагона её нет).
	DailyCargoUnloaded(ctx context.Context, from, to domain.LocalTime) (map[string]int, error)
	// LoadingDaily — погрузка в адрес терминалов по ЖД-суткам диапазона
	// [from; to] (отчёт «Погрузка»): строки-агрегаты по дате × терминалу ×
	// sms_1 × станции × клиенту × группе груза. Перегрузы (peregruz непустой)
	// исключены — это не фактическая погрузка (TARGET.md §3.17; в gtport
	// фильтра не было — осознанный отход).
	LoadingDaily(ctx context.Context, from, to domain.LocalTime) ([]domain.LoadingDailyRow, error)
}
