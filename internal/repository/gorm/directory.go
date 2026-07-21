package gormrepo

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/Gtport/DPmodule/internal/clock"
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

type cargoModel struct {
	Kod        int64  `gorm:"column:cargo_kod;primaryKey"`
	Name       string `gorm:"column:name"`
	CargoGroup string `gorm:"column:cargo_group"`
	CargoS     string `gorm:"column:cargo_s"`
	CargoSms   string `gorm:"column:cargo_sms"`
}

func (cargoModel) TableName() string { return "cargo" }

type markaModel struct {
	Okpo       int64  `gorm:"column:okpo;primaryKey"`
	StationKod int64  `gorm:"column:station_kod;primaryKey"`
	Station    string `gorm:"column:station"`
	CargoGroup string `gorm:"column:cargo_group;primaryKey"`
	Shipper    string `gorm:"column:shipper"`
	Client     string `gorm:"column:client"`
	Sms1       string `gorm:"column:sms_1"`
	Sms3       string `gorm:"column:sms_3"`
	Color      string `gorm:"column:color"`
	Sprav1     string `gorm:"column:sprav_1"`
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
	// Клиент провайдера АСУ для запросов 601 по вагонам этого грузополучателя.
	ProviderClient string `gorm:"column:provider_client"`
}

func (portsModel) TableName() string { return "ports" }

type routeSpeedModel struct {
	ID          int64   `gorm:"column:id;primaryKey"`
	StationNach string  `gorm:"column:station_nach"`
	IsBam       bool    `gorm:"column:is_bam"`
	FromKm      int     `gorm:"column:from_km"`
	Speed       float64 `gorm:"column:speed"`
}

func (routeSpeedModel) TableName() string { return "route_speed" }

type naznachStationModel struct {
	ID            int64  `gorm:"column:id;primaryKey"`
	DestStation   string `gorm:"column:dest_station"`
	OriginStation string `gorm:"column:origin_station"`
	Naznach       string `gorm:"column:naznach"`
	Univers       bool   `gorm:"column:univers"`
	Enabled       bool   `gorm:"column:enabled"`
}

func (naznachStationModel) TableName() string { return "naznach_station" }

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

func (r *DirectoryRepository) LoadCargo(ctx context.Context) ([]domain.Cargo, error) {
	var ms []cargoModel
	if err := r.db.WithContext(ctx).Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]domain.Cargo, len(ms))
	for i, m := range ms {
		out[i] = domain.Cargo{
			Kod: m.Kod, Name: m.Name,
			CargoGroup: m.CargoGroup, CargoS: m.CargoS, CargoSms: m.CargoSms,
		}
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
			Okpo: m.Okpo, StationKod: m.StationKod, Station: m.Station,
			CargoGroup: m.CargoGroup,
			Shipper:    m.Shipper, Client: m.Client, Sms1: m.Sms1, Sms3: m.Sms3,
			Color: m.Color, Sprav1: m.Sprav1,
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
			ProviderClient: m.ProviderClient,
		}
	}
	return out, nil
}

func (r *DirectoryRepository) LoadRouteSpeed(ctx context.Context) ([]domain.RouteSpeed, error) {
	var ms []routeSpeedModel
	if err := r.db.WithContext(ctx).Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]domain.RouteSpeed, len(ms))
	for i, m := range ms {
		out[i] = domain.RouteSpeed{
			StationNach: m.StationNach, IsBam: m.IsBam, FromKm: m.FromKm, Speed: m.Speed,
		}
	}
	return out, nil
}

// UpdateNaznachStationNaznach — смена дефолтного назначения пары станций
// (операторская панель перестановок). Пустой naznach допустим («по назначению»).
func (r *DirectoryRepository) UpdateNaznachStationNaznach(ctx context.Context, destStation, originStation, naznach string) error {
	res := r.db.WithContext(ctx).Model(&naznachStationModel{}).
		Where("dest_station = ? AND origin_station = ?", destStation, originStation).
		Updates(map[string]any{"naznach": naznach, "updated_at": clock.Now().Time()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("пара станций (%s, %s) не найдена в naznach_station", destStation, originStation)
	}
	return nil
}

func (r *DirectoryRepository) LoadNaznachStation(ctx context.Context) ([]domain.NaznachStation, error) {
	var ms []naznachStationModel
	if err := r.db.WithContext(ctx).Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]domain.NaznachStation, len(ms))
	for i, m := range ms {
		out[i] = domain.NaznachStation{
			DestStation: m.DestStation, OriginStation: m.OriginStation,
			Naznach: m.Naznach, Univers: m.Univers, Enabled: m.Enabled,
		}
	}
	return out, nil
}
