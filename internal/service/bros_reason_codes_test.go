package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/service"
)

// fakeReasonRepo — заглушка репозитория справочника кодов бросания.
type fakeReasonRepo struct {
	codes []domain.BrosReasonCode
	err   error
}

func (f fakeReasonRepo) ReasonCodes(context.Context) ([]domain.BrosReasonCode, error) {
	return f.codes, f.err
}

func TestBrosReasonCodes_Codes_passthrough(t *testing.T) {
	want := []domain.BrosReasonCode{
		{Code: "01", Description: "Неприем поезда"},
		{Code: "22", Description: "Ожидание локомотива"},
	}
	svc := service.NewBrosReasonCodes(fakeReasonRepo{codes: want})

	got, err := svc.Codes(context.Background())
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestBrosReasonCodes_Codes_error(t *testing.T) {
	svc := service.NewBrosReasonCodes(fakeReasonRepo{err: errors.New("boom")})

	_, err := svc.Codes(context.Background())
	assert.Error(t, err)
}
