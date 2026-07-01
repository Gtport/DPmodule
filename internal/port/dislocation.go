package port

import (
	"context"

	"github.com/Gtport/DPmodule/internal/domain"
)

// DislocationRepository — персистентность снимка дислокации. Реализация —
// repository/gorm (атомарная замена снимка по «варианту B»). RAM-движок читает
// снимок на старте (LoadActual) и заливает свежий целиком (ReplaceActual).
type DislocationRepository interface {
	// LoadActual читает весь текущий снимок (прогрев RAM-движка).
	LoadActual(ctx context.Context) ([]domain.Dislocation, error)
	// ReplaceActual атомарно заменяет снимок новым набором записей.
	ReplaceActual(ctx context.Context, items []domain.Dislocation) error
}
