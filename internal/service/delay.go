package service

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
)

// DelayStats — диагностика reconcile задержек вагонов (Stage 2, рядом с bros).
type DelayStats struct {
	Opened    int // открытых эпизодов
	Escalated int // эскалаций 4 → 5 (простой стал броском на той же станции)
	Closed    int // закрытых эпизодов
	Active    int // открытых после пересбора
}

// applyDelays — reconcile эпизодов задержек (таблица vagon_delay): память
// «вагон стоял на станции с ... по ...». Свежий батч (после Stage 4) сверяется
// с открытыми эпизодами (date_to IS NULL) по номеру вагона:
//   - вагон вошёл в статус 4/5, открытого эпизода нет → INSERT (date_from =
//     time_op — фактическое начало стоянки, включая «допороговое» время);
//   - стоит на той же станции → эпизод продолжается: UPDATE только отличий
//     (group_key/индексы); статус 4 → 5 затирает kind (эскалация, решение
//     владельца), date_from сохраняется — стоянка та же; понижения 5 → 4 нет;
//   - станция сменилась (уехал и снова стоит) → закрыть старый + открыть новый;
//   - вагон ушёл из статуса 4/5 → закрыть эпизод time_op'ом увёзшей операции;
//   - вагон пропал из выгрузки → закрыть моментом пересбора.
//
// Идёт ПОСЛЕ Stage 4 рядом с applyBros; прежний снимок не нужен — состояние
// «до» несут сами открытые эпизоды. Только при ingest РЖД. Отличие от bros:
// там оперативный снимок ПОЕЗДОВ (журнал/подъём/отчёт), здесь — аналитическая
// память ВАГОНА; связь через group_key (id_status4/5). Вагоны без индекса не
// теряются: эпизод пишется и с пустым group_key.
func applyDelays(ctx context.Context, kept []domain.Dislocation, repo port.VagonDelayRepository) (DelayStats, error) {
	var st DelayStats

	open, err := repo.Open(ctx)
	if err != nil {
		return DelayStats{}, fmt.Errorf("открытые эпизоды: %w", err)
	}
	openByVagon := make(map[string]domain.VagonDelay, len(open))
	for _, d := range open {
		openByVagon[d.Vagon] = d
	}

	now := clock.Now()
	seen := make(map[string]bool, len(openByVagon)) // вагоны батча в статусе 4/5

	for i := range kept {
		r := &kept[i]
		if r.Vagon == "" || r.Status == nil {
			continue
		}
		s := *r.Status
		if (s != 4 && s != 5) || seen[r.Vagon] {
			continue
		}
		seen[r.Vagon] = true

		cur, ok := openByVagon[r.Vagon]
		if !ok { // вошёл в задержку — новый эпизод
			if err := repo.Insert(ctx, newDelayEpisode(r, s, now)); err != nil {
				return DelayStats{}, fmt.Errorf("insert delay %s: %w", r.Vagon, err)
			}
			st.Opened++
			st.Active++
			continue
		}
		if cur.StationCode == r.CodeStationOper { // та же станция — продолжается
			if fields := delayContinueFields(cur, r, s, now); len(fields) > 0 {
				if _, esc := fields["kind"]; esc {
					st.Escalated++
				}
				if err := repo.Update(ctx, cur.ID, fields); err != nil {
					return DelayStats{}, fmt.Errorf("update delay %s: %w", r.Vagon, err)
				}
			}
			st.Active++
			continue
		}
		// Уехал и снова стоит на другой станции: закрыть старый + открыть новый.
		if err := repo.Update(ctx, cur.ID, delayCloseFields(cur, r.TimeOp, now)); err != nil {
			return DelayStats{}, fmt.Errorf("close delay %s: %w", r.Vagon, err)
		}
		st.Closed++
		if err := repo.Insert(ctx, newDelayEpisode(r, s, now)); err != nil {
			return DelayStats{}, fmt.Errorf("insert delay %s: %w", r.Vagon, err)
		}
		st.Opened++
		st.Active++
	}

	// Закрытия: открытые эпизоды вагонов, ушедших из статуса 4/5. Вагон в батче —
	// закрываем time_op'ом увёзшей операции; пропал из выгрузки — моментом пересбора.
	var keptByVagon map[string]*domain.Dislocation
	for _, cur := range open {
		if seen[cur.Vagon] {
			continue
		}
		if keptByVagon == nil { // ленивое построение только когда есть закрытия
			keptByVagon = brosIndexByVagon(kept)
		}
		var at *domain.LocalTime
		if r, ok := keptByVagon[cur.Vagon]; ok {
			at = r.TimeOp
		}
		if err := repo.Update(ctx, cur.ID, delayCloseFields(cur, at, now)); err != nil {
			return DelayStats{}, fmt.Errorf("close delay %s: %w", cur.Vagon, err)
		}
		st.Closed++
	}

	return st, nil
}

// newDelayEpisode — новый эпизод из записи снимка. date_from = time_op (начало
// стоянки); без времени операции — момент пересбора.
func newDelayEpisode(r *domain.Dislocation, status int, now domain.LocalTime) domain.VagonDelay {
	from := r.TimeOp
	if from == nil || time.Time(*from).IsZero() {
		from = &now
	}
	return domain.VagonDelay{
		Vagon: r.Vagon, DateNachD: dateOnly(r.DateNach), Kind: status,
		GroupKey: delayGroupKey(r, status),
		Index:    r.Index, IndexMain: r.IndexMain,
		StationCode: r.CodeStationOper, StationName: r.StationOper, Doroga: r.DorogaOper,
		DateFrom: from, CreatedAt: &now, UpdatedAt: &now,
	}
}

// delayContinueFields — изменения продолжающегося эпизода (только отличия):
// эскалация kind 4 → 5 (5 затирает 4, обратного понижения нет), свежие
// group_key/индексы. При sticky-удержании статуса ключи id_status4/5 в снимке
// пусты — пустым значением накопленный group_key не затираем.
func delayContinueFields(cur domain.VagonDelay, r *domain.Dislocation, status int, now domain.LocalTime) map[string]any {
	fields := map[string]any{}
	if cur.Kind == domain.DelayKindProstoi && status == domain.DelayKindBros {
		fields["kind"] = domain.DelayKindBros
	}
	if key := delayGroupKey(r, status); key != "" && key != cur.GroupKey {
		fields["group_key"] = key
	}
	if r.Index != "" && r.Index != cur.Index {
		fields["index_poezd"] = r.Index
	}
	if r.IndexMain != "" && r.IndexMain != cur.IndexMain {
		fields["index_main"] = r.IndexMain
	}
	if len(fields) > 0 {
		fields["updated_at"] = now
	}
	return fields
}

// delayCloseFields — поля закрытия эпизода. at — время увёзшей операции (time_op
// новой записи); nil/нулевое — вагон пропал из выгрузки, закрываем моментом now.
func delayCloseFields(cur domain.VagonDelay, at *domain.LocalTime, now domain.LocalTime) map[string]any {
	to := at
	if to == nil || time.Time(*to).IsZero() {
		to = &now
	}
	fields := map[string]any{
		"date_to":    to,
		"updated_at": now,
	}
	if cur.DateFrom != nil {
		if h := time.Time(*to).Sub(time.Time(*cur.DateFrom)).Hours(); h > 0 {
			fields["hours"] = math.Round(h*10) / 10
		} else {
			fields["hours"] = 0.0
		}
	}
	return fields
}

// delayGroupKey — ключ агрегации по виду задержки: id_status5 для броска,
// id_status4 для простоя (пуст у вагонов без индекса/станции/времени операции).
func delayGroupKey(r *domain.Dislocation, status int) string {
	if status == domain.DelayKindBros {
		return r.IdStatus5
	}
	return r.IdStatus4
}
