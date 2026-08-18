package gormrepo

import (
	"context"

	"gorm.io/gorm"

	"github.com/Gtport/DPmodule/internal/domain"
)

// BrosReasonCodesRepository — чтение справочника кодов бросания.
type BrosReasonCodesRepository struct {
	db *gorm.DB
}

func NewBrosReasonCodesRepository(db *gorm.DB) *BrosReasonCodesRepository {
	return &BrosReasonCodesRepository{db: db}
}

// brosReasonCodeModel — ORM-раскладка справочника кодов бросания.
type brosReasonCodeModel struct {
	Code        string `gorm:"column:code;primaryKey"`
	Description string `gorm:"column:description"`
}

func (brosReasonCodeModel) TableName() string { return "dpport.bros_reason_codes" }

// ReasonCodes — весь справочник, отсортированный по коду.
func (r *BrosReasonCodesRepository) ReasonCodes(ctx context.Context) ([]domain.BrosReasonCode, error) {
	var rows []brosReasonCodeModel
	if err := r.db.WithContext(ctx).Order("code").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domain.BrosReasonCode, 0, len(rows))
	for _, m := range rows {
		out = append(out, domain.BrosReasonCode{Code: m.Code, Description: m.Description})
	}
	return out, nil
}

// ── Снимок брошенных (таблица bros) ─────────────────────────────────────────

// BrosRepository реализует port.BrosRepository.
type BrosRepository struct {
	db *gorm.DB
}

func NewBrosRepository(db *gorm.DB) *BrosRepository {
	return &BrosRepository{db: db}
}

// brosModel — ORM-раскладка колонок bros. Даты (date_*) и штампы (prog_*, *_at) —
// LocalTime (Московское naive).
type brosModel struct {
	ID          string            `gorm:"column:id;primaryKey"`
	IDIndex     string            `gorm:"column:id_index"`
	Index0      string            `gorm:"column:index_0"`
	Index1      string            `gorm:"column:index_1"`
	StationBr   string            `gorm:"column:station_br"`
	DorogaBr    string            `gorm:"column:doroga_br"`
	DateBr      *domain.LocalTime `gorm:"column:date_br"`
	GruzpolS    string            `gorm:"column:gruzpol_s"`
	DatePod     *domain.LocalTime `gorm:"column:date_pod"`
	DatePodFact *domain.LocalTime `gorm:"column:date_pod_fact"`
	Prog0       *domain.LocalTime `gorm:"column:prog_0"`
	Prog1       *domain.LocalTime `gorm:"column:prog_1"`
	ToGo        *float64          `gorm:"column:to_go"`
	Plan        *domain.LocalTime `gorm:"column:plan"`
	PlanHistory string            `gorm:"column:plan_history"`
	StatusBr    bool              `gorm:"column:status_br"`
	Reason      string            `gorm:"column:reason"`
	Comment     string            `gorm:"column:comment"`
	Sostav      string            `gorm:"column:sostav"`
	VagonCount  int               `gorm:"column:vagon_count"`
	CreatedAt   *domain.LocalTime `gorm:"column:created_at"`
	UpdatedAt   *domain.LocalTime `gorm:"column:updated_at"`
}

func (brosModel) TableName() string { return "dpport.bros" }

func toBrosModel(b domain.Bros) brosModel {
	return brosModel{
		ID: b.ID, IDIndex: b.IDIndex, Index0: b.Index0, Index1: b.Index1,
		StationBr: b.StationBr, DorogaBr: b.DorogaBr, DateBr: b.DateBr,
		GruzpolS: b.GruzpolS, DatePod: b.DatePod, DatePodFact: b.DatePodFact,
		Prog0: b.Prog0, Prog1: b.Prog1, ToGo: b.ToGo, Plan: b.Plan,
		PlanHistory: b.PlanHistory, StatusBr: b.StatusBr, Reason: b.Reason,
		Comment: b.Comment, Sostav: b.Sostav, VagonCount: b.VagonCount,
		CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt,
	}
}

func toBrosDomain(m brosModel) domain.Bros {
	return domain.Bros{
		ID: m.ID, IDIndex: m.IDIndex, Index0: m.Index0, Index1: m.Index1,
		StationBr: m.StationBr, DorogaBr: m.DorogaBr, DateBr: m.DateBr,
		GruzpolS: m.GruzpolS, DatePod: m.DatePod, DatePodFact: m.DatePodFact,
		Prog0: m.Prog0, Prog1: m.Prog1, ToGo: m.ToGo, Plan: m.Plan,
		PlanHistory: m.PlanHistory, StatusBr: m.StatusBr, Reason: m.Reason,
		Comment: m.Comment, Sostav: m.Sostav, VagonCount: m.VagonCount,
		CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

func brosDomainSlice(ms []brosModel) []domain.Bros {
	out := make([]domain.Bros, len(ms))
	for i, m := range ms {
		out[i] = toBrosDomain(m)
	}
	return out
}

func (r *BrosRepository) Active(ctx context.Context) ([]domain.Bros, error) {
	// Порядок справки задаётся здесь, фронт не пересортировывает:
	// терминалы по алфавиту (без терминала — в конец), внутри терминала
	// свежие броски сверху (решение владельца 04.08.2026).
	var ms []brosModel
	if err := r.db.WithContext(ctx).Where("status_br = true").
		Order("(gruzpol_s = '') ASC, gruzpol_s ASC, date_br DESC NULLS LAST, created_at DESC").
		Find(&ms).Error; err != nil {
		return nil, err
	}
	return brosDomainSlice(ms), nil
}

func (r *BrosRepository) Insert(ctx context.Context, b domain.Bros) error {
	m := toBrosModel(b)
	return r.db.WithContext(ctx).Create(&m).Error
}

func (r *BrosRepository) Update(ctx context.Context, id string, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&brosModel{}).
		Where("id = ?", id).Updates(fields).Error
}

func (r *BrosRepository) History(ctx context.Context, limit, offset int) ([]domain.Bros, int, error) {
	var total int64
	if err := r.db.WithContext(ctx).Model(&brosModel{}).
		Where("status_br = false").Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var ms []brosModel
	if err := r.db.WithContext(ctx).Where("status_br = false").
		Order("updated_at DESC").Limit(limit).Offset(offset).Find(&ms).Error; err != nil {
		return nil, 0, err
	}
	return brosDomainSlice(ms), int(total), nil
}

func (r *BrosRepository) Filter(ctx context.Context, f domain.BrosFilter) ([]domain.Bros, int, error) {
	where := func(q *gorm.DB) *gorm.DB {
		if len(f.GruzpolS) > 0 {
			q = q.Where("gruzpol_s IN ?", f.GruzpolS)
		}
		if f.Status != nil {
			q = q.Where("status_br = ?", *f.Status)
		}
		// Период: бросок пересекается с [start; end] — брошен не позже конца И
		// (ещё активен ИЛИ поднят не раньше начала). Зеркало gtport BrosFilter.
		if f.StartDate != nil && f.EndDate != nil {
			q = q.Where("date_br <= ? AND (date_pod_fact IS NULL OR date_pod_fact >= ?)", f.EndDate, f.StartDate)
		} else if f.StartDate != nil {
			q = q.Where("date_br >= ? OR date_pod_fact >= ?", f.StartDate, f.StartDate)
		} else if f.EndDate != nil {
			q = q.Where("date_br <= ?", f.EndDate)
		}
		return q
	}

	var total int64
	if err := where(r.db.WithContext(ctx).Model(&brosModel{})).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var ms []brosModel
	q := where(r.db.WithContext(ctx).Model(&brosModel{})).Order("date_br DESC")
	if f.Limit > 0 {
		q = q.Limit(f.Limit)
	}
	if f.Offset > 0 {
		q = q.Offset(f.Offset)
	}
	if err := q.Find(&ms).Error; err != nil {
		return nil, 0, err
	}
	return brosDomainSlice(ms), int(total), nil
}

func (r *BrosRepository) GetByID(ctx context.Context, id string) (*domain.Bros, error) {
	var m brosModel
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&m).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	b := toBrosDomain(m)
	return &b, nil
}

func (r *BrosRepository) SearchByIndex(ctx context.Context, q string) ([]domain.Bros, error) {
	pattern := "%" + q + "%"
	var ms []brosModel
	if err := r.db.WithContext(ctx).
		Where("index_0 ILIKE ? OR index_1 ILIKE ?", pattern, pattern).
		Order("date_br DESC").Limit(50).Find(&ms).Error; err != nil {
		return nil, err
	}
	return brosDomainSlice(ms), nil
}

// PurgeCompletedOlderThan удаляет завершённые броски старше cutoff (по updated_at).
func (r *BrosRepository) PurgeCompletedOlderThan(ctx context.Context, cutoff domain.LocalTime) (int, error) {
	res := r.db.WithContext(ctx).
		Where("status_br = false AND updated_at < ?", cutoff).
		Delete(&brosModel{})
	if res.Error != nil {
		return 0, res.Error
	}
	return int(res.RowsAffected), nil
}

// ── Журнал бросков (таблица bros_journal) ───────────────────────────────────

// BrosJournalRepository реализует port.BrosJournalRepository.
type BrosJournalRepository struct {
	db *gorm.DB
}

func NewBrosJournalRepository(db *gorm.DB) *BrosJournalRepository {
	return &BrosJournalRepository{db: db}
}

type brosJournalModel struct {
	ID           int64             `gorm:"column:id;primaryKey"`
	BrosID       string            `gorm:"column:bros_id"`
	Date         *domain.LocalTime `gorm:"column:date"`
	Reason       string            `gorm:"column:reason"`
	Comment      string            `gorm:"column:comment"`
	ZayavkaNomer *string           `gorm:"column:zayavka_nomer"`
	ZayavkaDate  *domain.LocalTime `gorm:"column:zayavka_date"`
	DatePod      *domain.LocalTime `gorm:"column:date_pod"`
	ReasonText   *string           `gorm:"column:reason_text"`
	IsAgreed     *bool             `gorm:"column:is_agreed"`
	PlanPod      *domain.LocalTime `gorm:"column:plan_pod"`
	CreatedAt    *domain.LocalTime `gorm:"column:created_at"`
	CreatedBy    string            `gorm:"column:created_by"`
}

func (brosJournalModel) TableName() string { return "dpport.bros_journal" }

func toBrosJournalDomain(m brosJournalModel) domain.BrosJournalEntry {
	return domain.BrosJournalEntry{
		ID: m.ID, BrosID: m.BrosID, Date: m.Date, Reason: m.Reason, Comment: m.Comment,
		ZayavkaNomer: m.ZayavkaNomer, ZayavkaDate: m.ZayavkaDate, DatePod: m.DatePod,
		ReasonText: m.ReasonText, IsAgreed: m.IsAgreed, PlanPod: m.PlanPod,
		CreatedAt: m.CreatedAt, CreatedBy: m.CreatedBy,
	}
}

// Upsert — INSERT записи за её сутки или перезапись существующей (bros_id, date).
// Сырой SQL с RETURNING id (канон для атомарного upsert).
func (r *BrosJournalRepository) Upsert(ctx context.Context, e domain.BrosJournalEntry) (int64, error) {
	var id int64
	err := r.db.WithContext(ctx).Raw(`
		INSERT INTO dpport.bros_journal (
			bros_id, date, reason, comment,
			zayavka_nomer, zayavka_date, date_pod, reason_text,
			is_agreed, plan_pod, created_at, created_by
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (bros_id, date) DO UPDATE SET
			reason        = EXCLUDED.reason,
			comment       = EXCLUDED.comment,
			zayavka_nomer = EXCLUDED.zayavka_nomer,
			zayavka_date  = EXCLUDED.zayavka_date,
			date_pod      = EXCLUDED.date_pod,
			reason_text   = EXCLUDED.reason_text,
			is_agreed     = EXCLUDED.is_agreed,
			plan_pod      = EXCLUDED.plan_pod,
			created_at    = EXCLUDED.created_at,
			created_by    = EXCLUDED.created_by
		RETURNING id`,
		e.BrosID, e.Date, e.Reason, e.Comment,
		e.ZayavkaNomer, e.ZayavkaDate, e.DatePod, e.ReasonText,
		e.IsAgreed, e.PlanPod, e.CreatedAt, e.CreatedBy,
	).Scan(&id).Error
	return id, err
}

func (r *BrosJournalRepository) ByBrosID(ctx context.Context, brosID string) ([]domain.BrosJournalEntry, error) {
	var ms []brosJournalModel
	if err := r.db.WithContext(ctx).Where("bros_id = ?", brosID).
		Order("date DESC, created_at DESC").Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]domain.BrosJournalEntry, len(ms))
	for i, m := range ms {
		out[i] = toBrosJournalDomain(m)
	}
	return out, nil
}

func (r *BrosJournalRepository) ByBrosIDs(ctx context.Context, brosIDs []string) ([]domain.BrosJournalEntry, error) {
	if len(brosIDs) == 0 {
		return nil, nil
	}
	var ms []brosJournalModel
	if err := r.db.WithContext(ctx).Where("bros_id IN ?", brosIDs).
		Order("bros_id, date").Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]domain.BrosJournalEntry, len(ms))
	for i, m := range ms {
		out[i] = toBrosJournalDomain(m)
	}
	return out, nil
}

func (r *BrosJournalRepository) Latest(ctx context.Context, brosID string) (*domain.BrosJournalEntry, error) {
	var m brosJournalModel
	err := r.db.WithContext(ctx).Where("bros_id = ?", brosID).
		Order("date DESC, created_at DESC").First(&m).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	e := toBrosJournalDomain(m)
	return &e, nil
}
