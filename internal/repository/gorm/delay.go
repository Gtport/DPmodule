package gormrepo

import (
	"context"

	"gorm.io/gorm"

	"github.com/Gtport/DPmodule/internal/domain"
)

// VagonDelayRepository реализует port.VagonDelayRepository (таблица vagon_delay).
type VagonDelayRepository struct {
	db *gorm.DB
}

func NewVagonDelayRepository(db *gorm.DB) *VagonDelayRepository {
	return &VagonDelayRepository{db: db}
}

// vagonDelayModel — ORM-раскладка колонок vagon_delay. Всё время — LocalTime
// (Московское naive).
type vagonDelayModel struct {
	ID          int64             `gorm:"column:id;primaryKey"`
	Vagon       string            `gorm:"column:vagon"`
	DateNachD   *domain.LocalTime `gorm:"column:date_nach_d"`
	Kind        int               `gorm:"column:kind"`
	GroupKey    string            `gorm:"column:group_key"`
	Index       string            `gorm:"column:index_poezd"`
	IndexMain   string            `gorm:"column:index_main"`
	StationCode string            `gorm:"column:station_code"`
	StationName string            `gorm:"column:station_name"`
	Doroga      string            `gorm:"column:doroga"`
	GruzpolS    string            `gorm:"column:gruzpol_s"`
	DateFrom    *domain.LocalTime `gorm:"column:date_from"`
	DateTo      *domain.LocalTime `gorm:"column:date_to"`
	Hours       *float64          `gorm:"column:hours"`
	CreatedAt   *domain.LocalTime `gorm:"column:created_at"`
	UpdatedAt   *domain.LocalTime `gorm:"column:updated_at"`
}

func (vagonDelayModel) TableName() string { return "dpport.vagon_delay" }

func toVagonDelayModel(d domain.VagonDelay) vagonDelayModel {
	return vagonDelayModel{
		ID: d.ID, Vagon: d.Vagon, DateNachD: d.DateNachD, Kind: d.Kind,
		GroupKey: d.GroupKey, Index: d.Index, IndexMain: d.IndexMain,
		StationCode: d.StationCode, StationName: d.StationName, Doroga: d.Doroga,
		GruzpolS: d.GruzpolS,
		DateFrom: d.DateFrom, DateTo: d.DateTo, Hours: d.Hours,
		CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
	}
}

func toVagonDelayDomain(m vagonDelayModel) domain.VagonDelay {
	return domain.VagonDelay{
		ID: m.ID, Vagon: m.Vagon, DateNachD: m.DateNachD, Kind: m.Kind,
		GroupKey: m.GroupKey, Index: m.Index, IndexMain: m.IndexMain,
		StationCode: m.StationCode, StationName: m.StationName, Doroga: m.Doroga,
		GruzpolS: m.GruzpolS,
		DateFrom: m.DateFrom, DateTo: m.DateTo, Hours: m.Hours,
		CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

// Open — открытые эпизоды (date_to IS NULL), не больше одного на вагон
// (частичный уникальный индекс uq_vagon_delay_open).
func (r *VagonDelayRepository) Open(ctx context.Context) ([]domain.VagonDelay, error) {
	var ms []vagonDelayModel
	if err := r.db.WithContext(ctx).Where("date_to IS NULL").
		Order("date_from").Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]domain.VagonDelay, len(ms))
	for i, m := range ms {
		out[i] = toVagonDelayDomain(m)
	}
	return out, nil
}

// ByTrip — эпизоды задержек одного рейса (вагон + дата начала рейса).
func (r *VagonDelayRepository) ByTrip(ctx context.Context, vagon string, dateNachD domain.LocalTime) ([]domain.VagonDelay, error) {
	var ms []vagonDelayModel
	if err := r.db.WithContext(ctx).
		Where("vagon = ? AND date_nach_d = ?", vagon, dateNachD).
		Order("date_from").Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]domain.VagonDelay, len(ms))
	for i, m := range ms {
		out[i] = toVagonDelayDomain(m)
	}
	return out, nil
}

func (r *VagonDelayRepository) Insert(ctx context.Context, d domain.VagonDelay) error {
	m := toVagonDelayModel(d)
	return r.db.WithContext(ctx).Create(&m).Error
}

func (r *VagonDelayRepository) Update(ctx context.Context, id int64, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&vagonDelayModel{}).
		Where("id = ?", id).Updates(fields).Error
}

// vagonDelayRowModel — строка отчёта: эпизод + id строки рейса из vagon_history.
// Плоская (без embedded): Raw().Scan сопоставляет колонки только по прямым полям.
type vagonDelayRowModel struct {
	ID          int64             `gorm:"column:id"`
	Vagon       string            `gorm:"column:vagon"`
	DateNachD   *domain.LocalTime `gorm:"column:date_nach_d"`
	Kind        int               `gorm:"column:kind"`
	GroupKey    string            `gorm:"column:group_key"`
	Index       string            `gorm:"column:index_poezd"`
	IndexMain   string            `gorm:"column:index_main"`
	StationCode string            `gorm:"column:station_code"`
	StationName string            `gorm:"column:station_name"`
	Doroga      string            `gorm:"column:doroga"`
	GruzpolS    string            `gorm:"column:gruzpol_s"`
	DateFrom    *domain.LocalTime `gorm:"column:date_from"`
	DateTo      *domain.LocalTime `gorm:"column:date_to"`
	Hours       *float64          `gorm:"column:hours"`
	CreatedAt   *domain.LocalTime `gorm:"column:created_at"`
	UpdatedAt   *domain.LocalTime `gorm:"column:updated_at"`
	TripID      string            `gorm:"column:trip_id"`
}

func toVagonDelayRowDomain(m vagonDelayRowModel) domain.VagonDelayRow {
	return domain.VagonDelayRow{
		VagonDelay: domain.VagonDelay{
			ID: m.ID, Vagon: m.Vagon, DateNachD: m.DateNachD, Kind: m.Kind,
			GroupKey: m.GroupKey, Index: m.Index, IndexMain: m.IndexMain,
			StationCode: m.StationCode, StationName: m.StationName, Doroga: m.Doroga,
			GruzpolS: m.GruzpolS,
			DateFrom: m.DateFrom, DateTo: m.DateTo, Hours: m.Hours,
			CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
		},
		TripID: m.TripID,
	}
}

// ByPeriod — эпизоды, пересекающиеся с [from; to), с id строки рейса
// (vagon_history по вагону и дате начала рейса — та же пара, что даёт trip_key).
// terminal — фильтр по gruzpol_s (пусто — все). Сырой SQL: JOIN по канону.
func (r *VagonDelayRepository) ByPeriod(ctx context.Context, from, to domain.LocalTime, terminal string) ([]domain.VagonDelayRow, error) {
	q := `
		SELECT d.*, COALESCE(h.id, '') AS trip_id
		FROM dpport.vagon_delay d
		LEFT JOIN dpport.vagon_history h
		       ON h.vagon = d.vagon AND h.date_nach_d = d.date_nach_d
		WHERE d.date_from < ? AND (d.date_to IS NULL OR d.date_to >= ?)`
	args := []any{to, from}
	if terminal != "" {
		q += ` AND d.gruzpol_s = ?`
		args = append(args, terminal)
	}
	q += ` ORDER BY d.date_from`

	var ms []vagonDelayRowModel
	if err := r.db.WithContext(ctx).Raw(q, args...).Scan(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]domain.VagonDelayRow, len(ms))
	for i, m := range ms {
		out[i] = toVagonDelayRowDomain(m)
	}
	return out, nil
}

// Current — открытые эпизоды (задержаны сейчас) с id строки рейса.
func (r *VagonDelayRepository) Current(ctx context.Context) ([]domain.VagonDelayRow, error) {
	var ms []vagonDelayRowModel
	if err := r.db.WithContext(ctx).Raw(`
		SELECT d.*, COALESCE(h.id, '') AS trip_id
		FROM dpport.vagon_delay d
		LEFT JOIN dpport.vagon_history h
		       ON h.vagon = d.vagon AND h.date_nach_d = d.date_nach_d
		WHERE d.date_to IS NULL
		ORDER BY d.date_from`).Scan(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]domain.VagonDelayRow, len(ms))
	for i, m := range ms {
		out[i] = toVagonDelayRowDomain(m)
	}
	return out, nil
}

// PurgeClosedOlderThan удаляет закрытые эпизоды старше cutoff (по date_to);
// открытые (date_to IS NULL) не трогает.
func (r *VagonDelayRepository) PurgeClosedOlderThan(ctx context.Context, cutoff domain.LocalTime) (int, error) {
	res := r.db.WithContext(ctx).
		Where("date_to IS NOT NULL AND date_to < ?", cutoff).
		Delete(&vagonDelayModel{})
	if res.Error != nil {
		return 0, res.Error
	}
	return int(res.RowsAffected), nil
}
