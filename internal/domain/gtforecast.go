package domain

// GtSnapshot — сохранённый план вкладки «Прогноз прибытия/выгрузки» (перенос
// gtport gt_saved_plans): слепок рассчитанного сеанса на дату × причальную
// станцию. Содержимое — json-строки ответа simulate и входа сеанса; сервер
// разбирает их только для CSV-аналитики, архивный просмотр рисует фронт.
type GtSnapshot struct {
	ID        int64
	PlanDate  LocalTime // сутки, на которые сохранён план (date)
	Station   string    // код причальной станции (режим вкладки)
	StartDate LocalTime // стартовые сутки расчёта
	DaysCount int
	Request   string // вход сеанса (скорости, правки, тумблер)
	Trains    string // очередь поездов
	Flows     string // диаграммы Ганта по потокам
	FreeSlots string // свободные нитки горизонта
	Journal   string // журнал what-if правок
	SavedBy   string
	// Паспорт расчёта (миграция 000064): момент расчёта, вид (manual|auto),
	// jsonb-паспорт (скорости линий, отсечка, горизонт, правки).
	ComputedAt *LocalTime
	Kind       string
	Meta       string
	CreatedAt  *LocalTime
	UpdatedAt  *LocalTime
}

// Виды снапшота прогноза ГТ.
const (
	GtSnapshotManual = "manual" // кнопка диспетчера
	GtSnapshotAuto   = "auto"   // ежедневный крон gt_snapshot
)
