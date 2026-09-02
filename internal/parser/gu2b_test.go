package parser

import (
	"testing"
	"time"
)

const gu2bSample = `{
  "LAST_UPDATE": "2026-09-01 12:34:56.789",
  "NOTIFICATIONS": [
    {
      "NOTIFICATION_ID": 60383536,
      "NOTIFICATION_NUM": "4870",
      "STATE_ID": 2,
      "STATE": "Подписан",
      "DATE_CREATE": "2026-08-24 09:15:00",
      "STATION_CODE": "98560",
      "STATION_NAME": "МЫС АСТАФЬЕВА",
      "ORG_OKPO": "1126022",
      "ORG_NAME": "АО \"НАХОДКИНСКИЙ МТП\"",
      "PLACE_TRANSFER": "фронт 2",
      "WAY": "путь 3",
      "LOC": "",
      "DOC_LAST_OPER": "2026-08-24 09:20:11",
      "SIGNER_NAME": "Иванов И.И.",
      "SIGNER_POST": "приемосдатчик",
      "GATEWAY_SOURCE": "asu",
      "UPDATED_AT": "2026-08-24 09:21:00.123",
      "CARS": [
        {
          "CAR_ORDER": 1,
          "CAR_NUMBER": "52962537",
          "OPERATION_ID": 1,
          "OPERATION_NAME": "Выгрузка",
          "OPERATION_SHORT": "ВЫГР",
          "FREIGHT_CODE": "42103",
          "FREIGHT_NAME": "вагоны на своих осях",
          "CAR_REMARK": "",
          "INVOICE_ID": 123456789,
          "INVOICE_NUMBER": "ЭА123456"
        },
        {
          "CAR_ORDER": 2,
          "CAR_NUMBER": "63499578",
          "OPERATION_NAME": "БОП",
          "OPERATION_SHORT": "БОП"
        }
      ]
    }
  ]
}`

func TestParseGU2BUpdate(t *testing.T) {
	upd, err := ParseGU2BUpdate([]byte(gu2bSample), "nmtp")
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if upd.LastUpdate != "2026-09-01 12:34:56.789" {
		t.Errorf("LAST_UPDATE = %q", upd.LastUpdate)
	}
	if len(upd.Notifications) != 1 {
		t.Fatalf("уведомлений %d", len(upd.Notifications))
	}
	n := upd.Notifications[0]
	if n.Client != "nmtp" || n.NotificationID != 60383536 {
		t.Errorf("шапка: %+v", n)
	}
	if n.Number == nil || *n.Number != 4870 {
		t.Errorf("Number = %v, ждали 4870", n.Number)
	}
	if !n.Signed() {
		t.Error("документ должен быть подписан")
	}
	if n.DateCreate == nil || !n.DateCreate.Time().Equal(time.Date(2026, 8, 24, 9, 15, 0, 0, time.UTC)) {
		t.Errorf("DateCreate = %v", n.DateCreate)
	}
	if n.EventTime() != n.DateCreate {
		t.Error("момент выгрузки — DATE_CREATE, а не подписание")
	}
	if len(n.Cars) != 2 {
		t.Fatalf("вагонов %d", len(n.Cars))
	}
	if !n.Cars[0].IsUnload() || n.Cars[0].Vagon != "52962537" {
		t.Errorf("первый вагон: %+v", n.Cars[0])
	}
	if n.Cars[1].IsUnload() {
		t.Error("БОП — не выгрузка")
	}
}

func TestParseGU2BUpdate_Отказы(t *testing.T) {
	// Без NOTIFICATION_ID пачка не разбирается целиком: курсор двигать нельзя.
	if _, err := ParseGU2BUpdate([]byte(`{"LAST_UPDATE":"x","NOTIFICATIONS":[{"STATE":"Подписан"}]}`), "attis"); err == nil {
		t.Error("уведомление без NOTIFICATION_ID должно ронять разбор")
	}
	if _, err := ParseGU2BUpdate([]byte(`{"NOTIFICATIONS":[{"NOTIFICATION_ID":1,"DATE_CREATE":"24.08.2026"}]}`), "attis"); err == nil {
		t.Error("кривой формат даты должен ронять разбор")
	}
	// Нечисловой номер — не ошибка: только контроль полноты не считается.
	upd, err := ParseGU2BUpdate([]byte(`{"NOTIFICATIONS":[{"NOTIFICATION_ID":1,"NOTIFICATION_NUM":"б/н"}]}`), "attis")
	if err != nil || upd.Notifications[0].Number != nil {
		t.Errorf("нечисловой номер: err=%v num=%v", err, upd.Notifications[0].Number)
	}
}
