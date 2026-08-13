package gormrepo

import (
	"time"

	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/Gtport/DPmodule/internal/config"
)

// Open creates a GORM *DB with the project's connection-pool settings.
// Драйвер — pgx (через gorm.io/driver/postgres). SkipDefaultTransaction отключает
// неявную транзакцию вокруг каждого Create/Update — для нашей нагрузки (массовая
// заливка снимка дислокации) это убирает лишний оверхед; явные транзакции пишем сами.
//
// log — куда GORM пишет ошибки запросов и медленные запросы (см. zapGorm).
// nil допустим (cmd-утилиты, тесты): тогда БД молчит, как молчала при Silent.
func Open(cfg config.Postgres, log *zap.Logger, slowQuery time.Duration) (*gorm.DB, error) {
	if log == nil {
		log = zap.NewNop()
	}
	db, err := gorm.Open(postgres.Open(cfg.DSN), &gorm.Config{
		Logger:                 NewGormLogger(log, slowQuery),
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
