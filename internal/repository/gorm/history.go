package gormrepo

import (
	"context"

	"gorm.io/gorm"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

// vagonHistoryModel — ORM-раскладка колонок vagon_history (§3.19). trip_key —
// генерируемая колонка, в модель НЕ включена (БД считает сама).
type vagonHistoryModel struct {
	ID          string            `gorm:"column:id;primaryKey"`
	Vagon       string            `gorm:"column:vagon"`
	InvoiceMain string            `gorm:"column:invoice_main"`
	Invoice     string            `gorm:"column:invoice"`
	IndexMain   string            `gorm:"column:index_main"`
	IndexPp     string            `gorm:"column:index_pp"`
	DateNachD   *domain.LocalTime `gorm:"column:date_nach_d"`
	StationNach string            `gorm:"column:station_nach"`
	Gruzotpr    string            `gorm:"column:gruzotpr"`
	Zayavka     string            `gorm:"column:zayavka"`
	StanNazn    string            `gorm:"column:stan_nazn"`
	GruzpolS    string            `gorm:"column:gruzpol_s"`
	Naznach     string            `gorm:"column:naznach"`
	CargoS      string            `gorm:"column:cargo_s"`
	CargoGroup  string            `gorm:"column:cargo_group"`

	FreightExactName string   `gorm:"column:freight_exact_name"`
	GtdNumber        string   `gorm:"column:gtd_number"`
	Ves              *float64 `gorm:"column:ves"`
	Client           string   `gorm:"column:client"`
	RodVagUch        string   `gorm:"column:rod_vag_uch"`
	CarOwnerName     string   `gorm:"column:car_owner_name"`
	CarOwnerOkpo     string   `gorm:"column:car_owner_okpo"`
	CarTenantName    string   `gorm:"column:car_tenant_name"`
	CarTenantOkpo    string   `gorm:"column:car_tenant_okpo"`
	CarTrustedName   string   `gorm:"column:car_trusted_name"`
	CarTrustedOkpo   string   `gorm:"column:car_trusted_okpo"`
	Owner            string   `gorm:"column:owner"`
	PereadrType      string   `gorm:"column:pereadr_type"`
	PereadrPort      string   `gorm:"column:pereadr_port"`

	Status     *int              `gorm:"column:status"`
	DateDostav *domain.LocalTime `gorm:"column:date_dostav"`
	PlanMsk    *domain.LocalTime `gorm:"column:plan_msk"`
	PlanJd     *domain.LocalTime `gorm:"column:plan_jd"`
	Otkl       string            `gorm:"column:otkl"`
	Delay      *int              `gorm:"column:delay"`

	DatePrib  *domain.LocalTime `gorm:"column:date_prib"`
	DatePribD *domain.LocalTime `gorm:"column:date_prib_d"`
	DateVigr  *domain.LocalTime `gorm:"column:date_vigr"`
	DateVigrD *domain.LocalTime `gorm:"column:date_vigr_d"`
	PlaceVigr string            `gorm:"column:place_vigr"`

	Frost     *int              `gorm:"column:frost"`
	Shipments string            `gorm:"column:shipments"`
	Peregruz  string            `gorm:"column:peregruz"`
	Info1     string            `gorm:"column:info_1"`
	Info2     string            `gorm:"column:info_2"`
	Sms1      string            `gorm:"column:sms_1"`
	Sms2      string            `gorm:"column:sms_2"`
	Sms3      string            `gorm:"column:sms_3"`
	Color     string            `gorm:"column:color"`
	// Недоехавший (рейс закрыт удалением из пропавших) — миграция 000062.
	NotArrived bool              `gorm:"column:not_arrived"`
	CreatedAt  *domain.LocalTime `gorm:"column:created_at"`
	UpdatedAt  *domain.LocalTime `gorm:"column:updated_at"`
}

func (vagonHistoryModel) TableName() string { return "dpport.vagon_history" }

func toHistoryModel(h domain.VagonHistory) vagonHistoryModel {
	return vagonHistoryModel{
		ID: h.ID, Vagon: h.Vagon, InvoiceMain: h.InvoiceMain, Invoice: h.Invoice,
		IndexMain: h.IndexMain, IndexPp: h.IndexPp, DateNachD: h.DateNachD,
		StationNach: h.StationNach, Gruzotpr: h.Gruzotpr, Zayavka: h.Zayavka,
		StanNazn: h.StanNazn, GruzpolS: h.GruzpolS, Naznach: h.Naznach,
		CargoS: h.CargoS, CargoGroup: h.CargoGroup,
		FreightExactName: h.FreightExactName, GtdNumber: h.GtdNumber, Ves: h.Ves,
		Client: h.Client, RodVagUch: h.RodVagUch,
		CarOwnerName: h.CarOwnerName, CarOwnerOkpo: h.CarOwnerOkpo,
		CarTenantName: h.CarTenantName, CarTenantOkpo: h.CarTenantOkpo,
		CarTrustedName: h.CarTrustedName, CarTrustedOkpo: h.CarTrustedOkpo,
		Owner:       h.Owner,
		PereadrType: h.PereadrType, PereadrPort: h.PereadrPort,
		Status: h.Status, DateDostav: h.DateDostav, PlanMsk: h.PlanMsk, PlanJd: h.PlanJd,
		Otkl: h.Otkl, Delay: h.Delay,
		DatePrib: h.DatePrib, DatePribD: h.DatePribD, DateVigr: h.DateVigr,
		DateVigrD: h.DateVigrD, PlaceVigr: h.PlaceVigr,
		Frost: h.Frost, Shipments: h.Shipments, Peregruz: h.Peregruz,
		Info1: h.Info1, Info2: h.Info2, Sms1: h.Sms1, Sms2: h.Sms2, Sms3: h.Sms3,
		Color: h.Color, NotArrived: h.NotArrived,
		CreatedAt: h.CreatedAt, UpdatedAt: h.UpdatedAt,
	}
}

// HistoryRepository реализует port.HistoryRepository.
type HistoryRepository struct {
	db *gorm.DB
}

func NewHistoryRepository(db *gorm.DB) *HistoryRepository {
	return &HistoryRepository{db: db}
}

func (r *HistoryRepository) ExistingIDs(ctx context.Context, ids []string) (map[string]struct{}, error) {
	out := map[string]struct{}{}
	if len(ids) == 0 {
		return out, nil
	}
	var found []string
	if err := r.db.WithContext(ctx).Model(&vagonHistoryModel{}).
		Where("id IN ?", ids).Pluck("id", &found).Error; err != nil {
		return nil, err
	}
	for _, id := range found {
		out[id] = struct{}{}
	}
	return out, nil
}

// ExistingTrips — уже записанные рейсы по trip_key (генерируемая колонка, она же
// уникальный индекс): trip_key → id строки. По id рейс искать нельзя — в него
// входит станция отправления, которая у вагона может появиться позже (см.
// комментарий в port.HistoryRepository).
func (r *HistoryRepository) ExistingTrips(ctx context.Context, tripKeys []int64) (map[int64]string, error) {
	out := map[int64]string{}
	if len(tripKeys) == 0 {
		return out, nil
	}
	var found []struct {
		TripKey int64  `gorm:"column:trip_key"`
		ID      string `gorm:"column:id"`
	}
	if err := r.db.WithContext(ctx).Model(&vagonHistoryModel{}).
		Select("trip_key, id").Where("trip_key IN ?", tripKeys).Scan(&found).Error; err != nil {
		return nil, err
	}
	for _, f := range found {
		out[f.TripKey] = f.ID
	}
	return out, nil
}

func (r *HistoryRepository) Insert(ctx context.Context, rows []domain.VagonHistory) error {
	if len(rows) == 0 {
		return nil
	}
	models := make([]vagonHistoryModel, len(rows))
	for i, h := range rows {
		models[i] = toHistoryModel(h)
	}
	return r.db.WithContext(ctx).CreateInBatches(models, batchSize).Error
}

func (r *HistoryRepository) UpdateFields(ctx context.Context, id string, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&vagonHistoryModel{}).
		Where("id = ?", id).Updates(fields).Error
}

// TripsForPamyatki — рейсы перечисленных вагонов в виде, нужном движку памяток:
// якорь привязки (date_prib) и текущее состояние заполнения. Полные строки
// истории не читаем — их 70 колонок, а решают четыре поля.
//
// Матч считает Go (domain.ApplyPamyatki), а не SQL: рейсов на пачку немного
// (боевая пачка за 3 суток — 2371 вагон, не больше двух рейсов на вагон), зато
// правила выбора рейса покрыты обычными тестами без базы.
func (r *HistoryRepository) TripsForPamyatki(ctx context.Context, vagons []string) ([]domain.PamyatkaTrip, error) {
	if len(vagons) == 0 {
		return nil, nil
	}
	var rows []struct {
		ID          string
		Vagon       string
		DatePrib    *domain.LocalTime
		State       int
		NomGu45Pod  string
		NomGu45Ubor string
		DateUbor    *domain.LocalTime
	}
	err := r.db.WithContext(ctx).Model(&vagonHistoryModel{}).
		Select("id, vagon, date_prib, pamyatka_state AS state, nom_gu45_pod, nom_gu45_ubor, date_ubor").
		Where("vagon IN ?", vagons).
		Where("date_prib IS NOT NULL"). // без прибытия привязать памятку не к чему
		Order("vagon, date_prib").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]domain.PamyatkaTrip, len(rows))
	for i, r := range rows {
		out[i] = domain.PamyatkaTrip{
			ID: r.ID, Vagon: r.Vagon, DatePrib: r.DatePrib, State: r.State,
			NomGu45Pod: r.NomGu45Pod, NomGu45Ubor: r.NomGu45Ubor, DateUbor: r.DateUbor,
		}
	}
	return out, nil
}

// TripsForGU2B — рейсы вагонов для движка уведомлений ГУ-2б: якорь замка и
// текущие вехи выгрузки (см. port.HistoryRepository).
func (r *HistoryRepository) TripsForGU2B(ctx context.Context, vagons []string) ([]domain.GU2BTrip, error) {
	if len(vagons) == 0 {
		return nil, nil
	}
	var rows []struct {
		ID         string
		Vagon      string
		DatePrib   *domain.LocalTime
		DateVigr   *domain.LocalTime
		PlaceVigr  string
		NotArrived bool
	}
	err := r.db.WithContext(ctx).Model(&vagonHistoryModel{}).
		Select("id, vagon, date_prib, date_vigr, place_vigr, not_arrived").
		Where("vagon IN ?", vagons).
		Where("date_prib IS NOT NULL"). // без прибытия замку не за что цепляться
		Order("vagon, date_prib").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]domain.GU2BTrip, len(rows))
	for i, w := range rows {
		out[i] = domain.GU2BTrip{
			ID: w.ID, Vagon: w.Vagon, DatePrib: w.DatePrib,
			DateVigr: w.DateVigr, PlaceVigr: w.PlaceVigr, NotArrived: w.NotArrived,
		}
	}
	return out, nil
}

// ArrivedRows — строки с фактом прибытия за период (date_prib_d ∈ [from; to]),
// с фильтром по терминалам naznach (пусто — все). Читает по индексу
// ix_vagon_history_date_prib_d; сортировка — стабильная, по времени прибытия.
func (r *HistoryRepository) ArrivedRows(ctx context.Context, from, to domain.LocalTime, naznach []string) ([]domain.VagonHistory, error) {
	q := r.db.WithContext(ctx).Model(&vagonHistoryModel{}).
		Where("date_prib IS NOT NULL").
		Where("date_prib_d BETWEEN ? AND ?", from, to)
	if len(naznach) > 0 {
		q = q.Where("naznach IN ?", naznach)
	}
	var ms []vagonHistoryModel
	if err := q.Order("date_prib, index_pp, vagon").Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]domain.VagonHistory, len(ms))
	for i, m := range ms {
		out[i] = toHistoryDomain(m)
	}
	return out, nil
}

// PerestanovkaRows — строки с перестановкой (получатель ≠ назначение, оба
// заполнены) за период: byVigr=false — по дате прибытия (date_prib_d), true —
// по дате выгрузки (date_vigr_d). Отчёт «Факт перестановок».
func (r *HistoryRepository) PerestanovkaRows(ctx context.Context, from, to domain.LocalTime, byVigr bool) ([]domain.VagonHistory, error) {
	dateCol := "date_prib_d"
	if byVigr {
		dateCol = "date_vigr_d"
	}
	var ms []vagonHistoryModel
	err := r.db.WithContext(ctx).Model(&vagonHistoryModel{}).
		Where(dateCol+" BETWEEN ? AND ?", from, to).
		Where("gruzpol_s <> '' AND naznach <> '' AND gruzpol_s <> naznach").
		Order(dateCol + ", index_pp, vagon").
		Find(&ms).Error
	if err != nil {
		return nil, err
	}
	out := make([]domain.VagonHistory, len(ms))
	for i, m := range ms {
		out[i] = toHistoryDomain(m)
	}
	return out, nil
}

// toHistoryDomain — обратный маппинг ORM-модели в доменную структуру (полный,
// зеркало toHistoryModel).
func toHistoryDomain(m vagonHistoryModel) domain.VagonHistory {
	return domain.VagonHistory{
		ID: m.ID, Vagon: m.Vagon, InvoiceMain: m.InvoiceMain, Invoice: m.Invoice,
		IndexMain: m.IndexMain, IndexPp: m.IndexPp, DateNachD: m.DateNachD,
		StationNach: m.StationNach, Gruzotpr: m.Gruzotpr, Zayavka: m.Zayavka,
		StanNazn: m.StanNazn, GruzpolS: m.GruzpolS, Naznach: m.Naznach,
		CargoS: m.CargoS, CargoGroup: m.CargoGroup,
		FreightExactName: m.FreightExactName, GtdNumber: m.GtdNumber, Ves: m.Ves,
		Client: m.Client, RodVagUch: m.RodVagUch,
		CarOwnerName: m.CarOwnerName, CarOwnerOkpo: m.CarOwnerOkpo,
		CarTenantName: m.CarTenantName, CarTenantOkpo: m.CarTenantOkpo,
		CarTrustedName: m.CarTrustedName, CarTrustedOkpo: m.CarTrustedOkpo,
		Owner:       m.Owner,
		PereadrType: m.PereadrType, PereadrPort: m.PereadrPort,
		Status: m.Status, DateDostav: m.DateDostav, PlanMsk: m.PlanMsk, PlanJd: m.PlanJd,
		Otkl: m.Otkl, Delay: m.Delay,
		DatePrib: m.DatePrib, DatePribD: m.DatePribD, DateVigr: m.DateVigr,
		DateVigrD: m.DateVigrD, PlaceVigr: m.PlaceVigr,
		Frost: m.Frost, Shipments: m.Shipments, Peregruz: m.Peregruz,
		Info1: m.Info1, Info2: m.Info2, Sms1: m.Sms1, Sms2: m.Sms2, Sms3: m.Sms3,
		Color: m.Color, NotArrived: m.NotArrived,
		CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

// RowsByIDs — строки истории по списку id (правки «Истории прибывших»).
func (r *HistoryRepository) RowsByIDs(ctx context.Context, ids []string) ([]domain.VagonHistory, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var ms []vagonHistoryModel
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]domain.VagonHistory, len(ms))
	for i, m := range ms {
		out[i] = toHistoryDomain(m)
	}
	return out, nil
}

// UpdateFieldsBatch — обновления нескольких строк одной транзакцией (правки
// оператора применяются атомарно: либо весь батч, либо ничего).
func (r *HistoryRepository) UpdateFieldsBatch(ctx context.Context, updates map[string]map[string]any) error {
	if len(updates) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for id, fields := range updates {
			if len(fields) == 0 {
				continue
			}
			if err := tx.Model(&vagonHistoryModel{}).Where("id = ?", id).Updates(fields).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// FillAttribution — дозаполнение бизнес-атрибуции строк, у которых грузоотправитель
// пуст (рейс вставлен несматченным с marka; historyUpdateFields атрибуцию не ведёт).
// Guard gruzotpr = '' в WHERE: заполненные строки — в т.ч. вручную — не трогаются,
// повторный вызов идемпотентен. Адресация по trip_key (уникальный индекс).
func (r *HistoryRepository) FillAttribution(ctx context.Context, rows []domain.HistoryAttribution) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	now := clock.Now()
	filled := 0
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, a := range rows {
			res := tx.Model(&vagonHistoryModel{}).
				Where("trip_key = ? AND gruzotpr = ''", a.TripKey).
				Updates(map[string]any{
					"gruzotpr": a.Gruzotpr, "client": a.Client,
					"sms_1": a.Sms1, "sms_2": a.Sms2, "sms_3": a.Sms3,
					"color": a.Color, "updated_at": now,
				})
			if res.Error != nil {
				return res.Error
			}
			filled += int(res.RowsAffected)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return filled, nil
}

// DailyTerminalCounts — агрегаты «Оперативки» (сырой SQL — канон для аналитики):
// погруженные в адрес терминала по ЖД-суткам (date_nach_d × gruzpol_s, без
// перегрузов — семантика отчёта «Погрузка», TARGET.md §3.17), прибывшие
// (date_prib_d × naznach) и выгруженные (date_vigr_d × place_vigr) за диапазон
// ЖД-суток.
func (r *HistoryRepository) DailyTerminalCounts(ctx context.Context, from, to domain.LocalTime) (map[string]int, map[string]int, map[string]int, error) {
	type row struct {
		Day  domain.LocalTime `gorm:"column:day"`
		Term string           `gorm:"column:term"`
		N    int              `gorm:"column:n"`
	}
	key := func(d domain.LocalTime, term string) string { return d.String()[:10] + "|" + term }
	toMap := func(rows []row) map[string]int {
		m := make(map[string]int, len(rows))
		for _, x := range rows {
			m[key(x.Day, x.Term)] = x.N
		}
		return m
	}

	var pogrRows []row
	if err := r.db.WithContext(ctx).Raw(`
		SELECT date_nach_d AS day, gruzpol_s AS term, count(*) AS n
		  FROM dpport.vagon_history
		 WHERE date_nach_d BETWEEN ? AND ? AND gruzpol_s <> ''
		   AND COALESCE(peregruz, '') = ''
		 GROUP BY date_nach_d, gruzpol_s`, from, to).Scan(&pogrRows).Error; err != nil {
		return nil, nil, nil, err
	}
	var pribRows []row
	if err := r.db.WithContext(ctx).Raw(`
		SELECT date_prib_d AS day, naznach AS term, count(*) AS n
		  FROM dpport.vagon_history
		 WHERE date_prib_d BETWEEN ? AND ? AND naznach <> ''
		 GROUP BY date_prib_d, naznach`, from, to).Scan(&pribRows).Error; err != nil {
		return nil, nil, nil, err
	}
	var vigrRows []row
	if err := r.db.WithContext(ctx).Raw(`
		SELECT date_vigr_d AS day, place_vigr AS term, count(*) AS n
		  FROM dpport.vagon_history
		 WHERE date_vigr_d BETWEEN ? AND ? AND place_vigr <> ''
		 GROUP BY date_vigr_d, place_vigr`, from, to).Scan(&vigrRows).Error; err != nil {
		return nil, nil, nil, err
	}

	return toMap(pogrRows), toMap(pribRows), toMap(vigrRows), nil
}

// NotUnloadedCounts — «не выгружено» по истории (сырой SQL — канон для
// аналитики): прибывшие гружёные рейсы без вехи выгрузки, по терминалам.
// Семантика «не выгружен» — как у фильтра «не выгруж.» экрана истории
// (place_vigr пуст И не «недоехавший»); порог pribFrom отсекает старые хвосты
// gtport без актов, ves > 0 — порожних под погрузку.
func (r *HistoryRepository) NotUnloadedCounts(ctx context.Context, pribFrom domain.LocalTime) (map[string]int, error) {
	var rows []struct {
		Term string `gorm:"column:term"`
		N    int    `gorm:"column:n"`
	}
	if err := r.db.WithContext(ctx).Raw(`
		SELECT naznach AS term, count(*) AS n
		  FROM dpport.vagon_history
		 WHERE status = 10 AND COALESCE(place_vigr, '') = '' AND NOT not_arrived
		   AND naznach <> '' AND COALESCE(ves, 0) > 0
		   AND date_prib_d >= ?
		 GROUP BY naznach`, pribFrom).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]int, len(rows))
	for _, x := range rows {
		out[x.Term] = x.N
	}
	return out, nil
}

// DailyCargoUnloaded — выгружено по ЖД-суткам/терминалу/группе груза (сырой SQL —
// канон для аналитики). Отдельный запрос, а не расширение DailyTerminalCounts:
// «Оперативке» разбивка не нужна, и менять её контракт ради «Грузовой работы»
// значило бы трогать работающий экран.
func (r *HistoryRepository) DailyCargoUnloaded(ctx context.Context, from, to domain.LocalTime) (map[string]int, error) {
	var rows []struct {
		Day   domain.LocalTime `gorm:"column:day"`
		Term  string           `gorm:"column:term"`
		Group string           `gorm:"column:grp"`
		N     int              `gorm:"column:n"`
	}
	if err := r.db.WithContext(ctx).Raw(`
		SELECT date_vigr_d AS day, place_vigr AS term,
		       COALESCE(cargo_group, '') AS grp, count(*) AS n
		  FROM dpport.vagon_history
		 WHERE date_vigr_d BETWEEN ? AND ? AND place_vigr <> ''
		 GROUP BY date_vigr_d, place_vigr, COALESCE(cargo_group, '')`,
		from, to).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]int, len(rows))
	for _, x := range rows {
		out[x.Day.String()[:10]+"|"+x.Term+"|"+x.Group] = x.N
	}
	return out, nil
}

// LoadingDaily — погрузка в адрес терминалов по ЖД-суткам (сырой SQL — канон
// для аналитики). Перегрузы исключены: непустой peregruz — это перегруз в
// вагон-донор, а не фактическая погрузка (TARGET.md §3.17).
func (r *HistoryRepository) LoadingDaily(ctx context.Context, from, to domain.LocalTime) ([]domain.LoadingDailyRow, error) {
	// Локальная структура с явными column-тегами: плоские структуры для
	// Raw().Scan, маппинг колонок не доверяем авто-стратегии (sms_1).
	var rows []struct {
		Day         domain.LocalTime `gorm:"column:day"`
		GruzpolS    string           `gorm:"column:gruzpol_s"`
		Sms1        string           `gorm:"column:sms_1"`
		StationNach string           `gorm:"column:station_nach"`
		Client      string           `gorm:"column:client"`
		CargoGroup  string           `gorm:"column:cargo_group"`
		VagonCount  int              `gorm:"column:vagon_count"`
		TotalWeight float64          `gorm:"column:total_weight"`
	}
	if err := r.db.WithContext(ctx).Raw(`
		SELECT date_nach_d AS day, gruzpol_s,
		       COALESCE(sms_1, '') AS sms_1,
		       COALESCE(station_nach, '') AS station_nach,
		       COALESCE(client, '') AS client,
		       COALESCE(cargo_group, '') AS cargo_group,
		       count(*) AS vagon_count,
		       COALESCE(SUM(ves), 0) AS total_weight
		  FROM dpport.vagon_history
		 WHERE date_nach_d BETWEEN ? AND ?
		   AND gruzpol_s <> ''
		   AND COALESCE(peregruz, '') = ''
		 GROUP BY date_nach_d, gruzpol_s, COALESCE(sms_1, ''),
		          COALESCE(station_nach, ''), COALESCE(client, ''),
		          COALESCE(cargo_group, '')
		 ORDER BY date_nach_d, gruzpol_s, sms_1`,
		from, to).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domain.LoadingDailyRow, 0, len(rows))
	for _, x := range rows {
		out = append(out, domain.LoadingDailyRow{
			Day: x.Day, GruzpolS: x.GruzpolS, Sms1: x.Sms1,
			StationNach: x.StationNach, Client: x.Client, CargoGroup: x.CargoGroup,
			VagonCount: x.VagonCount, TotalWeight: x.TotalWeight,
		})
	}
	return out, nil
}
