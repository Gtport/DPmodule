package handler

import (
	"testing"

	"github.com/Gtport/DPmodule/internal/domain"
)

// Вкладки станций плана: только плановые с непустым plan_code, порядок по code,
// пустое имя станции заменяется кодом. Бесплановый клиент (речной порт) получает
// пустой список — фронт по нему прячет пункт меню «План подвода».
func TestPlanStations(t *testing.T) {
	got := planStations([]domain.PlanProfile{
		{StationCode: "984700", StationName: "НАХОДКА", Mode: domain.PlanModePlanned, PlanCode: "nk"},
		{StationCode: "985702", StationName: "МЫС АСТАФЬЕВА", Mode: domain.PlanModePlanned, PlanCode: "MA "},
		{StationCode: "111111", StationName: "РЕЧНОЙ ПОРТ", Mode: domain.PlanModeCapacity, PlanCode: ""},
		{StationCode: "222222", StationName: "БЕЗ КОДА", Mode: domain.PlanModePlanned, PlanCode: ""},
	})
	if len(got) != 2 {
		t.Fatalf("ждали 2 станции, получили %d: %+v", len(got), got)
	}
	if got[0].Code != "ma" || got[0].Label != "МЫС АСТАФЬЕВА" {
		t.Errorf("первая вкладка: ждали ma/МЫС АСТАФЬЕВА, получили %+v", got[0])
	}
	if got[1].Code != "nk" || got[1].Label != "НАХОДКА" {
		t.Errorf("вторая вкладка: ждали nk/НАХОДКА, получили %+v", got[1])
	}

	if got := planStations(nil); len(got) != 0 {
		t.Errorf("без профилей ждали пустой список, получили %+v", got)
	}
}
