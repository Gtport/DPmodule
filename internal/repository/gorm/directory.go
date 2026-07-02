package gormrepo

import (
	"context"

	"gorm.io/gorm"

	"github.com/Gtport/DPmodule/internal/domain"
)

// ──────────────────────────────────────────────────────────────────────────
//  ORM-модели справочников (отделены от доменных структур domain.*).
//  Имена таблиц без схемы — search_path = dpport выставлен на уровне БД
//  (bootstrap_dpport.sql), как и для остального кода.
// ──────────────────────────────────────────────────────────────────────────

type stationModel struct {
	Kod       int      `gorm:"column:kod;primaryKey"`
	Kod4      int      `gorm:"column:kod_4"`
	Name      string   `gorm:"column:name"`
	Road      string   `gorm:"column:road"`
	Latitude  *float64 `gorm:"column:latitude"`
	Longitude *float64 `gorm:"column:longitude"`
	IsBam     bool     `gorm:"column:is_bam"`
}

func (stationModel) TableName() string { return "stations" }

type cargoOperationModel struct {
	Kod   int    `gorm:"column:kod;primaryKey"`
	Oper  string `gorm:"column:oper"`
	OperS string `gorm:"column:oper_s"`
}

func (cargoOperationModel) TableName() string { return "cargo_operations" }

type markaModel struct {
	ID         int64  `gorm:"column:id;primaryKey"`
	Okpo       int64  `gorm:"column:okpo"`
	StationKod int64  `gorm:"column:station_kod"`
	CargoKod   int64  `gorm:"column:cargo_kod"`
	Shipper    string `gorm:"column:shipper"`
	CargoS     string `gorm:"column:cargo_s"`
	Client     string `gorm:"column:client"`
	CargoGroup string `gorm:"column:cargo_group"`
	Sms1       string `gorm:"column:sms_1"`
}

func (markaModel) TableName() string { return "marka" }

type portsModel struct {
	ID           int64  `gorm:"column:id;primaryKey"`
	Okpo         int64  `gorm:"column:okpo"`
	Location     string `gorm:"column:location"`
	Organisation string `gorm:"column:organisation"`
	NameS        string `gorm:"column:name_s"`
	Name         string `gorm:"column:name"`
	Code         string `gorm:"column:code"`
	// Слой настроек/физики (000004).
	PlanCode    string `gorm:"column:plan_code"`
	StationCode string `gorm:"column:station_code"`
	PcCoal      *int   `gorm:"column:pc_coal"`
	PcMetal     *int   `gorm:"column:pc_metal"`
	PcOther     *int   `gorm:"column:pc_other"`
	PcTotal     *int   `gorm:"column:pc_total"`
	Front       *int   `gorm:"column:front"`
	Color       string `gorm:"column:color"`
	Enabled     bool   `gorm:"column:enabled"`
	SortOrder   int    `gorm:"column:sort_order"`
}

func (portsModel) TableName() string { return "ports" }

// ──────────────────────────────────────────────────────────────────────────
//  Адаптер: реализует port.DirectoryRepository, маппит ORM-модели в domain.*.
// ──────────────────────────────────────────────────────────────────────────

type DirectoryRepository struct {
	db *gorm.DB
}

func NewDirectoryRepository(db *gorm.DB) *DirectoryRepository {
	return &DirectoryRepository{db: db}
}

func (r *DirectoryRepository) LoadStations(ctx context.Context) ([]domain.Station, error) {
	var ms []stationModel
	if err := r.db.WithContext(ctx).Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]domain.Station, len(ms))
	for i, m := range ms {
		out[i] = domain.Station{
			Kod: m.Kod, Kod4: m.Kod4, Name: m.Name, Road: m.Road,
			Latitude: m.Latitude, Longitude: m.Longitude, IsBam: m.IsBam,
		}
	}
	return out, nil
}

func (r *DirectoryRepository) LoadCargoOperations(ctx context.Context) ([]domain.CargoOperation, error) {
	var ms []cargoOperationModel
	if err := r.db.WithContext(ctx).Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]domain.CargoOperation, len(ms))
	for i, m := range ms {
		out[i] = domain.CargoOperation{Kod: m.Kod, Oper: m.Oper, OperS: m.OperS}
	}
	return out, nil
}

func (r *DirectoryRepository) LoadMarka(ctx context.Context) ([]domain.Marka, error) {
	var ms []markaModel
	if err := r.db.WithContext(ctx).Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]domain.Marka, len(ms))
	for i, m := range ms {
		out[i] = domain.Marka{
			Okpo: m.Okpo, StationKod: m.StationKod, CargoKod: m.CargoKod,
			Shipper: m.Shipper, CargoS: m.CargoS, Client: m.Client,
			CargoGroup: m.CargoGroup, Sms1: m.Sms1,
		}
	}
	return out, nil
}

func (r *DirectoryRepository) LoadPorts(ctx context.Context) ([]domain.Ports, error) {
	var ms []portsModel
	if err := r.db.WithContext(ctx).Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]domain.Ports, len(ms))
	for i, m := range ms {
		out[i] = domain.Ports{
			Okpo: m.Okpo, Location: m.Location, Organisation: m.Organisation,
			NameS: m.NameS, Name: m.Name, Code: m.Code,
			PlanCode: m.PlanCode, StationCode: m.StationCode,
			PcCoal: m.PcCoal, PcMetal: m.PcMetal, PcOther: m.PcOther, PcTotal: m.PcTotal,
			Front: m.Front, Color: m.Color, Enabled: m.Enabled, SortOrder: m.SortOrder,
		}
	}
	return out, nil
}
