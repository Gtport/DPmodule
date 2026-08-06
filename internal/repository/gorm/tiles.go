package gormrepo

import (
	"context"
	"database/sql"
	"errors"

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
	// Именно Row().Scan, НЕ gorm.Scan(&data): GORM трактует *[]byte как «срез
	// строк из uint8» и валится на конвертации bytea (проверено 06.08.2026 —
	// из-за этого карта на VPS осталась без подложки при живом кэше).
	var data []byte
	row := r.db.WithContext(ctx).
		Raw(`SELECT data FROM map_tiles WHERE z = ? AND x = ? AND y = ?`, z, x, y).
		Row()
	if err := row.Scan(&data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, port.ErrTileNotFound
		}
		return nil, err
	}
	if len(data) == 0 {
		return nil, port.ErrTileNotFound
	}
	return data, nil
}
