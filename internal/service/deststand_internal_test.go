package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/domain"
)

func dsLT(t time.Time) *domain.LocalTime {
	lt := domain.LocalTime(t)
	return &lt
}

func dsInt(v int) *int { return &v }

// TestDestStandList — порог, охват статусов и сортировка.
func TestDestStandList(t *testing.T) {
	now := time.Date(2026, time.August, 11, 9, 0, 0, 0, time.UTC)
	s2, s4, s9, s10, s12 := 2, 4, 9, 10, 12

	cache := overdueCache(
		// Стоит 20 суток, выгружен — самый долгий, должен быть первым.
		domain.Dislocation{Vagon: "5330", ID: "5330/1", Status: &s12,
			DatePrib: dsLT(now.Add(-498 * time.Hour)), Naznach: "АЭ"},
		// Гружёный, стоит 72 ч — в списке, состояние «гружён».
		domain.Dislocation{Vagon: "6816", ID: "6816/1", Status: &s10,
			DatePrib: dsLT(now.Add(-72 * time.Hour)), Naznach: "УТ-1"},
		// Ровно на пороге (48 ч) — НЕ в списке: порог строгий.
		domain.Dislocation{Vagon: "1111", Status: &s10, DatePrib: dsLT(now.Add(-48 * time.Hour))},
		// Чуть за порогом — в списке.
		domain.Dislocation{Vagon: "2222", ID: "2222/1", Status: &s12,
			DatePrib: dsLT(now.Add(-49 * time.Hour))},
		// Не дошёл до порога.
		domain.Dislocation{Vagon: "3333", Status: &s10, DatePrib: dsLT(now.Add(-10 * time.Hour))},
		// Статусы < 10 не в счёт, даже с древним прибытием (в пути, задержан,
		// кандидат): вагона на станции назначения ещё нет.
		domain.Dislocation{Vagon: "4444", Status: &s2, DatePrib: dsLT(now.Add(-300 * time.Hour))},
		domain.Dislocation{Vagon: "4445", Status: &s4, DatePrib: dsLT(now.Add(-300 * time.Hour))},
		domain.Dislocation{Vagon: "4446", Status: &s9, DatePrib: dsLT(now.Add(-300 * time.Hour))},
		// Статус ≥ 10, но прибытие не заполнено — считать не от чего.
		domain.Dislocation{Vagon: "5555", Status: &s10},
		// Без статуса и без номера — мусор снимка.
		domain.Dislocation{Vagon: "6666", DatePrib: dsLT(now.Add(-300 * time.Hour))},
	)

	svc := NewDestStandService(cache, nil)
	got := svc.list(now, 48)

	require.Len(t, got, 3)
	assert.Equal(t, []string{"5330", "6816", "2222"},
		[]string{got[0].Vagon, got[1].Vagon, got[2].Vagon}, "дольше стоящие первыми")

	assert.Equal(t, "выгружен", got[0].State)
	assert.Equal(t, 498, got[0].Hours)
	assert.Equal(t, 20, got[0].Days)

	assert.Equal(t, "гружён", got[1].State)
	assert.Equal(t, 72, got[1].Hours)
	assert.Equal(t, 3, got[1].Days)
}

// TestDestStandThresholdFromSettings — порог берётся из настроек, 0 → дефолт 48.
func TestDestStandThresholdFromSettings(t *testing.T) {
	assert.Equal(t, 48, domain.StatusPolicy{}.DestStandHoursOrDefault())
	assert.Equal(t, 24, domain.StatusPolicy{DestStandHours: 24}.DestStandHoursOrDefault())

	now := time.Date(2026, time.August, 11, 9, 0, 0, 0, time.UTC)
	s10 := 10
	cache := overdueCache(
		domain.Dislocation{Vagon: "7777", Status: &s10, DatePrib: dsLT(now.Add(-30 * time.Hour))},
	)
	svc := NewDestStandService(cache, nil)

	assert.Empty(t, svc.list(now, 48), "при пороге 48 ч тридцатичасовой вагон не долгостой")
	assert.Len(t, svc.list(now, 24), 1, "при пороге 24 ч — уже долгостой")
}

// TestDestStandUsesArrivalNotIdle — отсчёт от прибытия, а не от простоя РЖД:
// свежая подача обнуляет prost_*, но вагон всё равно долгостой (боевой случай
// 62194147: простой 2 суток при реальной стоянке 4,7).
func TestDestStandUsesArrivalNotIdle(t *testing.T) {
	now := time.Date(2026, time.August, 11, 9, 0, 0, 0, time.UTC)
	s12 := 12
	cache := overdueCache(domain.Dislocation{
		Vagon: "6219", ID: "6219/1", Status: &s12,
		DatePrib: dsLT(now.Add(-112 * time.Hour)),
		TimeOp:   dsLT(now.Add(-2 * time.Hour)), // только что подали
		OperS:    "Подача",
		ProstDn:  dsInt(0), ProstCh: dsInt(2),
	})

	got := NewDestStandService(cache, nil).list(now, 48)
	require.Len(t, got, 1)
	assert.Equal(t, 112, got[0].Hours)
}
