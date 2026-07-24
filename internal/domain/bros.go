package domain

// BrosReasonCode — строка справочника официальных кодов бросания РЖД
// (классификатор причин отстановки поезда от движения). code — двузначный
// («01».."95"), description — расшифровка. Наполняется миграцией, правится
// в Админе. Используется журналом бросков (подсказки кодов) — следующие ветки.
type BrosReasonCode struct {
	Code        string `json:"code"`
	Description string `json:"description"`
}
