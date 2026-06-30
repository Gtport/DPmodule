package gormrepo

import (
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/Gtport/DPmodule/internal/config"
)

// Open creates a GORM *DB with the project's connection-pool settings.
// Драйвер — pgx (через gorm.io/driver/postgres). SkipDefaultTransaction отключает
// неявную транзакцию вокруг каждого Create/Update — для нашей нагрузки (массовая
// заливка снимка дислокации) это убирает лишний оверхед; явные транзакции пишем сами.
func Open(cfg config.Postgres) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(cfg.DSN), &gorm.Config{
		Logger:                 gormlogger.Default.LogMode(gormlogger.Silent),
		SkipDefaultTransaction: true,
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetime))

	return db, nil
}
