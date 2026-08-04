package service

import (
	"context"
	"testing"

	"github.com/Gtport/DPmodule/internal/domain"
)

// «Не выгружено» оперативки: статус-10 по терминалам; порожний под погрузку
// (признак порожнего + вес 0/пуст) не считается — выгрузки у него не будет
// (решение владельца 04.08.2026).
func TestOperativka_NotUnloadedSkipsPorozhInbound(t *testing.T) {
	ctx := context.Background()
	st10 := 10

	dir := NewDirectoryCache(&unplDirStub{
		ports: []domain.Ports{
			{Okpo: 1, NameS: "АЭ", StationCode: "985702", Enabled: true},
		},
	})
	if err := dir.Load(ctx); err != nil {
		t.Fatal(err)
	}
	actual := NewActualCache(s9StubDisl{items: []domain.Dislocation{
		{Vagon: "1", Status: &st10, Naznach: "АЭ", Ves: fp(70)},           // гружёный прибыл → считается
		{Vagon: "2", Status: &st10, Naznach: "АЭ", PorozhPriznak: "1"},    // порожний под погрузку → нет
		{Vagon: "3", Status: &st10, Naznach: "АЭ", PorozhPriznak: "1", Ves: fp(70)}, // опустевший с весом → считается
	}})
	if err := actual.Load(ctx); err != nil {
		t.Fatal(err)
	}

	svc := NewOperativkaService(newHistStub(), actual, dir, nil)
	dto, err := svc.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(dto.Rows) != 1 {
		t.Fatalf("ожидалась 1 строка терминала, получено %d", len(dto.Rows))
	}
	if got := dto.Rows[0].NotUnloaded; got != 2 {
		t.Fatalf("«не выгружено» = %d, ожидалось 2 (порожний под погрузку не в счёте)", got)
	}
}
