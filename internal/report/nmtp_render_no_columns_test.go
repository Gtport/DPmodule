package report

// Книга «Подход груза» без раскладки nmtp_column (решение владельца
// 13.08.2026: отчёт есть у любого клиента, без колонок всё в «прочее») —
// рендер не должен падать на пустом r.Columns.

import (
	"testing"

	"github.com/Gtport/DPmodule/internal/domain"
)

func TestNmtpXLSX_NoColumns(t *testing.T) {
	r := domain.NmtpReport{
		Terminal: "АНБ",
		HasOther: true,
		Sections: []domain.NmtpSection{{
			Label: "ДАЛЬНЕВОСТОЧНАЯ ЖД",
			Rows: []domain.NmtpTrainRow{{
				Index: "9370-101-9857", StationOper: "АМУР",
				Counts: []int{5}, Tons: []float64{0.345}, Total: 5,
			}},
			Total: 5,
		}},
		ColCounts: []int{5}, ColTons: []float64{0.345},
		TotalVagons: 5, TotalTons: 0.345,
		ClientTons: []domain.NmtpClientTons{{Client: "ПРОЧЕЕ", Tons: 0.345}},
	}
	data, name, err := NmtpXLSX(r)
	if err != nil {
		t.Fatalf("рендер без колонок: %v", err)
	}
	if len(data) == 0 || name == "" {
		t.Fatalf("пустой результат: %d байт, имя %q", len(data), name)
	}
}
