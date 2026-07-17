package planmatch

import (
	"time"

	"github.com/Gtport/DPmodule/internal/domain"
)

// ApplyStats — сводка записи плана в снимок.
type ApplyStats struct {
	Stamped int // вагонов проставлено плановое прибытие (IndexPp/PlanMsk/PlanJd)
	Cleared int // вагонов очищено от прежних план-полей (стейл предыдущего плана)
}

// Apply накладывает результат матча на снимок дислокации и возвращает НОВЫЙ срез
// (исходный не мутируется). Перенос эталона clearPlanData + applyPlanData:
//
//   - Очистка: у «наших» вагонов (Naznach ИЛИ GruzpolS в target) со Status<10
//     сбрасываются IndexPp/PlanMsk/PlanJd — прежний план не должен «прилипать».
//   - Простановка: вагонам победивших ниток (m.Vagons) ставится IndexPp нитки и
//     её плановое время (PlanMsk — с правилом ≥18, PlanJd — без сдвига).
//
// now — единый штамп времени (clock.Now()), проставляется в UpdatedAt изменённых
// записей. Функция чистая: без БД, без часов — время приходит параметром.
func Apply(records []domain.Dislocation, matches []NitkaMatch, target map[string]struct{}, now domain.LocalTime) ([]domain.Dislocation, ApplyStats) {
	// Вагон → что проставить (из победивших ниток).
	type stamp struct {
		indexPp string
		planMsk time.Time
		planJd  time.Time
	}
	stampByVagon := make(map[string]stamp)
	for _, m := range matches {
		if !m.Matched {
			continue
		}
		for _, v := range m.Vagons {
			stampByVagon[v] = stamp{
				indexPp: m.Nitka.IndexPp,
				planMsk: m.Nitka.PlanMsk,
				planJd:  m.Nitka.PlanJd,
			}
		}
	}

	var stats ApplyStats
	out := make([]domain.Dislocation, len(records))
	for i, r := range records {
		arrived := r.Status != nil && *r.Status >= 10 // прибыл (10) / выгружен (12)

		// Очистка стейла: только «наши» и не прибывшие/выгруженные (эталон
		// clearPlanData расширен с 10 на ≥10 — после снятия заморозки прибывший
		// переходит в 12, его план так же неприкосновенен).
		if !arrived && isTarget(r.Naznach, r.GruzpolS, target) {
			if r.IndexPp != "" || r.PlanMsk != nil || r.PlanJd != nil {
				r.IndexPp = ""
				r.PlanMsk = nil
				r.PlanJd = nil
				r.UpdatedAt = now
				stats.Cleared++
			}
		}

		// Простановка плана (вагоны выбраны движком с учётом Status<10 и target).
		if s, ok := stampByVagon[r.Vagon]; ok {
			r.IndexPp = s.indexPp
			r.PlanMsk = toLocal(s.planMsk)
			r.PlanJd = toLocal(s.planJd)
			r.UpdatedAt = now
			stats.Stamped++
		}

		out[i] = r
	}
	return out, stats
}

// toLocal превращает naive time.Time в *domain.LocalTime; нулевое время → nil.
func toLocal(t time.Time) *domain.LocalTime {
	if t.IsZero() {
		return nil
	}
	return domain.NewLocalTime(t)
}
