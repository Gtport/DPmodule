package domain

import "errors"

// Sentinel errors — общие доменные ошибки. Хэндлеры маппят их в HTTP-коды
// (см. internal/handler). Каждая сущность переиспользует эти ошибки, не плодя свои.
var (
	ErrNotFound   = errors.New("not found")
	ErrBadRequest = errors.New("bad request")
	ErrForbidden  = errors.New("forbidden")
	ErrConflict   = errors.New("conflict")
)
