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
// проставляются как вагоны нитки с.ф. Три вида кандидатов:
//   - «группы» (Departed=false, Formed=false): вагоны на станции формирования синонима,
//     сборный из них ещё не собран (индексы прибывших поездов / Б/И);
//   - «сформированные» (Formed=true): состав собран и уже получил реальный индекс
//     АААА-БББ-ВВВВ (АААА = kod_4 станции формирования), но ещё стоит на ней;
//   - «уехавшие» (Departed=true): сформированный сборный покинул станцию — ловим по
//     тому же префиксу индекса, без плановых данных (хвост ВВВВ не проверяем:
//     сборный может следовать не до нашей станции).
type SFGroup struct {
	Key         string            // уникальный ключ группы (StationOper|index|date|IdDisl) — id_disl не уникален!
	StationOper string            // станция текущей операции (для уехавших — где поезд сейчас)
	Departed    bool              // true: покинул станцию формирования (найден по префиксу индекса)
	Formed      bool              // true: сформирован (реальный индекс), но ещё на станции формирования
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
// дислокации по ключу StationOper|индекс|дата|IdDisl среди «наших» площадок (target).
// Кандидаты двух видов: стоящие на станциях синонима (из sf) и уехавшие — по префиксу
// индекса = kod_4 станции формирования (kod4ByStation: имя станции → все её kod_4
// строками; пустая мапа отключает поиск уехавших). Исключает IdDisl, уже занятые обычными
// нитками (usedIdDisl); для уехавших дополнительно — прибывших и порожних. Перенос
// эталонного filterAndAggregateByStations (+ excludeUsedIdDisl + расширение departed);
// порт-специфику (набор «наших» площадок) движок получает параметром — не хардкодит.
func SFCandidates(
	synonym string,
	sf []SFRecord,
	records []domain.Dislocation,
	target map[string]struct{},
	usedIdDisl map[string]struct{},
	kod4ByStation map[string][]string,
) []SFGroup {
	stations := SFStations(synonym, sf)
	if len(stations) == 0 {
		return nil
	}
	// Префиксы индекса уехавших сборных = все kod_4 станций формирования синонима
	// (у крупной станции несколько парков с одним именем и разными кодами).
	prefixes := map[string]struct{}{}
	for st := range stations {
		for _, k4 := range kod4ByStation[st] {
			if k4 = strings.TrimSpace(k4); k4 != "" {
				prefixes[k4] = struct{}{}
			}
		}
	}

	groups := map[string]*SFGroup{}
	subIdx := map[string]map[string]int{} // ключ группы → sgKey → индекс подгруппы в g.SubGroups
	for i := range records {
		r := &records[i]
		_, standing := stations[strings.TrimSpace(r.StationOper)]
		departed := !standing && isDepartedSF(r, prefixes)
		// Сформирован, но ещё на станции: стоит на станции формирования, а индекс уже
		// «свой» (префикс = kod_4 этой станции). Порожние — не сборные под подвод.
		formed := standing && r.PorozhPriznak != "1" && hasSFIndexPrefix(r, prefixes)
		if !standing && !departed {
			continue // ни на станции формирования, ни уехавший с неё
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
				Departed:    departed,
				Formed:      formed,
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
	// Стабильный порядок: уехавшие → сформированные (секция «поезд-кандидат» в
	// диалоге) → группы; внутри — как эталон: дата → станция → индекс.
	rank := func(g SFGroup) int {
		switch {
		case g.Departed:
			return 0
		case g.Formed:
			return 1
		default:
			return 2
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if ri, rj := rank(out[i]), rank(out[j]); ri != rj {
			return ri < rj
		}
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

// hasSFIndexPrefix — первый сегмент индекса (АААА из АААА-БББ-ВВВВ) совпадает с kod_4
// одной из станций формирования синонима. Смотрим и текущий Index, и IndexMain (после
// переформирования в пути текущий индекс меняется, IndexMain хранит исходный).
func hasSFIndexPrefix(r *domain.Dislocation, prefixes map[string]struct{}) bool {
	if len(prefixes) == 0 {
		return false
	}
	for _, idx := range []string{r.IndexMain, r.Index} {
		if seg, _, ok := strings.Cut(strings.TrimSpace(idx), "-"); ok {
			if _, hit := prefixes[seg]; hit {
				return true
			}
		}
	}
	return false
}

// isDepartedSF — уехавший со станции формирования сборный: индекс со «своим»
// префиксом (hasSFIndexPrefix). Прибывшие (10/12) и порожние не предлагаются.
func isDepartedSF(r *domain.Dislocation, prefixes map[string]struct{}) bool {
	if r.PorozhPriznak == "1" {
		return false
	}
	if r.Status != nil && (*r.Status == 10 || *r.Status == 12) {
		return false
	}
	return hasSFIndexPrefix(r, prefixes)
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
