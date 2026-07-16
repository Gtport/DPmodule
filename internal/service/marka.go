package service

import (
	"strconv"

	"github.com/Gtport/DPmodule/internal/domain"
)

// MarkaStats — диагностика S2-3 (обогащение новых вагонов из marka + перестановки).
type MarkaStats struct {
	Candidates      int // записей, которым требовалась бизнес-атрибуция (Gruzotpr пусто)
	FilledFull      int // атрибуция заполнена полным совпадением marka (ОКПО+станция+группа)
	FilledPartial   int // заполнена частичным совпадением (станция+группа)
	FilledByTrain   int // заполнена наследованием по составу (S2-3d, единогласные соседи)
	MissedMarka     int // не нашли ни marka, ни состав (кандидаты донорства S2-3c)
	NaznachOverride int // Naznach взят из naznach_station (не дефолт GruzpolS)
}

// applyMarkaEnrichment — Stage 2 (S2-3, §3.17): груз и назначение после carry-over,
// ДО донорства status6. Порядок:
//  1. словарь cargo переприменяется по коду ЕТСНГ — для известного кода словарь
//     ИСТОЧНИК ПРАВДЫ (затирает перенесённое carry-over'ом); пустой/неизвестный
//     код — остаётся перенесённое (порожний сохраняет груз прошлого рейса);
//  2. бизнес-атрибуция из marka по ключу (ОКПО отправителя, станция отправления,
//     ГРУППА груза) — только записям с пустым Gruzotpr (новые вагоны);
//     Naznach — перестановка назначения (новым вагонам);
//  3. наследование по составу (S2-3d): оставшимся без атрибуции — от единогласных
//     соседей по IndexMain (кривое/пустое ОКПО или станция у части вагонов состава);
//  4. расчётный Sms2 = Sms1 + CargoSms (после наследования, по всем записям).
func applyMarkaEnrichment(kept []domain.Dislocation, dir *DirectoryCache) MarkaStats {
	var st MarkaStats
	for i := range kept {
		r := &kept[i]
		reapplyCargoDict(r, dir)
		if r.Gruzotpr == "" { // требуется бизнес-атрибуция
			st.Candidates++
			switch enrichFromMarka(r, dir) {
			case markaFull:
				st.FilledFull++
			case markaPartial:
				st.FilledPartial++
			case markaNone:
				st.MissedMarka++
			}
		}
		if r.Naznach == "" { // назначение ещё не определено (новый вагон)
			if enrichNaznach(r, dir) {
				st.NaznachOverride++
			}
		}
	}
	st.FilledByTrain = applyTrainInheritance(kept)
	st.MissedMarka -= st.FilledByTrain // унаследовавшие больше не кандидаты донорства
	for i := range kept {
		kept[i].Sms2 = joinNonEmpty(kept[i].Sms1, kept[i].CargoSms)
	}
	return st
}

// applyTrainInheritance — S2-3d: наследование бизнес-атрибуции по составу.
// Вагону без атрибуции (кривое/пустое/неизвестное ОКПО, станция вне marka)
// переносится атрибуция соседей по составу (IndexMain) — но ТОЛЬКО при полном
// единогласии доноров: составы бывают сборные, при разногласии не гадаем
// (вагон остаётся кандидатом донорства S2-3c). Наследуются выходные поля
// (Gruzotpr/Client/Sms1/Sms3), сырое GruzotprOkpo не подделываем. Кандидат с
// собственной группой груза наследует только от состава той же группы.
// Расширение сверх эталона gtlogic (решение владельца). Возвращает число
// заполненных записей.
func applyTrainInheritance(kept []domain.Dislocation) int {
	type trainAttr struct {
		gruzotpr, client, sms1, sms3, cargoGroup string
		conflict                                 bool
	}
	donors := map[string]*trainAttr{}
	for i := range kept {
		r := &kept[i]
		if r.Gruzotpr == "" || !trainInheritEligible(r) {
			continue
		}
		a, ok := donors[r.IndexMain]
		if !ok {
			donors[r.IndexMain] = &trainAttr{
				gruzotpr: r.Gruzotpr, client: r.Client,
				sms1: r.Sms1, sms3: r.Sms3, cargoGroup: r.CargoGroup,
			}
			continue
		}
		if a.gruzotpr != r.Gruzotpr || a.client != r.Client ||
			a.sms1 != r.Sms1 || a.sms3 != r.Sms3 || a.cargoGroup != r.CargoGroup {
			a.conflict = true
		}
	}

	filled := 0
	for i := range kept {
		r := &kept[i]
		if r.Gruzotpr != "" || !trainInheritEligible(r) {
			continue
		}
		a, ok := donors[r.IndexMain]
		if !ok || a.conflict {
			continue
		}
		if r.CargoGroup != "" && r.CargoGroup != a.cargoGroup {
			continue // чужеродный вагон в составе — не наследуем
		}
		r.Gruzotpr, r.Client, r.Sms1, r.Sms3 = a.gruzotpr, a.client, a.sms1, a.sms3
		filled++
	}
	return filled
}

// trainInheritEligible — участвует ли запись в наследовании по составу (донором
// или кандидатом): гружёная, с осмысленным индексом состава, не на станции
// отправления (статус ≠ 0 — условие владельца).
func trainInheritEligible(r *domain.Dislocation) bool {
	return r.PorozhPriznak != "1" &&
		r.IndexMain != "" && r.IndexMain != "Б/И" &&
		r.Status != nil && *r.Status != 0
}

// reapplyCargoDict — повторное применение словаря cargo после carry-over (первый
// раз — Stage 1, до carry-over). Известный код → группа/имя/метка из словаря
// безусловно; пустой или неизвестный код — поля не трогаем.
func reapplyCargoDict(r *domain.Dislocation, dir *DirectoryCache) {
	if r.CodeCargo == "" {
		return
	}
	kod, err := strconv.ParseInt(r.CodeCargo, 10, 64)
	if err != nil {
		return
	}
	if g, ok := dir.GetCargoByKod(kod); ok {
		r.CargoGroup = g.CargoGroup
		r.CargoS = g.CargoS
		r.CargoSms = g.CargoSms
	}
}

type markaMatch int

const (
	markaNone markaMatch = iota
	markaFull
	markaPartial
)

// enrichFromMarka заполняет бизнес-атрибуцию из marka по ключу (ОКПО грузоотправителя,
// код станции отправления, ГРУППА груза). Полное совпадение → применяем; иначе, ТОЛЬКО
// если ОКПО в marka вовсе не известен, — частичное по (станция+группа) любого
// отправителя (для известного ОКПО пробел не домысливаем). Группа — из словаря cargo
// (шаг 1 applyMarkaEnrichment); пустая группа (порожний, код вне словаря) → none.
func enrichFromMarka(r *domain.Dislocation, dir *DirectoryCache) markaMatch {
	if r.GruzotprOkpo == "" || r.CodeStationNach == "" || r.CargoGroup == "" {
		return markaNone
	}
	okpo, err1 := strconv.ParseInt(r.GruzotprOkpo, 10, 64)
	st, err2 := strconv.ParseInt(r.CodeStationNach, 10, 64)
	if err1 != nil || err2 != nil {
		return markaNone
	}
	if m, ok := dir.GetMarkaByCompositeKey(okpo, st, r.CargoGroup); ok {
		applyMarka(r, m)
		return markaFull
	}
	if !dir.MarkaHasOkpo(okpo) {
		if recs := dir.GetMarkaByStationAndGroup(st, r.CargoGroup); len(recs) > 0 {
			applyMarka(r, recs[0])
			return markaPartial
		}
	}
	return markaNone
}

// applyMarka переносит непустую бизнес-атрибуцию записи marka в дислокацию
// (shipper/client/sms_1/sms_3). Груз-поля marka больше не даёт — они из словаря
// cargo; Sms2 — расчётный (applyMarkaEnrichment, шаг 3). Пустые поля не затирают.
func applyMarka(r *domain.Dislocation, m domain.Marka) {
	if m.Shipper != "" {
		r.Gruzotpr = m.Shipper
	}
	if m.Client != "" {
		r.Client = m.Client
	}
	if m.Sms1 != "" {
		r.Sms1 = m.Sms1
	}
	if m.Sms3 != "" {
		r.Sms3 = m.Sms3
	}
}

// joinNonEmpty — склейка непустых частей через пробел (расчёт Sms2).
func joinNonEmpty(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + " " + b
	}
}

// enrichNaznach устанавливает «фактическое назначение» (площадка внутри порта). По
// умолчанию = GruzpolS; если для (станция назначения, станция отправления) задана
// перестановка в naznach_station — берём её. Возвращает true, если сработала
// перестановка (не дефолт). §3.17.
func enrichNaznach(r *domain.Dislocation, dir *DirectoryCache) bool {
	r.Naznach = r.GruzpolS
	if r.StanNazn == "" || r.StationNach == "" {
		return false
	}
	if nz, ok := dir.GetNaznach(r.StanNazn, r.StationNach); ok {
		r.Naznach = nz
		return true
	}
	return false
}
