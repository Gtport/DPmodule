package parser

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Gtport/DPmodule/internal/domain"
)

// Контракт <client>/gu2b/update — по документу «Контракт отдачи ГУ-2б»
// (владелец → провайдер, 17.08.2026) в стиле его же ручки reference/update:
// конверт с курсором LAST_UPDATE и пачкой уведомлений, ключи UPPER_SNAKE,
// имена полей — как в нормализованных таблицах провайдера
// (kgdm.gu2b_notifications / gu2b_cars). Ручка у провайдера ЕЩЁ НЕ реализована
// (проверено по коду rwgate от 02.09.2026) — эталонная реализация обеих сторон
// наша, см. docs/GU2B.md; сменится живой формат — правится только этот файл.
//
// Времена — Московские naive «YYYY-MM-DD HH:MM:SS» (допускаются и с .sss).

// GU2BUpdate — результат разбора ответа gu2b/update одного клиента.
// LastUpdate — курсор провайдера как пришёл: дословно уйдёт в since следующего
// запроса (переформатирование могло бы съесть миллисекунды и зациклить пачку).
type GU2BUpdate struct {
	LastUpdate    string
	Notifications []domain.GU2BNotification
}

type gu2bEnvelope struct {
	LastUpdate    string          `json:"LAST_UPDATE"`
	Notifications []gu2bNotifRaw  `json:"NOTIFICATIONS"`
}

type gu2bNotifRaw struct {
	NotificationID  int64        `json:"NOTIFICATION_ID"`
	NotificationNum string       `json:"NOTIFICATION_NUM"`
	StateID         *int         `json:"STATE_ID"`
	State           string       `json:"STATE"`
	DateCreate      string       `json:"DATE_CREATE"`
	StationCode     string       `json:"STATION_CODE"`
	StationName     string       `json:"STATION_NAME"`
	OrgOKPO         string       `json:"ORG_OKPO"`
	OrgName         string       `json:"ORG_NAME"`
	PlaceTransfer   string       `json:"PLACE_TRANSFER"`
	Way             string       `json:"WAY"`
	Loc             string       `json:"LOC"`
	DocLastOper     string       `json:"DOC_LAST_OPER"`
	SignerName      string       `json:"SIGNER_NAME"`
	SignerPost      string       `json:"SIGNER_POST"`
	GatewaySource   string       `json:"GATEWAY_SOURCE"`
	UpdatedAt       string       `json:"UPDATED_AT"`
	Cars            []gu2bCarRaw `json:"CARS"`
}

type gu2bCarRaw struct {
	CarOrder       int    `json:"CAR_ORDER"`
	CarNumber      string `json:"CAR_NUMBER"`
	OperationID    *int   `json:"OPERATION_ID"`
	OperationName  string `json:"OPERATION_NAME"`
	OperationShort string `json:"OPERATION_SHORT"`
	FreightCode    string `json:"FREIGHT_CODE"`
	FreightName    string `json:"FREIGHT_NAME"`
	CarRemark      string `json:"CAR_REMARK"`
	InvoiceID      *int64 `json:"INVOICE_ID"`
	InvoiceNumber  string `json:"INVOICE_NUMBER"`
}

// ParseGU2BUpdate разбирает ответ <client>/gu2b/update. client — код клиента
// провайдера из пути запроса (в теле его нет). Ошибка любого уведомления
// прерывает весь разбор: курсор since нельзя двигать, пока пачка не разобрана
// целиком, — частичный результат опаснее отказа (правило то же, что у памяток).
func ParseGU2BUpdate(raw []byte, client string) (GU2BUpdate, error) {
	var env gu2bEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return GU2BUpdate{}, fmt.Errorf("gu2b/update (%s): парсинг JSON: %w", client, err)
	}
	out := GU2BUpdate{
		LastUpdate:    env.LastUpdate,
		Notifications: make([]domain.GU2BNotification, 0, len(env.Notifications)),
	}
	for i := range env.Notifications {
		n, err := convertGU2B(&env.Notifications[i], client)
		if err != nil {
			return GU2BUpdate{}, err
		}
		out.Notifications = append(out.Notifications, n)
	}
	return out, nil
}

func convertGU2B(src *gu2bNotifRaw, client string) (domain.GU2BNotification, error) {
	if src.NotificationID == 0 {
		return domain.GU2BNotification{}, fmt.Errorf("gu2b (%s): уведомление без NOTIFICATION_ID", client)
	}
	dateCreate, err := gu2bTime(src.DateCreate, "DATE_CREATE", client, src.NotificationID)
	if err != nil {
		return domain.GU2BNotification{}, err
	}
	docLastOper, err := gu2bTime(src.DocLastOper, "DOC_LAST_OPER", client, src.NotificationID)
	if err != nil {
		return domain.GU2BNotification{}, err
	}
	updatedAt, err := gu2bTime(src.UpdatedAt, "UPDATED_AT", client, src.NotificationID)
	if err != nil {
		return domain.GU2BNotification{}, err
	}

	n := domain.GU2BNotification{
		Client:         client,
		NotificationID: src.NotificationID,
		Number:         gu2bNumber(src.NotificationNum),
		StateID:        src.StateID,
		State:          src.State,
		DateCreate:     dateCreate,
		StationCode:    src.StationCode,
		StationName:    src.StationName,
		OrgOKPO:        src.OrgOKPO,
		OrgName:        src.OrgName,
		PlaceTransfer:  src.PlaceTransfer,
		Way:            src.Way,
		Loc:            src.Loc,
		DocLastOper:    docLastOper,
		SignerName:     src.SignerName,
		SignerPost:     src.SignerPost,
		GatewaySource:  src.GatewaySource,
		UpdatedAt:      updatedAt,
		Cars:           make([]domain.GU2BCar, 0, len(src.Cars)),
	}
	for _, c := range src.Cars {
		n.Cars = append(n.Cars, domain.GU2BCar{
			CarOrder:       c.CarOrder,
			Vagon:          strings.TrimSpace(c.CarNumber),
			OperationID:    c.OperationID,
			OperationName:  c.OperationName,
			OperationShort: c.OperationShort,
			FreightCode:    c.FreightCode,
			FreightName:    c.FreightName,
			CarRemark:      c.CarRemark,
			InvoiceID:      c.InvoiceID,
			InvoiceNumber:  c.InvoiceNumber,
		})
	}
	return n, nil
}

// gu2bNumber — сквозной номер уведомления. Приходит строкой; не число — nil
// (контроль полноты по нему просто не считается), это не повод терять документ.
func gu2bNumber(s string) *int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return nil
	}
	return &n
}

// gu2bTime — «YYYY-MM-DD HH:MM:SS[.sss]» → LocalTime; пусто → nil.
func gu2bTime(s, field, client string, id int64) (*domain.LocalTime, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	for _, layout := range []string{"2006-01-02 15:04:05.000", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return domain.NewLocalTime(t), nil
		}
	}
	return nil, fmt.Errorf("gu2b (%s): уведомление %d: %s %q не в формате YYYY-MM-DD HH:MM:SS[.sss]",
		client, id, field, s)
}
