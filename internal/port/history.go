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
	// ExistingTrips возвращает уже записанные рейсы по НАСТОЯЩЕМУ ключу рейса
	// (trip_key = вагон + дата начала рейса, уникальный индекс таблицы) в виде
	// trip_key → id строки.
	//
	// Зачем отдельно от ExistingIDs: id строки истории включает ещё и станцию
	// отправления, а trip_key — нет. Если у вагона станция отправления поменялась
	// (например, раньше её не удавалось прочитать), id получается новый, рейс
	// выглядит несуществующим — и вставка падает на уникальном trip_key. Искать
	// рейс нужно тем ключом, которым его опознаёт база.
	ExistingTrips(ctx context.Context, tripKeys []int64) (map[int64]string, error)
	// Insert вставляет новые строки истории (полные вехи рейса).
	Insert(ctx context.Context, rows []domain.VagonHistory) error
	// UpdateFields точечно обновляет колонки строки по id (ключи — имена колонок).
	UpdateFields(ctx context.Context, id string, fields map[string]any) error
	// TripsForPamyatki — рейсы перечисленных вагонов для движка памяток ГУ-45:
	// только якорь привязки (date_prib) и состояние заполнения памятками.
	// Рейсы без date_prib не возвращаются — привязать памятку к ним не к чему.
	TripsForPamyatki(ctx context.Context, vagons []string) ([]domain.PamyatkaTrip, error)
	// TripsForGU2B — рейсы перечисленных вагонов для движка уведомлений ГУ-2б:
	// якорь замка (date_prib) и текущие вехи выгрузки. Рейсы без date_prib не
	// возвращаются (замка нет), «недоехавшие» — возвращаются с флагом: движок
	// обязан их видеть и пропускать осознанно.
	TripsForGU2B(ctx context.Context, vagons []string) ([]domain.GU2BTrip, error)
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
	// FillAttribution — дозаполнение бизнес-атрибуции строк истории, у которых
	// грузоотправитель ещё пуст (рейс попал в историю несматченным с marka):
	// обновляются ТОЛЬКО строки WHERE gruzotpr = '' — заполненная атрибуция,
	// в т.ч. внесённая вручную, не перетирается. Возвращает число заполненных.
	FillAttribution(ctx context.Context, rows []domain.HistoryAttribution) (int, error)
	// DailyTerminalCounts — счётчики «Оперативки»: сколько вагонов погружено
	// в адрес терминала (date_nach_d, по gruzpol_s, без перегрузов — та же
	// семантика, что у отчёта «Погрузка»), прибыло (date_prib_d, по naznach)
	// и выгружено (date_vigr_d, по place_vigr) за каждые ЖД-сутки диапазона
	// [from; to]. Ключи карт: "yyyy-MM-dd|терминал".
	DailyTerminalCounts(ctx context.Context, from, to domain.LocalTime) (pogr, prib, vigr map[string]int, err error)
	// NotUnloadedCounts — «не выгружено» для «Оперативки» ПО ИСТОРИИ (решение
	// владельца 20.08.2026, замена снимкового счёта: после переноса истории
	// gtport в истории есть прибывшие рейсы, чьих вагонов наш поток не видит —
	// снимковый счётчик их не досчитывал). Считаются прибывшие гружёные рейсы
	// без вехи выгрузки: status = 10, place_vigr пуст, не «недоехавший»,
	// прибытие не раньше pribFrom (отсечка старых хвостов без актов). Порожние
	// под погрузку (ves 0/пуст) исключены — их рейс остаётся с прибытием без
	// выгрузки навсегда (выбытие-10 порожняка веху не пишет). Ключ карты —
	// терминал (naznach).
	NotUnloadedCounts(ctx context.Context, pribFrom domain.LocalTime) (map[string]int, error)
	// DailyCargoUnloaded — то же «выгружено», но с разбивкой ПО ГРУППЕ ГРУЗА:
	// «Грузовой работе» нужна отдельная цифра на каждую линию учёта терминала
	// (уголь/металл/чугун), тогда как «Оперативке» достаточно суммы. Ключ карты:
	// "yyyy-MM-dd|терминал|группа" (группа пустая, если у вагона её нет).
	DailyCargoUnloaded(ctx context.Context, from, to domain.LocalTime) (map[string]int, error)
	// PerestanovkaRows — строки истории с перестановкой (получатель ≠ назначение,
	// оба заполнены) за период: byVigr=false — по дате прибытия (date_prib_d),
	// true — по дате выгрузки (date_vigr_d). Отчёт «Факт перестановок».
	PerestanovkaRows(ctx context.Context, from, to domain.LocalTime, byVigr bool) ([]domain.VagonHistory, error)
	// LoadingDaily — погрузка в адрес терминалов по ЖД-суткам диапазона
	// [from; to] (отчёт «Погрузка»): строки-агрегаты по дате × терминалу ×
	// sms_1 × станции × клиенту × группе груза. Перегрузы (peregruz непустой)
	// исключены — это не фактическая погрузка (TARGET.md §3.17; в gtport
	// фильтра не было — осознанный отход).
	LoadingDaily(ctx context.Context, from, to domain.LocalTime) ([]domain.LoadingDailyRow, error)
	// SearchRows — страница экрана «Работа с историческими данными»: строки по
	// фильтру + общее число подходящих (для пагинации). orderCol — имя колонки
	// СТРОГО из белого списка сервиса (в SQL попадает как есть); сортировка
	// дополняется NULLS LAST и стабилизирующим хвостом vagon, id — иначе при
	// равных значениях страницы «плавают».
	SearchRows(ctx context.Context, f domain.HistorySearchFilter, orderCol string, desc bool, limit, offset int) (rows []domain.VagonHistory, total int, err error)
	// IterateSearch — курсорный обход ВСЕГО отфильтрованного набора в порядке
	// сортировки (экспорт Excel): строки читаются по одной, 100 тыс. доменных
	// структур в память не материализуются. Ошибка fn прерывает обход.
	IterateSearch(ctx context.Context, f domain.HistorySearchFilter, orderCol string, desc bool, fn func(domain.VagonHistory) error) error
	// DistinctStationsNach — уникальные станции погрузки истории (словарь
	// фильтра «Станция погрузки»; в gtport словарь брали из marka — DISTINCT
	// честнее: покрывает и станции, которых в marka нет).
	DistinctStationsNach(ctx context.Context) ([]string, error)
}
