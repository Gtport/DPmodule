package domain

import "strings"

// ГУ-2б — уведомление о завершении грузовой операции: документ провайдера,
// которым порт подтверждает ФАКТ выгрузки (реже — другую операцию: БОП и т.п.).
// Решение владельца 17.08.2026: ГУ-2б становится источником факта выгрузки
// вместо косвенных признаков (порожний/выбытие из снимка); анализ корпуса
// провайдера показал 100% совпадение терминала с ручным учётом gtport, включая
// все перестановки АЭ↔ГУТ-2 (см. docs/GU2B.md).
//
// Уведомления приходят инкрементом от провайдера (крон service.GU2BService),
// копятся в dpport.gu2b_notification/gu2b_car (контроль полноты по сквозной
// нумерации) и — при включённой перезаписи — уточняют вехи выгрузки
// date_vigr/date_vigr_d/place_vigr в vagon_history движком ApplyGU2B.

// GU2BNotification — шапка уведомления ГУ-2б.
type GU2BNotification struct {
	Client         string     // клиент провайдера из пути запроса (attis/nmtp)
	NotificationID int64      // ключ документа у провайдера (уникален, не циклится)
	Number         *int64     // сквозной номер уведомления клиента (контроль полноты); nil — не число
	StateID        *int       // код состояния документа
	State          string     // «Подписан» | «Заготовка» | «Испорчен»
	DateCreate     *LocalTime // создание уведомления = момент завершения грузовой операции (МСК)
	StationCode    string     // код станции ИЗ ДОКУМЕНТА (5-значный, с настроечным в лоб не совпадает)
	StationName    string     // имя станции — терминал матчится по нему, не по коду
	OrgOKPO        string     // ОКПО организации фактического терминала (консистентен на 100%)
	OrgName        string
	PlaceTransfer  string // место передачи — свободный текст, признаком не служит
	Way            string
	Loc            string
	DocLastOper    *LocalTime // последняя операция над документом (подписание)
	SignerName     string
	SignerPost     string
	GatewaySource  string // asu | lk — каким шлюзом уведомление добыто у провайдера
	UpdatedAt      *LocalTime
	Cars           []GU2BCar
}

// GU2BCar — строка вагона в уведомлении. Признак выгрузки живёт ЗДЕСЬ, а не в
// шапке; накладная строки — НЕ накладная прибытия (похоже, порожней отправки),
// поэтому к рейсу вагон вяжется замком по времени, не по накладной.
type GU2BCar struct {
	CarOrder       int
	Vagon          string
	OperationID    *int
	OperationName  string // «Выгрузка» | «БОП» | …
	OperationShort string // «ВЫГР» | …
	FreightCode    string
	FreightName    string
	CarRemark      string
	InvoiceID      *int64
	InvoiceNumber  string
}

// IsUnload — операция строки означает выгрузку. Словарь операций провайдера
// проверен на корпусе: ВЫГР 16044, БОП 36, «БЕЗ » 12 — но полный маппинг
// имя→кратко подтверждён лишь на одном значении, поэтому смотрим оба поля.
func (c GU2BCar) IsUnload() bool {
	if equalFoldTrim(c.OperationShort, "ВЫГР") {
		return true
	}
	return equalFoldTrim(c.OperationName, "Выгрузка")
}

// GU2BStateSigned — единственное состояние документа, которому верим:
// «Заготовка» и «Испорчен» вехи не пишут (198 таких строк в корпусе).
const GU2BStateSigned = "Подписан"

// Signed — документ подписан.
func (n *GU2BNotification) Signed() bool { return equalFoldTrim(n.State, GU2BStateSigned) }

// EventTime — момент завершения грузовой операции: создание уведомления
// (сверка корпуса: АСУ транслирует именно его в date_vigr, медиана расхождения
// 0 ч), фолбэк — подписание.
func (n *GU2BNotification) EventTime() *LocalTime {
	if n.DateCreate != nil {
		return n.DateCreate
	}
	return n.DocLastOper
}

func equalFoldTrim(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}
