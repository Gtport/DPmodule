package service

import (
	"context"
	"testing"

	"github.com/Gtport/DPmodule/internal/domain"
)

type unplStubRepo struct {
	rows    map[string]domain.Dislocation
	deleted []string
}

func newUnplStub() *unplStubRepo { return &unplStubRepo{rows: map[string]domain.Dislocation{}} }

func (r *unplStubRepo) Upsert(_ context.Context, items []domain.Dislocation) (int, error) {
	for _, it := range items {
		r.rows[it.Vagon] = it
	}
	return len(items), nil
}
func (r *unplStubRepo) DeleteByVagons(_ context.Context, vagons []string) (int, error) {
	n := 0
	for _, v := range vagons {
		if _, ok := r.rows[v]; ok {
			delete(r.rows, v)
			r.deleted = append(r.deleted, v)
			n++
		}
	}
	return n, nil
}
func (r *unplStubRepo) LoadAll(context.Context) ([]domain.Dislocation, error) {
	out := make([]domain.Dislocation, 0, len(r.rows))
	for _, it := range r.rows {
		out = append(out, it)
	}
	return out, nil
}

// «Бесплановые в подходе»: ловим смену станции без плана ближе порога на
// терминал плановой станции; план/прибытие снимают запись.
func TestTrackUnplannedMoves(t *testing.T) {
	ctx := context.Background()
	st2, st10 := 2, 10
	km := func(v int) *int { return &v }

	// Справочник: АЭ — терминал плановой станции (plan_code ma), БП — без плана.
	dir := NewDirectoryCache(&unplDirStub{
		ports: []domain.Ports{
			{Okpo: 1, NameS: "АЭ", PlanCode: "ma", StationCode: "985702", Enabled: true},
			{Okpo: 2, NameS: "БП", PlanCode: "", StationCode: "984700", Enabled: true},
		},
	})
	if err := dir.Load(ctx); err != nil {
		t.Fatal(err)
	}

	// Прежний снимок: вагоны стояли на станции 111111.
	prev := []domain.Dislocation{
		{Vagon: "A", CodeStationOper: "111111", Status: &st2},
		{Vagon: "B", CodeStationOper: "111111", Status: &st2},
		{Vagon: "C", CodeStationOper: "111111", Status: &st2},
		{Vagon: "D", CodeStationOper: "111111", Status: &st2},
	}
	actual := NewActualCache(s9StubDisl{items: prev})
	if err := actual.Load(ctx); err != nil {
		t.Fatal(err)
	}

	kept := []domain.Dislocation{
		// A: сменил станцию, без плана, 500 < 1000, АЭ (плановая) → сигнал.
		{Vagon: "A", CodeStationOper: "222222", Status: &st2, Naznach: "АЭ", RasstStanNazn: km(500)},
		// B: сменил станцию, но далеко (1500) → нет.
		{Vagon: "B", CodeStationOper: "222222", Status: &st2, Naznach: "АЭ", RasstStanNazn: km(1500)},
		// C: сменил станцию, близко, но терминал БЕЗ плана подвода → нет.
		{Vagon: "C", CodeStationOper: "222222", Status: &st2, Naznach: "БП", RasstStanNazn: km(300)},
		// D: станция та же → нет.
		{Vagon: "D", CodeStationOper: "111111", Status: &st2, Naznach: "АЭ", RasstStanNazn: km(400)},
	}
	repo := newUnplStub()
	added, _, err := trackUnplannedMoves(ctx, kept, actual, repo, dir, 0) // 0 → дефолт 1000
	if err != nil {
		t.Fatal(err)
	}
	if added != 1 {
		t.Fatalf("ожидался 1 сигнал, получено %d (rows=%v)", added, repo.rows)
	}
	if _, ok := repo.rows["A"]; !ok {
		t.Fatal("сигнал должен быть по вагону A")
	}

	// Автоснятие: A получил план; вагон E (в таблице) прибыл (статус 10).
	repo.rows["E"] = domain.Dislocation{Vagon: "E"}
	plan := domain.LocalTime{}
	_ = plan
	keptNext := []domain.Dislocation{
		{Vagon: "A", CodeStationOper: "333333", Status: &st2, Naznach: "АЭ",
			RasstStanNazn: km(400), PlanMsk: ltm(2026, 7, 21, 10, 0)},
		{Vagon: "E", CodeStationOper: "999999", Status: &st10, Naznach: "АЭ"},
	}
	_, cleared, err := trackUnplannedMoves(ctx, keptNext, actual, repo, dir, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if cleared != 2 {
		t.Fatalf("план и прибытие должны снять 2 записи, снято %d", cleared)
	}
	if len(repo.rows) != 0 {
		t.Fatalf("таблица должна опустеть, осталось %v", repo.rows)
	}
}


// unplDirStub — минимальный DirectoryRepository для теста (только ports).
type unplDirStub struct {
	rows  map[string]domain.Dislocation
	ports []domain.Ports
}

func (s *unplDirStub) LoadStations(context.Context) ([]domain.Station, error) { return nil, nil }
func (s *unplDirStub) LoadCargoOperations(context.Context) ([]domain.CargoOperation, error) {
	return nil, nil
}
func (s *unplDirStub) LoadCargo(context.Context) ([]domain.Cargo, error) { return nil, nil }
func (s *unplDirStub) LoadMarka(context.Context) ([]domain.Marka, error) { return nil, nil }
func (s *unplDirStub) LoadPorts(context.Context) ([]domain.Ports, error) { return s.ports, nil }
func (s *unplDirStub) LoadRouteSpeed(context.Context) ([]domain.RouteSpeed, error) {
	return nil, nil
}
func (s *unplDirStub) LoadNaznachStation(context.Context) ([]domain.NaznachStation, error) {
	return nil, nil
}
func (s *unplDirStub) UpdateNaznachStationNaznach(context.Context, string, string, string) error {
	return nil
}
