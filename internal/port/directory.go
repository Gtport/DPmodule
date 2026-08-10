package port

import (
	"context"

	"github.com/Gtport/DPmodule/internal/domain"
)

// DirectoryRepository — порт загрузки справочников обогащения из хранилища.
// Реализация на GORM — internal/repository/gorm. Кэш в RAM строит сервис
// (internal/service.DirectoryCache), он зависит от этого интерфейса, а не от GORM.
type DirectoryRepository interface {
	LoadStations(ctx context.Context) ([]domain.Station, error)
	LoadCargoOperations(ctx context.Context) ([]domain.CargoOperation, error)
	LoadCargo(ctx context.Context) ([]domain.Cargo, error)
	LoadPorozhCargo(ctx context.Context) ([]domain.PorozhCargo, error)
	LoadMarka(ctx context.Context) ([]domain.Marka, error)
	LoadPorts(ctx context.Context) ([]domain.Ports, error)
	LoadRouteSpeed(ctx context.Context) ([]domain.RouteSpeed, error)
	LoadNaznachStation(ctx context.Context) ([]domain.NaznachStation, error)
	// UpdateNaznachStationNaznach меняет дефолтное назначение пары станций
	// (операторская панель перестановок; пустой naznach = «по назначению»).
	UpdateNaznachStationNaznach(ctx context.Context, destStation, originStation, naznach string) error
	// UpsertMarka добавляет строку словаря marka либо обновляет существующую по
	// ключу (okpo, station_kod, cargo_group) — назначение атрибуции из модалки
	// «Без атрибуции» с галкой «добавить в справочник».
	UpsertMarka(ctx context.Context, m domain.Marka) error
}
