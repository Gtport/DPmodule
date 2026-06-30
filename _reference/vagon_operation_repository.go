// server/internal/repository/vagon_operation_repository.go
package repository

import (
	"context"
	"fmt"
	"strings"

	"gtport/server/internal/models"

	"github.com/jmoiron/sqlx"
)

type VagonOperationRepository struct {
	db *sqlx.DB
}

func NewVagonOperationRepository(db *sqlx.DB) *VagonOperationRepository {
	return &VagonOperationRepository{db: db}
}

// ReplaceForTrip перезаписывает ВСЕ операции рейса в одной транзакции:
// DELETE по trip_key + батч-INSERT нового набора. Идемпотентно — повторный
// запрос истории (например, при смене статуса на 10) честно затирает прежние
// операции, без дублей и «хвостов». trip_key проставляется здесь, единый на рейс.
func (r *VagonOperationRepository) ReplaceForTrip(ctx context.Context, tripKey int64, ops []models.VagonOperation) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM vagon_operation WHERE trip_key = $1`, tripKey); err != nil {
		return fmt.Errorf("delete operations: %w", err)
	}

	const cols = 5
	const batch = 1000
	for start := 0; start < len(ops); start += batch {
		end := start + batch
		if end > len(ops) {
			end = len(ops)
		}
		chunk := ops[start:end]
		ph := make([]string, 0, len(chunk))
		args := make([]interface{}, 0, len(chunk)*cols)
		for i, op := range chunk {
			n := i * cols
			ph = append(ph, fmt.Sprintf("($%d,$%d,$%d,$%d,$%d)", n+1, n+2, n+3, n+4, n+5))
			args = append(args, tripKey, op.DateOp, op.KopVmd, op.StanOp, op.IndexPoezd)
		}
		q := `INSERT INTO vagon_operation (trip_key, date_op, kop_vmd, stan_op, index_poezd) VALUES ` +
			strings.Join(ph, ",")
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			return fmt.Errorf("insert operations: %w", err)
		}
	}

	return tx.Commit()
}
