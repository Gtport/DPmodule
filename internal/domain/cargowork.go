package domain

// «Грузовая работа» — суточный учётный лист терминала (перенос gtport
// vigr_at/vigr_ut/vigr_gut на универсальную модель).
//
// Ключ строки — (ЖД-сутки, терминал, линия учёта). Линия задаётся справочником
// PortCargoLine: у одного терминала это может быть одна строка «всего», у
// другого — уголь/металл/чугун. В gtport то же самое было тремя таблицами с
// колонками coal_*/metal_*/chugun_*.

// Виды линий учёта терминала.
const (
	CargoLineUnload = "unload" // колонки таблицы выгрузки
	CargoLineLoad   = "load"   // строки таблицы погрузки
)

// PortCargoLine — строка справочника линий учёта терминала.
//
// CargoKey для выгрузки совпадает с cargo.cargo_group (по нему считаются вехи
// прибытия/выгрузки из истории); пустой — терминал ведёт учёт одной строкой.
// Для погрузки это произвольный код рода: погрузка вбивается руками и с
// историей вагонов не связана.
//
// Pc — перерабатывающая способность линии, ваг/сут. Пусто → берём из ports по
// роду груза (как Stage 4), т.е. по умолчанию цифра у них общая.
type PortCargoLine struct {
	ID        int64
	Terminal  string
	Kind      string
	CargoKey  string
	Label     string
	Pc        *int
	SortOrder int
	Enabled   bool
	// PlanLabel — метка колонки плана подвода, откуда берётся остаток на
	// станции («Остаток на 18:00»). Пусто — остаток для линии не подтягиваем.
	PlanLabel string
}

// CargoWorkRow — суточный учётный лист одной линии выгрузки.
//
// Три слоя полей, и путать их нельзя:
//
//	АВТО     — Ost18, OstSt, Prib, VigrStan, UsefulFormation, TotalFormation,
//	           Downtime. Пересобираются пересчётом суток; правки оператора
//	           сюда не попадают.
//	РУЧНЫЕ   — Plan, VigrFact, Prim. Только они приходят с фронта.
//	РАСЧЁТНЫЕ— Ost, Effectiv, Perepokaz. Считает сервер (в gtport формулы жили
//	           и на клиенте, и в репозитории — расходились).
type CargoWorkRow struct {
	ID       int64
	DateJd   LocalTime // учётные ЖД-сутки (= date_prib_d поездов)
	Terminal string
	CargoKey string

	// Авто-слой.
	Ost18           int    // остаток на начало суток (= Ost предыдущих суток)
	OstSt           int    // остаток на станции (план подвода, «Остаток на 18:00»)
	Prib            int    // прибыло (вехи истории)
	VigrStan        int    // выгружено по данным АСУ (вехи истории)
	UsefulFormation int    // сколько успели бы выгрузить за сутки
	TotalFormation  int    // сколько всего было подано
	Downtime        string // простой порта «Ч:ММ»

	// Ручной слой.
	Plan     int    // план выгрузки
	VigrFact int    // выгрузка по данным порта
	Prim     string // комментарий

	// Расчётный слой.
	Ost       int // Ost18 + Prib − VigrFact
	Effectiv  int // VigrFact / UsefulFormation × 100, %
	Perepokaz int // VigrStan − VigrFact

	AnalyticsJSON      string // снимок расчёта суток (CargoWorkAnalytics)
	TrainStructureJSON string // снимок состава поездов линии

	CreatedAt *LocalTime
	UpdatedAt *LocalTime
}

// Recalc пересчитывает производные поля. Держим в домене, чтобы формулы жили в
// одном месте: их применяют и пересчёт суток, и сохранение правок оператора.
//
// Эффективность при нулевом полезном образовании — 0, а не деление на ноль:
// порт не мог выгрузить ничего, сравнивать не с чем.
func (r *CargoWorkRow) Recalc() {
	r.Ost = r.Ost18 + r.Prib - r.VigrFact
	r.Perepokaz = r.VigrStan - r.VigrFact
	if r.UsefulFormation > 0 {
		r.Effectiv = int(float64(r.VigrFact)/float64(r.UsefulFormation)*100 + 0.5)
	} else {
		r.Effectiv = 0
	}
}

// CargoWorkLoadRow — суточная строка погрузки (целиком ручная).
type CargoWorkLoadRow struct {
	ID        int64
	DateJd    LocalTime
	Terminal  string
	CargoKey  string
	LoadFact  int
	Plan      int
	Ost       int
	CreatedAt *LocalTime
	UpdatedAt *LocalTime
}
