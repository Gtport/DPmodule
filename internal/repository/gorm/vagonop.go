package gormrepo

// Репозиторий запроса 601: трейл vagon_operation (снапшот-семантика — без
// gorm.Model/хуков, сырой SQL по канону) и очередь vagon_op_request.

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/Gtport/DPmodule/internal/domain"
)

type VagonOperationRepository struct {
	db *gorm.DB
}

func NewVagonOperationRepository(db *gorm.DB) *VagonOperationRepository {
	return &VagonOperationRepository{db: db}
}

type vagonOperationModel struct {
	TripKey    int64            `gorm:"column:trip_key"`
	DateOp     domain.LocalTime `gorm:"column:date_op"`
	KopVmd     string           `gorm:"column:kop_vmd"`
	StanOp     string           `gorm:"column:stan_op"`
	IndexPoezd *string          `gorm:"column:index_poezd"` // NULL — вне поезда
}

func (vagonOperationModel) TableName() string { return "vagon_operation" }

func (r *VagonOperationRepository) ReplaceForTrip(ctx context.Context, tripKey int64, ops []domain.VagonOperation) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`DELETE FROM vagon_operation WHERE trip_key = ?`, tripKey).Error; err != nil {
			return fmt.Errorf("очистка трейла %d: %w", tripKey, err)
		}
		// Дедуп по времени в пределах ответа: PK (trip_key, date_op), провайдер
		// может отдать несколько операций с одинаковой секундой — оставляем последнюю.
		byTS := make(map[domain.LocalTime]vagonOperationModel, len(ops))
		order := make([]domain.LocalTime, 0, len(ops))
		for _, o := range ops {
			m := vagonOperationModel{TripKey: tripKey, DateOp: o.DateOp, KopVmd: o.KopVmd, StanOp: o.StanOp}
			if o.IndexPoezd != "" {
				idx := o.IndexPoezd
				m.IndexPoezd = &idx
			}
			if _, dup := byTS[o.DateOp]; !dup {
				order = append(order, o.DateOp)
			}
			byTS[o.DateOp] = m
		}
		rows := make([]vagonOperationModel, 0, len(order))
		for _, ts := range order {
			rows = append(rows, byTS[ts])
		}
		if len(rows) == 0 {
			return nil
		}
		if err := tx.CreateInBatches(rows, 1000).Error; err != nil {
			return fmt.Errorf("вставка трейла %d: %w", tripKey, err)
		}
		return nil
	})
}

func (r *VagonOperationRepository) OperationsByTrip(ctx context.Context, tripKey int64) ([]domain.VagonOperation, error) {
	var ms []vagonOperationModel
	if err := r.db.WithContext(ctx).
		Raw(`SELECT trip_key, date_op, kop_vmd, stan_op, index_poezd
		     FROM vagon_operation WHERE trip_key = ? ORDER BY date_op`, tripKey).
		Scan(&ms).Error; err != nil {
		return nil, fmt.Errorf("трейл рейса %d: %w", tripKey, err)
	}
	out := make([]domain.VagonOperation, len(ms))
	for i, m := range ms {
		out[i] = domain.VagonOperation{TripKey: m.TripKey, DateOp: m.DateOp, KopVmd: m.KopVmd, StanOp: m.StanOp}
		if m.IndexPoezd != nil {
			out[i].IndexPoezd = *m.IndexPoezd
		}
	}
	return out, nil
}

func (r *VagonOperationRepository) Enqueue(ctx context.Context, reqs []domain.VagonOpRequest) error {
	for _, q := range reqs {
		if err := r.db.WithContext(ctx).Exec(`
			INSERT INTO vagon_op_request
			       (trip_key, vagon, date_nach_d, client, reason, priority, attempts, last_error, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, 0, '', ?, ?)
			ON CONFLICT (trip_key) DO UPDATE SET
			       reason = EXCLUDED.reason,
			       client = EXCLUDED.client,
			       priority = GREATEST(vagon_op_request.priority, EXCLUDED.priority),
			       attempts = 0, last_error = '',
			       updated_at = EXCLUDED.updated_at`,
			q.TripKey, q.Vagon, q.DateNachD, q.Client, q.Reason, q.Priority, q.CreatedAt, q.UpdatedAt,
		).Error; err != nil {
			return fmt.Errorf("заявка 601 %s: %w", q.Vagon, err)
		}
	}
	return nil
}

func (r *VagonOperationRepository) NextBatch(ctx context.Context, limit int) ([]domain.VagonOpRequest, error) {
	var ms []struct {
		TripKey   int64            `gorm:"column:trip_key"`
		Vagon     string           `gorm:"column:vagon"`
		DateNachD domain.LocalTime `gorm:"column:date_nach_d"`
		Client    string           `gorm:"column:client"`
		Reason    string           `gorm:"column:reason"`
		Priority  int              `gorm:"column:priority"`
		Attempts  int              `gorm:"column:attempts"`
	}
	if err := r.db.WithContext(ctx).
		Raw(`SELECT trip_key, vagon, date_nach_d, client, reason, priority, attempts
		     FROM vagon_op_request ORDER BY priority DESC, created_at LIMIT ?`, limit).
		Scan(&ms).Error; err != nil {
		return nil, fmt.Errorf("очередь 601: %w", err)
	}
	out := make([]domain.VagonOpRequest, len(ms))
	for i, m := range ms {
		out[i] = domain.VagonOpRequest{
			TripKey: m.TripKey, Vagon: m.Vagon, DateNachD: m.DateNachD,
			Client: m.Client, Reason: m.Reason, Priority: m.Priority, Attempts: m.Attempts,
		}
	}
	return out, nil
}

func (r *VagonOperationRepository) Complete(ctx context.Context, tripKey int64) error {
	return r.db.WithContext(ctx).Exec(`DELETE FROM vagon_op_request WHERE trip_key = ?`, tripKey).Error
}

func (r *VagonOperationRepository) Fail(ctx context.Context, tripKey int64, msg string, maxAttempts int, now domain.LocalTime) error {
	if err := r.db.WithContext(ctx).Exec(`
		UPDATE vagon_op_request
		   SET attempts = attempts + 1, last_error = ?, updated_at = ?
		 WHERE trip_key = ?`, msg, now, tripKey).Error; err != nil {
		return err
	}
	// Исчерпал попытки — снимаем, чтобы очередь не крутила вечный отказ.
	return r.db.WithContext(ctx).
		Exec(`DELETE FROM vagon_op_request WHERE trip_key = ? AND attempts >= ?`, tripKey, maxAttempts).Error
}

func (r *VagonOperationRepository) QueueSize(ctx context.Context) (int, error) {
	var n int
	err := r.db.WithContext(ctx).Raw(`SELECT count(*) FROM vagon_op_request`).Scan(&n).Error
	return n, err
}
