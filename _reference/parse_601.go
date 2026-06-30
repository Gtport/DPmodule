// server/internal/service/parse_601.go
package service

import (
	"encoding/json"
	"strings"
	"time"

	"gtport/server/internal/models"
)

// response601 — ответ запроса 601 «История продвижения вагона».
type response601 struct {
	Status string `json:"status"`
	Data   struct {
		Operations []operation601 `json:"operations"`
	} `json:"data"`
}

type operation601 struct {
	KopVmd     string `json:"KOP_VMD"`
	DateOp     string `json:"DATE_OP"`
	StanOp     string `json:"STAN_OP"`
	IndexPoezd string `json:"INDEX_POEZD"`
}

// Parse601 разбирает ответ 601 в список операций. trip_key здесь НЕ ставится —
// его проставит репозиторий (единый на рейс). Индекс «000…0» → nil.
func Parse601(raw []byte) ([]models.VagonOperation, error) {
	var resp response601
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	ops := make([]models.VagonOperation, 0, len(resp.Data.Operations))
	for _, o := range resp.Data.Operations {
		ops = append(ops, models.VagonOperation{
			DateOp:     parse601Date(o.DateOp),
			KopVmd:     strings.TrimSpace(o.KopVmd),
			StanOp:     strings.TrimSpace(o.StanOp),
			IndexPoezd: emptyIndexToNil(o.IndexPoezd),
		})
	}
	return ops, nil
}

// parse601Date — «2026-06-18 08:16:00» (а также ISO-варианты), без таймзоны.
func parse601Date(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	formats := []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05.000",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return &t
		}
	}
	return nil
}

// emptyIndexToNil: пустая строка или все нули («000…0») → nil («не в поезде»).
func emptyIndexToNil(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" || strings.Trim(s, "0") == "" {
		return nil
	}
	return &s
}
