package parser

// Разбор ответа запроса 601 «История продвижения вагона» (провайдер АСУ,
// GET /wagons/{vagon}/history/{client}?from=&to=). Из ~35 полей операции берём
// четыре (перенос Parse601 из gtport): код операции, время, станция, индекс
// поезда. TripKey здесь НЕ ставится — его проставляет вызывающий (единый на
// рейс, см. domain.TripKeyOf).

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Gtport/DPmodule/internal/domain"
)

// response601 — конверт ответа: {status, message, data:{operations:[...]}}.
type response601 struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    struct {
		Operations []operation601 `json:"operations"`
	} `json:"data"`
}

type operation601 struct {
	KopVmd     string `json:"KOP_VMD"`
	DateOp     string `json:"DATE_OP"`
	StanOp     string `json:"STAN_OP"`
	IndexPoezd string `json:"INDEX_POEZD"`
}

// Parse601 разбирает ответ 601 в список операций продвижения. Операции без
// распознанного времени пропускаются (время — часть PK хранения). Индекс
// «000…0» → пустая строка («не в поезде»).
func Parse601(raw []byte) ([]domain.VagonOperation, error) {
	var resp response601
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("601: разбор JSON: %w", err)
	}
	if resp.Status != "success" {
		return nil, fmt.Errorf("601: статус %q: %s", resp.Status, resp.Message)
	}
	ops := make([]domain.VagonOperation, 0, len(resp.Data.Operations))
	for _, o := range resp.Data.Operations {
		ts, ok := parse601Time(o.DateOp)
		if !ok {
			continue
		}
		ops = append(ops, domain.VagonOperation{
			DateOp:     ts,
			KopVmd:     strings.TrimSpace(o.KopVmd),
			StanOp:     strings.TrimSpace(o.StanOp),
			IndexPoezd: emptyIndexToBlank(o.IndexPoezd),
		})
	}
	return ops, nil
}

// parse601Time — «2026-06-18 08:16:00» и ISO-варианты, МСК без таймзоны
// (инвариант LocalTime — время отдаём как пришло, не корректируем).
func parse601Time(s string) (domain.LocalTime, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return domain.LocalTime{}, false
	}
	for _, f := range []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05.000",
	} {
		if t, err := time.Parse(f, s); err == nil {
			return domain.LocalTime(t), true
		}
	}
	return domain.LocalTime{}, false
}

// emptyIndexToBlank: пустой или нулевой («000…0») индекс → "" («не в поезде»).
func emptyIndexToBlank(s string) string {
	s = strings.TrimSpace(s)
	if strings.Trim(s, "0") == "" {
		return ""
	}
	return s
}
