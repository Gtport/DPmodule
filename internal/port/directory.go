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
	LoadMarka(ctx context.Context) ([]domain.Marka, error)
	LoadPorts(ctx context.Context) ([]domain.Ports, error)
	LoadRouteSpeed(ctx context.Context) ([]domain.RouteSpeed, error)
	LoadNaznachStation(ctx context.Context) ([]domain.NaznachStation, error)
}
