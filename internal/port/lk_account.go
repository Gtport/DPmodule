package port

import (
	"context"

	"github.com/Gtport/DPmodule/internal/domain"
)

// LKAccountRepository — чтение аккаунтов ЛК РЖД (таблица lk_account). Данные
// мелкие и правятся владельцем в админ-редакторе, поэтому читаем из БД по
// запросу без кэша: правка логина должна работать сразу, без рестарта.
type LKAccountRepository interface {
	// Accounts — включённые аккаунты по порядку sort_order.
	Accounts(ctx context.Context) ([]domain.LKAccount, error)
}
