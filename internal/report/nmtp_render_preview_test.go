package report

// Временный тест-превью: собирает книгу на фикстуре и кладёт файл в путь из
// NMTP_PREVIEW_OUT — только для ручной сверки оформления. Без переменной — скип.

import (
	"os"
	"testing"
	"time"

	"github.com/Gtport/DPmodule/internal/domain"
)

func TestNmtpXLSXPreview(t *testing.T) {
	out := os.Getenv("NMTP_PREVIEW_OUT")
	if out == "" {
		t.Skip("превью: задайте NMTP_PREVIEW_OUT")
	}
	lt := func(s string) *domain.LocalTime {
		v, err := time.Parse("2006-01-02T15:04:05", s)
		if err != nil {
			t.Fatal(err)
		}
		return domain.NewLocalTime(v)
	}
	cols := []domain.NmtpColumnHead{
		{Group: "РУК", Station: "МЕЖДУРЕЧЕНСК", Mark: "ГЖ"},
		{Group: "РУК", Station: "БЕЛОВО", Mark: "ГЖ"},
		{Group: "РУК", Station: "ПОЛОСУХИНО", Mark: "ГЖ"},
		{Group: "ТМК", Station: "ХАСАН + БАРДИНО", Mark: "ГЖБС,ГЖ+Ж"},
		{Group: "ЭЛЬГА", Station: "УЛАК", Mark: "Ж"},
		{Group: "КЛЦ-МАРИС", Station: "ЧЕЛУТАЙ", Mark: "ДОМСШ"},
	}
	row := func(idx, st string, counts []int, total int) domain.NmtpTrainRow {
		return domain.NmtpTrainRow{
			Index: idx, StationOper: st, DateNach: lt("2026-07-24T00:00:00"),
			ControlVagon: "60594728", Prog: lt("2026-07-31T05:39:00"),
			Counts: counts, Total: total,
		}
	}
	r := domain.NmtpReport{
		Terminal: "УТ-1",
		Columns:  cols,
		HasOther: true,
		Sections: []domain.NmtpSection{
			{Label: "НАХОДКА / БАРХАТНАЯ", Near: true, Total: 140, Rows: []domain.NmtpTrainRow{
				row("9379-206-9847", "ЛОЗОВЫЙ", []int{70, 0, 0, 0, 0, 0, 0}, 70),
				row("9131-059-9847", "СМОЛЯНИНОВО", []int{0, 0, 0, 68, 0, 0, 2}, 70),
			}},
			{Label: "Дальневосточная ЖД", Total: 71, Rows: []domain.NmtpTrainRow{
				row("9131-521-9861", "ИЗВЕСТКОВАЯ", []int{0, 0, 0, 0, 71, 0, 0}, 71),
			}},
		},
		Abandoned: []domain.NmtpSection{
			{Label: "ДАЛЬНЕВОСТОЧНАЯ", Total: 68, Rows: []domain.NmtpTrainRow{
				func() domain.NmtpTrainRow {
					t := row("8520-088-8632", "ПОЛОСУХИНО", []int{0, 0, 68, 0, 0, 0, 0}, 68)
					t.DateBros = lt("2026-07-10T00:00:00")
					t.Prog = nil
					return t
				}(),
			}},
		},
		ColCounts:       []int{70, 0, 68, 68, 71, 0, 2},
		ColTons:         []float64{4.83, 0, 4.69, 4.72, 4.9, 0, 0.14},
		TotalVagons:     279,
		TotalTons:       19.28,
		TrainsActive:    3,
		TrainsAbandoned: 1,
		UnloadForecast:  20.0,
		Norm:            3500,
		ClientTons: []domain.NmtpClientTons{
			{Client: "РУК", Tons: 9.52}, {Client: "ТМК", Tons: 4.72}, {Client: "ЭЛЬГА", Tons: 4.9},
		},
	}
	data, name, err := NmtpXLSX(r)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("файл: %s, %d байт", name, len(data))
	if err := os.WriteFile(out, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
