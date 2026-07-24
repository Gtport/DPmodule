package port

import (
	"context"

	"github.com/Gtport/DPmodule/internal/domain"
)

// BrosReasonCodesRepository — чтение справочника кодов бросания (bros_reason_codes).
type BrosReasonCodesRepository interface {
	// ReasonCodes возвращает весь справочник, отсортированный по коду.
	ReasonCodes(ctx context.Context) ([]domain.BrosReasonCode, error)
}
