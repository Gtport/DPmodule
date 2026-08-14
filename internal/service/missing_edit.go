package service

import (
	"context"
	"fmt"

	"github.com/Gtport/DPmodule/internal/auth"
	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

// Ручное управление списком пропавших (записи-8), помимо подтверждения прибытия
// (missing_confirm.go). Решение владельца 14.08.2026:
//   - «Скрыть» (operator+): запись НЕ удаляется, а помечается dismissed_at и
//     уходит из списков — снимется возвратом вагона в поток либо TTL-очисткой;
//   - «Удалить» (senior-operator/admin): запись-8 снимается насовсем, а рейс в
//     vagon_history получает пометку not_arrived («недоехавший») — чтобы не
//     висел «в пути», не попадал в «не выгруж.» и отчёт просрочки.
// Оба действия — в журнале событий (dismiss_missing / delete_missing).

// resolveMissingByIDs — записи-8 по id рейсов; незнакомый id — честная ошибка
// (вагон уже ушёл из пропавших: вернулся в поток, снят TTL или обработан
// параллельно — диспетчер должен увидеть, что картина устарела).
func (s *ArrivalsService) resolveMissingByIDs(ctx context.Context, vagonIDs []string) ([]domain.Dislocation, error) {
	if len(vagonIDs) == 0 {
		return nil, fmt.Errorf("не выбраны вагоны")
	}
	missing, err := s.proc.status9.MissingRows(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[string]*domain.Dislocation{}
	for i := range missing {
		if missing[i].Status != nil && *missing[i].Status == 8 {
			byID[missing[i].ID] = &missing[i]
		}
	}
	recs := make([]domain.Dislocation, 0, len(vagonIDs))
	seen := map[string]struct{}{}
	for _, id := range vagonIDs {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		r, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("вагон уже не в пропавших (id %s) — обновите список", id)
		}
		recs = append(recs, *r)
	}
	return recs, nil
}

// missingTrains — уникальные индексы поездов записей (для журнала).
func missingTrains(recs []domain.Dislocation) []string {
	var trains []string
	seen := map[string]struct{}{}
	for i := range recs {
		idx := recs[i].Index
		if idx == "" {
			idx = recs[i].IndexMain
		}
		if idx == "" {
			continue
		}
		if _, dup := seen[idx]; dup {
			continue
		}
		seen[idx] = struct{}{}
		trains = append(trains, idx)
	}
	return trains
}

// DismissMissing — «Скрыть» пропавших: пометка dismissed_at на записях-8
// (симметрия DismissCandidates для статуса 9). Права — operator+ (общий гейт
// мутаций группы). Запись остаётся в таблице: вернётся вагон — снимется
// reconcile'ом, не вернётся — уйдёт TTL-очисткой.
func (s *ArrivalsService) DismissMissing(ctx context.Context, vagonIDs []string) (ArrivalsUpdateResult, error) {
	recs, err := s.resolveMissingByIDs(ctx, vagonIDs)
	if err != nil {
		return ArrivalsUpdateResult{}, err
	}
	vagons := make([]string, 0, len(recs))
	for i := range recs {
		if recs[i].Vagon != "" {
			vagons = append(vagons, recs[i].Vagon)
		}
	}
	n, err := s.proc.status9.SetDismissedMissing(ctx, vagons, clock.Now())
	if err != nil {
		return ArrivalsUpdateResult{}, err
	}
	if s.proc.journal != nil {
		s.proc.journal.RecordArrivalsEdit(ctx, "dismiss_missing", n,
			map[string]any{"selected": len(vagonIDs), "trains": missingTrains(recs)})
	}
	return ArrivalsUpdateResult{Updated: n, Selected: len(vagonIDs)}, nil
}

// DeleteMissing — «Удалить» пропавших (senior-operator/admin): рейс в истории
// помечается not_arrived («недоехавший» — не «в пути», вне «не выгруж.» и
// отчёта просрочки), запись-8 удаляется. Порядок «пометка → удаление»
// осознанный: при сбое пометки вагон остаётся в пропавших и операцию можно
// повторить. Вернувшийся в дислокацию вагон снимает пометку реальным переходом
// статуса (historyUpdateFields) — жизнь вернее ручного решения.
func (s *ArrivalsService) DeleteMissing(ctx context.Context, vagonIDs []string) (ArrivalsUpdateResult, error) {
	if cl := auth.ClaimsFromContext(ctx); cl != nil && !cl.Allows(auth.AccessCrossShift) {
		return ArrivalsUpdateResult{}, fmt.Errorf(
			"%w: удалять пропавших может старший оператор или администратор", ErrArrivalsAccess)
	}
	recs, err := s.resolveMissingByIDs(ctx, vagonIDs)
	if err != nil {
		return ArrivalsUpdateResult{}, err
	}
	now := clock.Now()

	// Строка рейса в истории обычно уже есть; отсутствующим (вагон пропал до
	// первой записи) — полная строка из записи-8, затем пометка поверх.
	ids := make([]string, 0, len(recs))
	for i := range recs {
		ids = append(ids, recs[i].ID)
	}
	existing, err := s.repo.ExistingIDs(ctx, ids)
	if err != nil {
		return ArrivalsUpdateResult{}, err
	}
	var toInsert []domain.VagonHistory
	for i := range recs {
		if _, ok := existing[recs[i].ID]; !ok {
			toInsert = append(toInsert, buildHistoryRow(&recs[i], now, 0))
		}
	}
	if err := s.repo.Insert(ctx, toInsert); err != nil {
		return ArrivalsUpdateResult{}, fmt.Errorf("строка рейса в истории: %w", err)
	}
	updates := make(map[string]map[string]any, len(recs))
	for i := range recs {
		updates[recs[i].ID] = map[string]any{"not_arrived": true, "updated_at": &now}
	}
	if err := s.repo.UpdateFieldsBatch(ctx, updates); err != nil {
		return ArrivalsUpdateResult{}, fmt.Errorf("пометка «недоехавший» в истории: %w", err)
	}

	vagons := make([]string, 0, len(recs))
	for i := range recs {
		if recs[i].Vagon != "" {
			vagons = append(vagons, recs[i].Vagon)
		}
	}
	n, err := s.proc.status9.DeleteByVagons(ctx, vagons)
	if err != nil {
		return ArrivalsUpdateResult{}, fmt.Errorf("снятие из пропавших: %w", err)
	}
	if s.proc.journal != nil {
		s.proc.journal.RecordArrivalsEdit(ctx, "delete_missing", n,
			map[string]any{"selected": len(vagonIDs), "trains": missingTrains(recs), "vagons": vagons})
	}
	return ArrivalsUpdateResult{Updated: n, Selected: len(vagonIDs)}, nil
}
