package gormrepo

import (
	"context"

	"gorm.io/gorm"

	"github.com/Gtport/DPmodule/internal/port"
)

// TileRepository — кэш тайлов OSM (таблица map_tiles, схема — docs/MAP_TILES.md).
// Счётчики hits/last_hit_at таблицы НЕ обновляем: репозиторий read-only, а
// запись на каждый отданный тайл — лишняя нагрузка ради статистики.
type TileRepository struct {
	db *gorm.DB
}

func NewTileRepository(db *gorm.DB) *TileRepository {
	return &TileRepository{db: db}
}

func (r *TileRepository) Tile(ctx context.Context, z, x, y int) ([]byte, error) {
	var data []byte
	err := r.db.WithContext(ctx).
		Raw(`SELECT data FROM map_tiles WHERE z = ? AND x = ? AND y = ?`, z, x, y).
		Scan(&data).Error
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, port.ErrTileNotFound
	}
	return data, nil
}
