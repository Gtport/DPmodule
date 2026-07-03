package service

import (
	"context"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
)

// Status9Stats — диагностика согласования таблицы кандидатов (S2-1).
type Status9Stats struct {
	Inserted int // новых живых кандидатов статуса 9 (первое появление)
	Removed  int // снято (вагон вернулся в поток / сменил статус)
	Missing8 int // пропавших записано/обновлено (статус 8)
}

// reconcileCandidates — Stage 2 (S2-1): согласование таблицы кандидатов в прибытие
// (status9) с новым батчем и актуальным снимком. Вызывается ПОСЛЕ Stage 1 (у записей
// есть статус) и ДО подмены снимка (actual — прежний снимок). §3.14.
//
// Живой батч:
//   - статус 9, первое появление (в актуальной ∉ {9}): вагона нет в таблице → insert;
//     лежал как 8 (пропадал) → снять 8 и записать 9 (вернулся живым кандидатом);
//   - статус ≠ 9, вагон в таблице → снять (вернулся в поток / сменил статус).
//
// Пропавшие (в актуальной, нет в батче):
//   - статус актуального = 6 (порожний в пути) → выбыл, ничего;
//   - иначе → копия актуальной + статус 8 (при conflict — перевод 9→8, правки целы).
//
// Статус-9 записи ОСТАЮТСЯ в наборе kept (в снимке). Статус-8 в снимок НЕ идёт.
func reconcileCandidates(
	ctx context.Context,
	kept []domain.Dislocation,
	actual *ActualCache,
	repo port.Status9Repository,
) (Status9Stats, error) {
	inTable, err := repo.VagonStatuses(ctx)
	if err != nil {
		return Status9Stats{}, err
	}

	seen := make(map[string]struct{}, len(kept))
	var toInsert9 []domain.Dislocation
	var toDelete []string

	for i := range kept {
		r := kept[i]
		if r.Vagon == "" {
			continue
		}
		seen[r.Vagon] = struct{}{}
		tblStatus, has := inTable[r.Vagon]

		if r.Status != nil && *r.Status == 9 {
			prev, ok := actual.FindVagonInActual(r.Vagon)
			first := !ok || prev.Status == nil || *prev.Status != 9
			switch {
			case has && tblStatus == 8:
				// вернулся живым 9 из «пропавших» → снять защищённый 8, записать 9
				toDelete = append(toDelete, r.Vagon)
				toInsert9 = append(toInsert9, r)
			case !has && first:
				// первое появление живого кандидата
				toInsert9 = append(toInsert9, r)
			}
			// иначе (в таблице уже 9, либо не первое появление) — оставляем как есть
		} else if has {
			// вернулся в поток / сменил статус → снять кандидата (8 или 9)
			toDelete = append(toDelete, r.Vagon)
		}
	}

	// Пропавшие: были в актуальной, нет в новом батче.
	now := clock.Now()
	var toMissing8 []domain.Dislocation
	for _, v := range actual.All() {
		if v.Vagon == "" {
			continue
		}
		if _, present := seen[v.Vagon]; present {
			continue
		}
		if v.Status != nil && *v.Status == 6 {
			continue // порожний в пути — выбыл
		}
		rec := v
		s8 := 8
		rec.Status = &s8
		rec.UpdatedAt = now
		toMissing8 = append(toMissing8, rec)
	}

	var st Status9Stats
	if st.Removed, err = repo.DeleteByVagons(ctx, toDelete); err != nil {
		return Status9Stats{}, err
	}
	if st.Inserted, err = repo.InsertNew(ctx, toInsert9); err != nil {
		return Status9Stats{}, err
	}
	if st.Missing8, err = repo.UpsertMissing(ctx, toMissing8); err != nil {
		return Status9Stats{}, err
	}
	return st, nil
}
