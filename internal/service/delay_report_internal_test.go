package service

// Тесты отчёта «Простои за период»: обрезка длительности границами периода,
// открытый эпизод до «сейчас», агрегат по станциям (сортировка по вагоно-часам),
// итоги и уникальные вагоны.

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

func delayReportEp(vagon, code, name string, kind int, from string, to string) domain.VagonDelayRow {
	f := trailTime(from)
	r := domain.VagonDelayRow{VagonDelay: domain.VagonDelay{
		Vagon: vagon, Kind: kind, StationCode: code, StationName: name, Doroga: "ДВ", DateFrom: &f,
	}}
	if to != "" {
		t := trailTime(to)
		r.DateTo = &t
		h := time.Time(t).Sub(time.Time(f)).Hours()
		r.Hours = &h
	}
	return r
}

func TestDelayReport(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))
	defer restore()

	repo := &fakeDelayRepo{period: []domain.VagonDelayRow{
		// Закрыт, начался ДО периода: 10.07 08:00 → 12.07 08:00; в периоде с 11.07 00:00 → 32 ч.
		delayReportEp("100", "930000", "ИРКУТСК", 4, "2026-07-10T08:00:00", "2026-07-12T08:00:00"),
		// Открыт (стоит сейчас): 18.07 00:00 → конец периода 21.07 00:00 → 72 ч (не до «сейчас» 22.07).
		delayReportEp("200", "984700", "НАХОДКА", 5, "2026-07-18T00:00:00", ""),
		// Тот же вагон 100, та же станция: 19.07 00:00 → 19.07 12:00 → 12 ч.
		delayReportEp("100", "930000", "ИРКУТСК", 4, "2026-07-19T00:00:00", "2026-07-19T12:00:00"),
	}}
	svc := NewDelayService(repo)

	rep, err := svc.Report(context.Background(),
		domain.LocalTime(time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC)),
		domain.LocalTime(time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC))) // включительно → конец 21.07 00:00
	require.NoError(t, err)

	assert.Equal(t, 3, rep.TotalEpisodes)
	assert.Equal(t, 2, rep.TotalVagons) // вагон 100 дважды
	assert.Equal(t, 1, rep.OpenNow)
	assert.Equal(t, 116.0, rep.TotalHours) // 32 + 72 + 12

	require.Len(t, rep.Stations, 2)
	// Тяжёлая станция сверху: НАХОДКА 72 (всё — брошен), ИРКУТСК 44 (всё — простой).
	assert.Equal(t, "НАХОДКА", rep.Stations[0].StationName)
	assert.Equal(t, 72.0, rep.Stations[0].Hours)
	assert.Equal(t, 72.0, rep.Stations[0].Hours5)
	assert.Equal(t, 1, rep.Stations[0].OpenNow)
	assert.Equal(t, "ИРКУТСК", rep.Stations[1].StationName)
	assert.Equal(t, 44.0, rep.Stations[1].Hours)
	assert.Equal(t, 44.0, rep.Stations[1].Hours4)
	assert.Equal(t, 2, rep.Stations[1].Episodes)
	assert.Equal(t, 1, rep.Stations[1].Vagons) // один вагон, два эпизода

	// Свежие эпизоды сверху; обрезка проставлена в строках.
	require.Len(t, rep.Records, 3)
	assert.Equal(t, 12.0, rep.Records[0].HoursInPeriod) // 19.07
	assert.Equal(t, 72.0, rep.Records[1].HoursInPeriod) // 18.07 (открытый)
	assert.Equal(t, 32.0, rep.Records[2].HoursInPeriod) // 10.07 (обрезан началом)
	// Полная длительность закрытого эпизода не тронута (48 ч).
	require.NotNil(t, rep.Records[2].Hours)
	assert.Equal(t, 48.0, *rep.Records[2].Hours)
}
