package gormrepo

import (
	"context"

	"gorm.io/gorm"

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
	CreatedAt *domain.LocalTime `gorm:"column:created_at"`
	UpdatedAt *domain.LocalTime `gorm:"column:updated_at"`
}

func (vagonHistoryModel) TableName() string { return "vagon_history" }

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
		Color: h.Color, CreatedAt: h.CreatedAt, UpdatedAt: h.UpdatedAt,
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
		Color: m.Color, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
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

// DailyTerminalCounts — агрегаты «Оперативки» (сырой SQL — канон для аналитики):
// прибывшие по ЖД-суткам/терминалам (date_prib_d × naznach) и выгруженные
// (date_vigr_d × place_vigr) за диапазон ЖД-суток.
func (r *HistoryRepository) DailyTerminalCounts(ctx context.Context, from, to domain.LocalTime) (map[string]int, map[string]int, error) {
	type row struct {
		Day  domain.LocalTime `gorm:"column:day"`
		Term string           `gorm:"column:term"`
		N    int              `gorm:"column:n"`
	}
	key := func(d domain.LocalTime, term string) string { return d.String()[:10] + "|" + term }

	var pribRows []row
	if err := r.db.WithContext(ctx).Raw(`
		SELECT date_prib_d AS day, naznach AS term, count(*) AS n
		  FROM vagon_history
		 WHERE date_prib_d BETWEEN ? AND ? AND naznach <> ''
		 GROUP BY date_prib_d, naznach`, from, to).Scan(&pribRows).Error; err != nil {
		return nil, nil, err
	}
	var vigrRows []row
	if err := r.db.WithContext(ctx).Raw(`
		SELECT date_vigr_d AS day, place_vigr AS term, count(*) AS n
		  FROM vagon_history
		 WHERE date_vigr_d BETWEEN ? AND ? AND place_vigr <> ''
		 GROUP BY date_vigr_d, place_vigr`, from, to).Scan(&vigrRows).Error; err != nil {
		return nil, nil, err
	}

	prib := make(map[string]int, len(pribRows))
	for _, x := range pribRows {
		prib[key(x.Day, x.Term)] = x.N
	}
	vigr := make(map[string]int, len(vigrRows))
	for _, x := range vigrRows {
		vigr[key(x.Day, x.Term)] = x.N
	}
	return prib, vigr, nil
}
