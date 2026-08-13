package gormrepo_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/config"
	gormrepo "github.com/Gtport/DPmodule/internal/repository/gorm"
)

// Integration-тест против реальной БД. Запускается только если задан
// DPMODULE_TEST_PG_DSN (иначе Skip). Требует применённой миграции
// 000044_bros_reason_codes. Только чтение — таблицу не мутирует.
func TestBrosReasonCodesRepository_ReasonCodes(t *testing.T) {
	dsn := os.Getenv("DPMODULE_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("DPMODULE_TEST_PG_DSN не задан — пропускаю integration-тест")
	}

	db, err := gormrepo.Open(config.Postgres{DSN: dsn, MaxOpenConns: 5, MaxIdleConns: 2}, nil, 0)
	require.NoError(t, err)

	repo := gormrepo.NewBrosReasonCodesRepository(db)
	codes, err := repo.ReasonCodes(context.Background())
	require.NoError(t, err)

	// Классификатор — 40 кодов из сида миграции.
	assert.Len(t, codes, 40)

	// Отсортированы по коду (первый — "01").
	require.NotEmpty(t, codes)
	assert.Equal(t, "01", codes[0].Code)

	// Спот-проверка расшифровки.
	byCode := map[string]string{}
	for _, c := range codes {
		byCode[c.Code] = c.Description
	}
	assert.Contains(t, byCode, "22")
	assert.NotEmpty(t, byCode["22"])
}
