package service

import (
	"context"
	"fmt"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

// ConfirmMissingRequest — ручная фиксация прибытия ПРОПАВШЕГО вагона (запись-8).
// Из-за дискретности выгрузок дислокации вагон мог прибыть, выгрузиться и выпасть
// из потока, не получив статусов 9/10/12 — диспетчер восстанавливает факты руками:
// время прибытия обязательно, выгрузка (время + место) — опционально в той же форме.
type ConfirmMissingRequest struct {
	VagonIDs  []string          `json:"vagon_ids"`            // id рейсов (записи-8 в status9)
	DatePrib  *domain.LocalTime `json:"date_prib"`            // факт прибытия, обязательно
	DateVigr  *domain.LocalTime `json:"date_vigr,omitempty"`  // факт выгрузки (пусто — не выгружен)
	PlaceVigr string            `json:"place_vigr,omitempty"` // терминал выгрузки (пусто — naznach записи)
	Index     string            `json:"index,omitempty"`      // правка индекса поезда (пусто — индекс записи-8)
	TimeBase  string            `json:"time_base,omitempty"`  // шкала введённых времён: "jd"|"msk" (пусто — как ЖД)
}

// ConfirmMissing — «Подтвердить прибытие» пропавшего: веха в vagon_history
// (статус 10, с выгрузкой — 12; поля — как у ConfirmArrival/«Выгрузить») и снятие
// записи-8. СНИМОК НЕ ТРОГАЕМ — вагона в нём нет (он выпал из дислокации), и
// синтетическую запись поток всё равно вычеркнул бы первым же батчем; вернётся в
// дислокацию — пойдёт обычным конвейером (реальный переход →10/→12 от АСУ ручную
// веху перепишет — с потоком не спорим). Порядок «история → удаление записи-8»
// осознанный: при сбое вехи вагон остаётся в пропавших и операцию можно повторить.
//
// Права: operator+ (общий гейт группы), правило «сегодня/вчера» не применяется —
// смысл функции в старых датах. Здравый смысл проверяется по данным:
// time_op последней операции ≤ date_prib ≤ date_vigr ≤ сейчас.
func (s *ArrivalsService) ConfirmMissing(ctx context.Context, req ConfirmMissingRequest) (ArrivalsUpdateResult, error) {
	if len(req.VagonIDs) == 0 {
		return ArrivalsUpdateResult{}, fmt.Errorf("не выбраны вагоны")
	}
	if req.DatePrib == nil || req.DatePrib.IsZero() {
		return ArrivalsUpdateResult{}, fmt.Errorf("не указано время прибытия")
	}
	if err := checkTimeBase(req.TimeBase); err != nil {
		return ArrivalsUpdateResult{}, err
	}
	// Две проекции введённых времён: хранение (прибытие — ЖД-штамп, выгрузка —
	// МСК) и МСК-эквиваленты для сравнений с time_op/«сейчас» — они в МСК.
	pribJd := arrivalToJd(req.DatePrib, req.TimeBase)
	pribMsk := req.DatePrib
	if req.TimeBase != domain.TimeBaseMSK {
		pribMsk = planMskFromJd(pribJd) // введено ЖД («час ≥ 18» — предыдущие сутки МСК)
	}
	vigrMsk := unloadToMsk(req.DateVigr, req.TimeBase)
	now := clock.Now()
	if pribMsk.Time().After(now.Time()) {
		return ArrivalsUpdateResult{}, fmt.Errorf("время прибытия в будущем")
	}
	if req.DateVigr != nil {
		if req.DateVigr.IsZero() {
			return ArrivalsUpdateResult{}, fmt.Errorf("не указано время выгрузки")
		}
		if vigrMsk.Time().Before(pribMsk.Time()) {
			return ArrivalsUpdateResult{}, fmt.Errorf("выгрузка раньше прибытия")
		}
		if vigrMsk.Time().After(now.Time()) {
			return ArrivalsUpdateResult{}, fmt.Errorf("время выгрузки в будущем")
		}
	} else if req.PlaceVigr != "" {
		return ArrivalsUpdateResult{}, fmt.Errorf("место выгрузки указано без времени выгрузки")
	}
	if req.PlaceVigr != "" && s.dir != nil {
		if _, ok := s.dir.PortByNameS(req.PlaceVigr); !ok {
			return ArrivalsUpdateResult{}, fmt.Errorf("неизвестный терминал %q", req.PlaceVigr)
		}
	}

	// Записи-8 по id рейса (resolveMissingByIDs: незнакомый id — честная ошибка).
	recs, err := s.resolveMissingByIDs(ctx, req.VagonIDs)
	if err != nil {
		return ArrivalsUpdateResult{}, err
	}
	for i := range recs {
		r := &recs[i]
		if r.TimeOp != nil && pribMsk.Time().Before(r.TimeOp.Time()) {
			return ArrivalsUpdateResult{}, fmt.Errorf(
				"прибытие раньше последней операции: вагон %s видели %s",
				r.Vagon, r.TimeOp.String())
		}
	}

	// Строка рейса в истории обычно уже есть (создавалась при первом появлении в
	// снимке); отсутствующим — полная строка из записи-8, затем веха поверх.
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
			// Веха прибытия пишется ниже своим UPDATE'ом (pribJd, уже ЖД) — здесь
			// нужна только строка рейса из записи-8 (статус 8, ветка 10 не сработает),
			// поэтому час отсечки не важен: 0 = дефолт.
			toInsert = append(toInsert, buildHistoryRow(&recs[i], now, 0))
		}
	}
	if err := s.repo.Insert(ctx, toInsert); err != nil {
		return ArrivalsUpdateResult{}, fmt.Errorf("строка рейса в истории: %w", err)
	}

	// Веха — те же поля, что ConfirmArrival (переход →10) и «Выгрузить» (→12).
	status := 10
	if req.DateVigr != nil {
		status = 12
	}
	updates := make(map[string]map[string]any, len(recs))
	for i := range recs {
		r := &recs[i]
		fields := map[string]any{
			"status": status,
			// Подтверждённое прибытие снимает возможную пометку «недоехавший»
			// (рейс мог быть удалён из пропавших ранее и вернуться).
			"not_arrived": false,
			"date_prib":   pribJd, // хранение — ЖД-штамп, как у автоматики (date_kon)
			"date_prib_d": dateOnly(pribJd),
			"delay":       calculateHistoryDelay(dateOnly(pribJd), r.DateDostav),
			"otkl":        calculateOtkl(pribJd, r.PlanMsk),
			"plan_msk":    r.PlanMsk,
			"plan_jd":     r.PlanJd,
			"naznach":     r.Naznach, // операторские перестановки пережили пропажу в записи-8
			"updated_at":  &now,
		}
		// Индекс поезда прибытия: правка оператора сильнее индекса записи-8.
		if req.Index != "" {
			fields["index_pp"] = req.Index
		} else if r.Index != "" {
			fields["index_pp"] = r.Index // фактический поезд последней дислокации
		}
		if req.DateVigr != nil {
			fields["date_vigr"] = vigrMsk // хранение — МСК-факт, как у автоматики (time_op)
			fields["date_vigr_d"] = dateOnly(jdFromFact(vigrMsk))
			place := req.PlaceVigr
			if place == "" {
				place = r.Naznach
			}
			fields["place_vigr"] = place
		}
		updates[r.ID] = fields
	}
	if err := s.repo.UpdateFieldsBatch(ctx, updates); err != nil {
		return ArrivalsUpdateResult{}, fmt.Errorf("веха прибытия в историю: %w", err)
	}

	// Снятие из пропавших (БД и RAM одним путём). Веха уже записана: при сбое
	// здесь повторное подтверждение перезапишет её и повторит удаление.
	vagons := make([]string, 0, len(recs))
	for i := range recs {
		if recs[i].Vagon != "" {
			vagons = append(vagons, recs[i].Vagon)
		}
	}
	if _, err := s.proc.status9.DeleteByVagons(ctx, vagons); err != nil {
		return ArrivalsUpdateResult{}, fmt.Errorf("снятие из пропавших: %w", err)
	}

	if s.proc.journal != nil {
		extra := map[string]any{
			"selected":      len(req.VagonIDs),
			"trains":        missingTrains(recs),
			"new_date_prib": pribJd.String(), // в журнале — значения хранения
		}
		if req.DateVigr != nil {
			extra["new_date_vigr"] = vigrMsk.String()
			if req.PlaceVigr != "" {
				extra["new_place_vigr"] = req.PlaceVigr
			}
		}
		if req.Index != "" {
			extra["new_index"] = req.Index
		}
		if req.TimeBase != "" {
			extra["time_base"] = req.TimeBase
		}
		s.proc.journal.RecordArrivalsEdit(ctx, "confirm_missing", len(updates), extra)
	}
	return ArrivalsUpdateResult{Updated: len(updates), Selected: len(req.VagonIDs)}, nil
}
