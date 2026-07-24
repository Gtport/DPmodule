package service

import (
	"context"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
)

// BrosReasonCodes — справочник кодов бросания (только чтение). Расшифровки
// нужны журналу бросков (подсказки кодов) и отчёту — экраны следующих веток.
type BrosReasonCodes struct {
	repo port.BrosReasonCodesRepository
}

func NewBrosReasonCodes(repo port.BrosReasonCodesRepository) *BrosReasonCodes {
	return &BrosReasonCodes{repo: repo}
}

// Codes — весь справочник, отсортированный по коду.
func (s *BrosReasonCodes) Codes(ctx context.Context) ([]domain.BrosReasonCode, error) {
	return s.repo.ReasonCodes(ctx)
}
