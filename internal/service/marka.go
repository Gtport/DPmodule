package service

import (
	"strconv"

	"github.com/Gtport/DPmodule/internal/domain"
)

// MarkaStats — диагностика S2-3 (обогащение новых вагонов из marka + перестановки).
type MarkaStats struct {
	Candidates      int // записей, которым требовался груз (Gruzotpr пусто)
	FilledFull      int // груз заполнен полным совпадением marka
	FilledPartial   int // груз заполнен частичным совпадением (станция+груз)
	MissedMarka     int // marka не нашла груз (кандидаты донорства S2-3c)
	NaznachOverride int // Naznach взят из naznach_station (не дефолт GruzpolS)
}

// applyMarkaEnrichment — Stage 2 (S2-3, §3.17): обогащение груза и назначения новых
// вагонов. Идёт ПОСЛЕ carry-over и ДО донорства status6. Груз добираем у записей с
// пустым Gruzotpr (новые вагоны + существующие, у кого груз не перенёсся из актуальной);
// назначение (Naznach) — у записей с пустым Naznach (новые; у существующих перенесено
// carry-over'ом). marka — сокращённый набор нашей схемы (без sms_2/3, sprav, color).
func applyMarkaEnrichment(kept []domain.Dislocation, dir *DirectoryCache) MarkaStats {
	var st MarkaStats
	for i := range kept {
		r := &kept[i]
		if r.Gruzotpr == "" { // требуется груз
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
	return st
}

type markaMatch int

const (
	markaNone markaMatch = iota
	markaFull
	markaPartial
)

// enrichFromMarka заполняет груз-поля из marka по ключу (ОКПО грузоотправителя, код
// станции отправления, код груза). Полное совпадение → применяем; иначе, ТОЛЬКО если
// ОКПО в marka вовсе не известен, — частичное по (станция+груз) любого отправителя
// (паритет с gtlogic: для известного ОКПО пробел не домысливаем). §3.17.
func enrichFromMarka(r *domain.Dislocation, dir *DirectoryCache) markaMatch {
	if r.GruzotprOkpo == "" || r.CodeStationNach == "" || r.CodeCargo == "" {
		return markaNone
	}
	okpo, err1 := strconv.ParseInt(r.GruzotprOkpo, 10, 64)
	st, err2 := strconv.ParseInt(r.CodeStationNach, 10, 64)
	cg, err3 := strconv.ParseInt(r.CodeCargo, 10, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return markaNone
	}
	if recs, ok := dir.GetMarkaByCompositeKey(okpo, st, cg); ok && len(recs) > 0 {
		applyMarka(r, recs[0])
		return markaFull
	}
	if !dir.MarkaHasOkpo(okpo) {
		if recs := dir.GetMarkaByStationAndCargo(st, cg); len(recs) > 0 {
			applyMarka(r, recs[0])
			return markaPartial
		}
	}
	return markaNone
}

// applyMarka переносит непустые груз-поля записи marka в дислокацию (сокращённый набор
// схемы: shipper/cargo_s/client/cargo_group/sms_1). Пустые поля marka не затирают.
func applyMarka(r *domain.Dislocation, m domain.Marka) {
	if m.Shipper != "" {
		r.Gruzotpr = m.Shipper
	}
	if m.CargoS != "" {
		r.CargoS = m.CargoS
	}
	if m.CargoGroup != "" {
		r.CargoGroup = m.CargoGroup
	}
	if m.Client != "" {
		r.Client = m.Client
	}
	if m.Sms1 != "" {
		r.CargoSms = m.Sms1
		r.Sms1 = m.Sms1
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
