package service

import (
	"context"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
)

// Status9Stats — диагностика наполнения таблицы кандидатов (S2-1a).
type Status9Stats struct {
	Inserted int // новых кандидатов статуса 9 (первое появление)
	Removed  int // удалено (вагон вернулся в поток со статусом ≠ 9)
}

// applyStatus9Live — S2-1a: наполнение таблицы кандидатов из «живого» батча по
// статусу 9. Вызывается в Stage 2 ПОСЛЕ Stage 1 (у записей есть статус) и ДО
// подмены снимка (actual — прежний снимок, для сравнения «первого появления»).
//
//   - вагон со статусом 9, первое появление (в актуальной ∉ {9}) → в таблицу
//     (InsertNew не перезапишет операторские правки существующего кандидата);
//   - вагон со статусом ≠ 9, присутствующий в таблице → удаляем (вернулся в поток
//     или сменил статус) — покрывает и авто-удаление живого кандидата, и возврат
//     защищённого статус-8 (наполнение 8 — в S2-1b).
//
// Статус-9 записи ОСТАЮТСЯ в наборе kept (в снимке) — их из батча не убираем.
func applyStatus9Live(
	ctx context.Context,
	kept []domain.Dislocation,
	actual *ActualCache,
	repo port.Status9Repository,
) (Status9Stats, error) {
	existing, err := repo.Vagons(ctx)
	if err != nil {
		return Status9Stats{}, err
	}

	var toInsert []domain.Dislocation
	var toDelete []string
	for i := range kept {
		r := kept[i]
		if r.Vagon == "" {
			continue
		}
		if r.Status != nil && *r.Status == 9 {
			// Первое появление: в актуальной вагона нет или его статус не 9.
			prev, ok := actual.FindVagonInActual(r.Vagon)
			first := !ok || prev.Status == nil || *prev.Status != 9
			if first {
				toInsert = append(toInsert, r)
			}
		} else if _, inTable := existing[r.Vagon]; inTable {
			// Вернулся в поток со статусом ≠ 9 → снять кандидата.
			toDelete = append(toDelete, r.Vagon)
		}
	}

	var st Status9Stats
	if st.Inserted, err = repo.InsertNew(ctx, toInsert); err != nil {
		return Status9Stats{}, err
	}
	if st.Removed, err = repo.DeleteByVagons(ctx, toDelete); err != nil {
		return Status9Stats{}, err
	}
	return st, nil
}
