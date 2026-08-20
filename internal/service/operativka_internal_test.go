package service

import (
	"context"
	"testing"

	"github.com/Gtport/DPmodule/internal/domain"
)

func (r *histStubRepo) NotUnloadedCounts(_ context.Context, _ domain.LocalTime) (map[string]int, error) {
	return r.notUnloaded, nil
}

// «Не выгружено» оперативки считается ПО ИСТОРИИ (решение владельца
// 20.08.2026): карточка берёт счётчик из HistoryRepository.NotUnloadedCounts
// и раскладывает по терминалам реестра. Фильтры (гружёный, не «недоехавший»,
// окно прибытия) живут в SQL репозитория — их сторожит integration-тест
// TestHistoryRepository_NotUnloadedCounts.
func TestOperativka_NotUnloadedFromHistory(t *testing.T) {
	ctx := context.Background()

	dir := NewDirectoryCache(&unplDirStub{
		ports: []domain.Ports{
			{Okpo: 1, NameS: "АЭ", StationCode: "985702", Enabled: true},
			{Okpo: 2, NameS: "ГУТ-2", StationCode: "985702", Enabled: true},
		},
	})
	if err := dir.Load(ctx); err != nil {
		t.Fatal(err)
	}

	repo := newHistStub()
	repo.notUnloaded = map[string]int{"АЭ": 161, "ГУТ-2": 0, "ЧУЖОЙ": 5}

	svc := NewOperativkaService(repo, dir, nil)
	dto, err := svc.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(dto.Rows) != 2 {
		t.Fatalf("ожидались 2 строки терминалов, получено %d", len(dto.Rows))
	}
	byTerm := map[string]int{}
	for _, r := range dto.Rows {
		byTerm[r.Terminal] = r.NotUnloaded
	}
	if byTerm["АЭ"] != 161 || byTerm["ГУТ-2"] != 0 {
		t.Fatalf("«не выгружено» по терминалам = %v, ожидалось АЭ=161, ГУТ-2=0", byTerm)
	}
}
