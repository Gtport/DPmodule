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
