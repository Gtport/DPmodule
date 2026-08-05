package port

import (
	"context"
	"errors"
)

// ErrTileNotFound — тайла нет в кэше. Обработчик отвечает 404: в OSM за
// промахом не ходим (решение владельца 06.08.2026, docs/MAP_TILES.md).
var ErrTileNotFound = errors.New("tile not found")

// TileRepository — чтение кэша тайлов OSM (вторая БД, блок tiles: конфига).
// Только чтение: кэш наполнен переносом со старой машины, приложение его
// не пополняет.
type TileRepository interface {
	// Tile отдаёт PNG тайла z/x/y либо ErrTileNotFound.
	Tile(ctx context.Context, z, x, y int) ([]byte, error)
}
