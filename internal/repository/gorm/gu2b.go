package gormrepo

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

// Модели без gorm.Model/хуков: штампы времени ставим сами из clock.Now()
// (канон — единственный источник «сейчас»).

type gu2bNotificationModel struct {
	NotificationID    int64             `gorm:"column:notification_id;primaryKey"`
	Client            string            `gorm:"column:client"`
	Number            *int64            `gorm:"column:number"`
	StateID           *int              `gorm:"column:state_id"`
	State             string            `gorm:"column:state"`
	DateCreate        *domain.LocalTime `gorm:"column:date_create"`
	StationCode       string            `gorm:"column:station_code"`
	StationName       string            `gorm:"column:station_name"`
	OrgOkpo           string            `gorm:"column:org_okpo"`
	OrgName           string            `gorm:"column:org_name"`
	PlaceTransfer     string            `gorm:"column:place_transfer"`
	Way               string            `gorm:"column:way"`
	Loc               string            `gorm:"column:loc"`
	DocLastOper       *domain.LocalTime `gorm:"column:doc_last_oper"`
	SignerName        string            `gorm:"column:signer_name"`
	SignerPost        string            `gorm:"column:signer_post"`
	GatewaySource     string            `gorm:"column:gateway_source"`
	ProviderUpdatedAt *domain.LocalTime `gorm:"column:provider_updated_at"`
	ReceivedAt        *domain.LocalTime `gorm:"column:received_at"`
}

func (gu2bNotificationModel) TableName() string { return "dpport.gu2b_notification" }

type gu2bCarModel struct {
	NotificationID int64  `gorm:"column:notification_id;primaryKey"`
	CarOrder       int    `gorm:"column:car_order;primaryKey"`
	Vagon          string `gorm:"column:vagon"`
	OperationID    *int   `gorm:"column:operation_id"`
	OperationName  string `gorm:"column:operation_name"`
	OperationShort string `gorm:"column:operation_short"`
	FreightCode    string `gorm:"column:freight_code"`
	FreightName    string `gorm:"column:freight_name"`
	CarRemark      string `gorm:"column:car_remark"`
	InvoiceID      *int64 `gorm:"column:invoice_id"`
	InvoiceNumber  string `gorm:"column:invoice_number"`
}

func (gu2bCarModel) TableName() string { return "dpport.gu2b_car" }

type gu2bCursorModel struct {
	Client    string            `gorm:"column:client;primaryKey"`
	Since     string            `gorm:"column:since"`
	UpdatedAt *domain.LocalTime `gorm:"column:updated_at"`
}

func (gu2bCursorModel) TableName() string { return "dpport.gu2b_cursor" }

// GU2BRepository реализует port.GU2BRepository.
type GU2BRepository struct{ db *gorm.DB }

func NewGU2BRepository(db *gorm.DB) *GU2BRepository { return &GU2BRepository{db: db} }

// GetCursor — курсор клиента; строки нет → пустая строка без ошибки.
func (r *GU2BRepository) GetCursor(ctx context.Context, client string) (string, error) {
	var m gu2bCursorModel
	err := r.db.WithContext(ctx).Where("client = ?", client).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return m.Since, nil
}

// SetCursor — upsert курсора клиента.
func (r *GU2BRepository) SetCursor(ctx context.Context, client, since string) error {
	now := clock.Now()
	m := gu2bCursorModel{Client: client, Since: since, UpdatedAt: &now}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "client"}},
		DoUpdates: clause.AssignmentColumns([]string{"since", "updated_at"}),
	}).Create(&m).Error
}

// Upsert — пачка уведомлений одной транзакцией: шапка по notification_id,
// вагоны — полной заменой состава (повторный приход того же DocId — последняя
// версия документа, состав мог измениться; DELETE+INSERT надёжнее выборочного
// UPDATE и идемпотентен).
func (r *GU2BRepository) Upsert(ctx context.Context, notifications []domain.GU2BNotification) error {
	if len(notifications) == 0 {
		return nil
	}
	now := clock.Now()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i := range notifications {
			n := &notifications[i]
			m := gu2bNotificationModel{
				NotificationID: n.NotificationID, Client: n.Client, Number: n.Number,
				StateID: n.StateID, State: n.State, DateCreate: n.DateCreate,
				StationCode: n.StationCode, StationName: n.StationName,
				OrgOkpo: n.OrgOKPO, OrgName: n.OrgName,
				PlaceTransfer: n.PlaceTransfer, Way: n.Way, Loc: n.Loc,
				DocLastOper: n.DocLastOper, SignerName: n.SignerName, SignerPost: n.SignerPost,
				GatewaySource: n.GatewaySource, ProviderUpdatedAt: n.UpdatedAt, ReceivedAt: &now,
			}
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "notification_id"}},
				DoUpdates: clause.AssignmentColumns([]string{
					"client", "number", "state_id", "state", "date_create",
					"station_code", "station_name", "org_okpo", "org_name",
					"place_transfer", "way", "loc", "doc_last_oper",
					"signer_name", "signer_post", "gateway_source",
					"provider_updated_at", "received_at",
				}),
			}).Create(&m).Error; err != nil {
				return fmt.Errorf("gu2b: шапка %d: %w", n.NotificationID, err)
			}

			if err := tx.Where("notification_id = ?", n.NotificationID).
				Delete(&gu2bCarModel{}).Error; err != nil {
				return fmt.Errorf("gu2b: очистка вагонов %d: %w", n.NotificationID, err)
			}
			cars := make([]gu2bCarModel, 0, len(n.Cars))
			for _, c := range n.Cars {
				cars = append(cars, gu2bCarModel{
					NotificationID: n.NotificationID, CarOrder: c.CarOrder, Vagon: c.Vagon,
					OperationID: c.OperationID, OperationName: c.OperationName,
					OperationShort: c.OperationShort, FreightCode: c.FreightCode,
					FreightName: c.FreightName, CarRemark: c.CarRemark,
					InvoiceID: c.InvoiceID, InvoiceNumber: c.InvoiceNumber,
				})
			}
			if len(cars) > 0 {
				if err := tx.CreateInBatches(cars, 500).Error; err != nil {
					return fmt.Errorf("gu2b: вагоны %d: %w", n.NotificationID, err)
				}
			}
		}
		return nil
	})
}

// MaxNumber — наибольший сохранённый сквозной номер клиента (0 — нет числовых).
func (r *GU2BRepository) MaxNumber(ctx context.Context, client string) (int64, error) {
	var max *int64
	err := r.db.WithContext(ctx).
		Raw(`SELECT MAX(number) FROM dpport.gu2b_notification WHERE client = ?`, client).
		Scan(&max).Error
	if err != nil || max == nil {
		return 0, err
	}
	return *max, nil
}

// MissingNumbers — дыры сквозной нумерации в сохранённом диапазоне клиента.
// generate_series по [MIN; MAX] дешёв: номера — тысячи, не миллионы.
func (r *GU2BRepository) MissingNumbers(ctx context.Context, client string, limit int) ([]int64, error) {
	var out []int64
	err := r.db.WithContext(ctx).Raw(`
		SELECT gs FROM generate_series(
			(SELECT MIN(number) FROM dpport.gu2b_notification WHERE client = ?),
			(SELECT MAX(number) FROM dpport.gu2b_notification WHERE client = ?)
		) gs
		WHERE NOT EXISTS (
			SELECT 1 FROM dpport.gu2b_notification n WHERE n.client = ? AND n.number = gs
		)
		ORDER BY gs
		LIMIT ?`, client, client, client, limit).Scan(&out).Error
	return out, err
}

// PriorUnloadEvents — принятые ранее события выгрузки вагонов не старше from:
// только подписанные документы и только выгрузочные строки — ровно те, что
// движок когда-то принял или примет (для дедупа 72 ч).
func (r *GU2BRepository) PriorUnloadEvents(ctx context.Context, vagons []string, from domain.LocalTime) ([]domain.GU2BPriorEvent, error) {
	if len(vagons) == 0 {
		return nil, nil
	}
	var rows []struct {
		Vagon          string
		NotificationID int64
		T              domain.LocalTime
	}
	err := r.db.WithContext(ctx).Raw(`
		SELECT c.vagon, c.notification_id, COALESCE(n.date_create, n.doc_last_oper) AS t
		FROM dpport.gu2b_car c
		JOIN dpport.gu2b_notification n ON n.notification_id = c.notification_id
		WHERE c.vagon IN ?
		  AND (c.operation_short ILIKE 'ВЫГР' OR c.operation_name ILIKE 'Выгрузка')
		  AND LOWER(TRIM(n.state)) = LOWER(?)
		  AND COALESCE(n.date_create, n.doc_last_oper) >= ?`,
		vagons, domain.GU2BStateSigned, from).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]domain.GU2BPriorEvent, len(rows))
	for i, w := range rows {
		out[i] = domain.GU2BPriorEvent{Vagon: w.Vagon, NotificationID: w.NotificationID, T: w.T}
	}
	return out, nil
}
