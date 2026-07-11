package planmatch

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Gtport/DPmodule/internal/domain"
)

// SFRecord — запись справочника sf (таблица dpport.sf): вариант написания синонима
// станции формирования → каноническая станция + потолок вагонов.
type SFRecord struct {
	Sinonim  string
	Station  string
	Quantity int
}

// SFGroup — группа-кандидат вагонов для с.ф.: агрегация снимка дислокации по станции
// формирования. Показывается пользователю в диалоге; выбранные группы на confirm
// проставляются как вагоны нитки с.ф.
type SFGroup struct {
	Key         string            // уникальный ключ группы (StationOper|index|date|IdDisl) — id_disl не уникален!
	StationOper string            // станция текущей операции (= станция формирования синонима)
	Index       string            // IndexMain если непуст, иначе Index (как в эталоне)
	IndexMain   string            //
	DateOp      *domain.LocalTime // дата операции группы
	IdDisl      string            // id операции (может совпадать у разных групп — не идентификатор!)
	Quantity    int               // число вагонов в группе
	Vagons      []string          // номера вагонов (для простановки на confirm)
	SubGroups   []SubGroup        // подгруппы (IndexMain|Naznach|Sms1|GruzpolS) — для «Состава»
}

// SFStations возвращает станции формирования для синонима из справочника sf.
// Синоним — в ВЕРХНЕМ регистре (как sinonim в sf и как отдаёт парсер, extractSynonym).
func SFStations(synonym string, sf []SFRecord) map[string]struct{} {
	syn := strings.ToUpper(strings.TrimSpace(synonym))
	out := map[string]struct{}{}
	for _, r := range sf {
		if strings.ToUpper(strings.TrimSpace(r.Sinonim)) == syn {
			out[strings.TrimSpace(r.Station)] = struct{}{}
		}
	}
	return out
}

// SFCandidates собирает группы-кандидаты вагонов для с.ф.-синонима: агрегирует записи
// дислокации на станциях синонима (из sf) по ключу StationOper|индекс|дата|IdDisl,
// среди «наших» площадок (target). Исключает IdDisl, уже занятые обычными нитками
// (usedIdDisl). Перенос эталонного filterAndAggregateByStations (+ excludeUsedIdDisl);
// порт-специфику (набор «наших» площадок) движок получает параметром — не хардкодит.
func SFCandidates(
	synonym string,
	sf []SFRecord,
	records []domain.Dislocation,
	target map[string]struct{},
	usedIdDisl map[string]struct{},
) []SFGroup {
	stations := SFStations(synonym, sf)
	if len(stations) == 0 {
		return nil
	}

	groups := map[string]*SFGroup{}
	subIdx := map[string]map[string]int{} // ключ группы → sgKey → индекс подгруппы в g.SubGroups
	for i := range records {
		r := &records[i]
		if _, ok := stations[strings.TrimSpace(r.StationOper)]; !ok {
			continue // не станция формирования синонима
		}
		if !isTarget(r.Naznach, r.GruzpolS, target) {
			continue // не наша площадка
		}
		if r.IdDisl != "" {
			if _, used := usedIdDisl[r.IdDisl]; used {
				continue // группа уже занята обычной ниткой
			}
		}

		indexToUse := r.Index
		if r.IndexMain != "" {
			indexToUse = r.IndexMain
		}
		key := r.StationOper + "|" + indexToUse + "|" + dateKey(r.DateOp) + "|" + r.IdDisl

		g := groups[key]
		if g == nil {
			g = &SFGroup{
				Key:         key,
				StationOper: r.StationOper,
				Index:       indexToUse,
				IndexMain:   r.IndexMain,
				DateOp:      r.DateOp,
				IdDisl:      r.IdDisl,
			}
			groups[key] = g
			subIdx[key] = map[string]int{}
		}
		g.Quantity++
		g.Vagons = append(g.Vagons, r.Vagon)

		// Подгруппы (как в addToAggregation) — для «Состава» и станции нитки.
		sgKey := fmt.Sprintf("%s|%s|%s|%s",
			emptyToDefault(r.IndexMain, unknownFallback),
			emptyToDefault(r.Naznach, unknownFallback),
			emptyToDefault(r.Sms1, unknownFallback),
			emptyToDefault(r.GruzpolS, unknownFallback))
		if idx, ok := subIdx[key][sgKey]; ok {
			g.SubGroups[idx].Quantity++
		} else {
			g.SubGroups = append(g.SubGroups, SubGroup{
				Key: sgKey, IndexMain: r.IndexMain, Naznach: r.Naznach, Sms1: r.Sms1,
				GruzpolS: r.GruzpolS, Quantity: 1, StationOper: r.StationOper,
				DateOp: r.DateOp, IdDisl: r.IdDisl,
			})
			subIdx[key][sgKey] = len(g.SubGroups) - 1
		}
	}

	out := make([]SFGroup, 0, len(groups))
	for _, g := range groups {
		out = append(out, *g)
	}
	// Стабильный порядок (как эталон сортирует группы по дате): дата → станция → индекс.
	sort.Slice(out, func(i, j int) bool {
		if di, dj := dateKey(out[i].DateOp), dateKey(out[j].DateOp); di != dj {
			return di < dj
		}
		if out[i].StationOper != out[j].StationOper {
			return out[i].StationOper < out[j].StationOper
		}
		return out[i].Index < out[j].Index
	})
	return out
}

// UsedIdDisl собирает IdDisl, занятые сматченными обычными нитками (для исключения
// из кандидатов с.ф.). Перенос excludeUsedIdDisl.
func UsedIdDisl(matches []NitkaMatch) map[string]struct{} {
	used := map[string]struct{}{}
	for _, m := range matches {
		if m.Matched && m.IdDisl != "" {
			used[m.IdDisl] = struct{}{}
		}
	}
	return used
}

// dateKey — ключ даты операции (YYYY-MM-DD) или "" для nil.
func dateKey(t *domain.LocalTime) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.Time().Format("2006-01-02")
}
