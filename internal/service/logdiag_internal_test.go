package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Gtport/DPmodule/internal/domain"
)

// Обе выборки живут на путях диагностики: одна — в строке лога о дырах
// справочников, вторая — в сообщении об отказе вставки истории. Выход за
// границу среза здесь превратил бы отказ базы в панику, то есть поломку
// поверх поломки, поэтому короткие входы проверяем отдельно.
func TestSampleCodes_BoundedAndSafeOnShortInput(t *testing.T) {
	assert.Empty(t, sampleCodes(nil))
	assert.Equal(t, []int{33004}, sampleCodes([]int{33004}))

	many := make([]int, 250)
	for i := range many {
		many[i] = 100000 + i
	}
	got := sampleCodes(many)
	assert.Len(t, got, 10)
	assert.Equal(t, 100000, got[0], "выборка идёт с начала — правят справочник с первого кода")
}

func TestSampleTripIDs_BoundedAndSafeOnShortInput(t *testing.T) {
	assert.Empty(t, sampleTripIDs(nil))

	rows := []domain.VagonHistory{
		{ID: "63499578/872504/12.07.2026"},
		{ID: "54117452/872504/13.07.2026"},
		{ID: "11111111/872504/14.07.2026"},
		{ID: "22222222/872504/15.07.2026"},
	}
	got := sampleTripIDs(rows)
	assert.Equal(t, 3, strings.Count(got, "/")/2, "в выборку идут три рейса, не вся пачка")
	assert.Contains(t, got, "63499578/872504/12.07.2026")
	assert.NotContains(t, got, "22222222")
}
