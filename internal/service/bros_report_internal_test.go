package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/Gtport/DPmodule/internal/domain"
)

func brosDayLT(y int, m time.Month, d int) *domain.LocalTime {
	lt := domain.LocalTime(time.Date(y, m, d, 0, 0, 0, 0, time.UTC))
	return &lt
}

func brosEntry(date *domain.LocalTime, reason string, agreed *bool) domain.BrosJournalEntry {
	return domain.BrosJournalEntry{Date: date, Reason: reason, IsAgreed: agreed}
}

// Логика «последней известной записи» (пример gtport): бросок 09.05→14.05,
// журнал 09.05=49(прочие), 11.05=05, 13.05=05. Дни 09..13 (5): 09,10 прочие;
// 11,12,13 код05.
func TestBrosCountDays_LastKnown(t *testing.T) {
	from := dayFloor(time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC))
	to := dayFloor(time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC))
	entries := []domain.BrosJournalEntry{
		brosEntry(brosDayLT(2026, 5, 9), "49", nil),
		brosEntry(brosDayLT(2026, 5, 11), "05", nil),
		brosEntry(brosDayLT(2026, 5, 13), "05", nil),
	}
	var row domain.BrosReportRow
	brosCountDays(&row, from, to, entries, "")

	assert.Equal(t, 5, row.DaysTotal)
	assert.Equal(t, 3, row.DaysCode05)
	assert.Equal(t, 2, row.DaysOther)
	assert.Equal(t, 0, row.DaysCode01Agreed)
}

// Код 01: согласованный и несогласованный простой разводятся по is_agreed.
func TestBrosCountDays_Code01Agreed(t *testing.T) {
	from := dayFloor(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	to := dayFloor(time.Date(2026, 6, 4, 0, 0, 0, 0, time.UTC)) // 3 суток
	agreed := true
	entries := []domain.BrosJournalEntry{brosEntry(brosDayLT(2026, 6, 1), "01", &agreed)}
	var row domain.BrosReportRow
	brosCountDays(&row, from, to, entries, "")

	assert.Equal(t, 3, row.DaysTotal)
	assert.Equal(t, 3, row.DaysCode01Agreed)
	assert.Equal(t, 0, row.DaysCode01NotAgreed)
	assert.Equal(t, 0, row.DaysOther)
}

func TestBrosCountDays_Code01NotAgreed(t *testing.T) {
	from := dayFloor(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	to := dayFloor(time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)) // 2 суток
	no := false
	entries := []domain.BrosJournalEntry{brosEntry(brosDayLT(2026, 6, 1), "01", &no)}
	var row domain.BrosReportRow
	brosCountDays(&row, from, to, entries, "")

	assert.Equal(t, 2, row.DaysTotal)
	assert.Equal(t, 2, row.DaysCode01NotAgreed)
}

// Нет журнала — код берётся из снимка bros (fallback), is_agreed неизвестен.
func TestBrosCountDays_FallbackReason(t *testing.T) {
	from := dayFloor(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	to := dayFloor(time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)) // 2 суток
	var row domain.BrosReportRow
	brosCountDays(&row, from, to, nil, "05")

	assert.Equal(t, 2, row.DaysTotal)
	assert.Equal(t, 2, row.DaysCode05)

	// Код 01 без журнала → is_agreed неизвестен → прочие (не согл./несогл.).
	var row2 domain.BrosReportRow
	brosCountDays(&row2, from, to, nil, "01")
	assert.Equal(t, 2, row2.DaysOther)
	assert.Equal(t, 0, row2.DaysCode01Agreed)
}
