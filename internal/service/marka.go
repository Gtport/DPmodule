package service

import (
	"strconv"

	"github.com/Gtport/DPmodule/internal/domain"
)

// MarkaStats — диагностика S2-3 (обогащение новых вагонов из marka + перестановки).
type MarkaStats struct {
	Candidates      int // записей, которым требовалась бизнес-атрибуция (Gruzotpr пусто)
	FilledFull      int // атрибуция заполнена строгим совпадением marka (ОКПО+станция+группа)
	FilledByTrain   int // заполнена наследованием по составу (S2-3d, единогласные соседи)
	MissedMarka     int // не нашли ни marka, ни состав (кандидаты донорства S2-3c)
	NaznachOverride int // Naznach взят из naznach_station (не дефолт GruzpolS)
}

// applyMarkaEnrichment — Stage 2 (S2-3, §3.17): груз и назначение после carry-over,
// ДО донорства status6. Атрибутированные существующие вагоны (Gruzotpr непустой
// после carry-over) НЕ трогаем совсем: груз-поля и sms_2 переносятся из последнего
// снимка как есть — код груза может испортиться в пути следования, снимок вернее
// потока (решение владельца). Правки словарей на такие вагоны действуют только
// через пересчёт снимка. Остальным (новые и неатрибутированные):
//  1. словарь cargo переприменяется по коду ЕТСНГ (повтор Stage 1 — для юнитов
//     и симметрии; пустой/неизвестный код — поля не трогаются);
//  2. бизнес-атрибуция из marka СТРОГО по ключу (ОКПО отправителя, станция
//     отправления, ГРУППА груза); частичный матч (станция+группа при чужом ОКПО)
//     упразднён — подставлял атрибуцию чужого отправителя (решение владельца);
//  3. наследование по составу (S2-3d): оставшимся без атрибуции — от единогласных
//     соседей по IndexMain (кривое/пустое ОКПО или станция у части вагонов состава);
//  4. расчётный Sms2 = Sms1 + CargoSms (после наследования).
// Naznach — перестановка назначения — любым записям с пустым Naznach.
func applyMarkaEnrichment(kept []domain.Dislocation, dir *DirectoryCache) MarkaStats {
	var st MarkaStats
	fresh := make([]bool, len(kept)) // без атрибуции на входе → обогащаем и считаем sms_2
	for i := range kept {
		r := &kept[i]
		if r.Gruzotpr == "" { // требуется бизнес-атрибуция
			fresh[i] = true
			reapplyCargoDict(r, dir)
			st.Candidates++
			if enrichFromMarka(r, dir) {
				st.FilledFull++
			} else {
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
		if fresh[i] {
			kept[i].Sms2 = joinNonEmpty(kept[i].Sms1, kept[i].CargoSms)
		}
	}
	return st
}

// applyTrainInheritance — S2-3d: наследование бизнес-атрибуции по составу.
// Вагону без атрибуции (кривое/пустое/неизвестное ОКПО, станция вне marka)
// переносится атрибуция соседей по составу (IndexMain) — но ТОЛЬКО при полном
// единогласии доноров: составы бывают сборные, при разногласии не гадаем
// (вагон остаётся кандидатом донорства S2-3c). Наследуются выходные поля
// (Gruzotpr/Client/Sms1/Sms3/Color), сырое GruzotprOkpo не подделываем. Кандидат с
// собственной группой груза наследует только от состава той же группы.
// Расширение сверх эталона gtlogic (решение владельца). Возвращает число
// заполненных записей.
func applyTrainInheritance(kept []domain.Dislocation) int {
	type trainAttr struct {
		gruzotpr, client, sms1, sms3, color, cargoGroup string
		conflict                                        bool
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
				sms1: r.Sms1, sms3: r.Sms3, color: r.Color, cargoGroup: r.CargoGroup,
			}
			continue
		}
		if a.gruzotpr != r.Gruzotpr || a.client != r.Client ||
			a.sms1 != r.Sms1 || a.sms3 != r.Sms3 || a.color != r.Color ||
			a.cargoGroup != r.CargoGroup {
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
		r.Gruzotpr, r.Client, r.Sms1, r.Sms3, r.Color = a.gruzotpr, a.client, a.sms1, a.sms3, a.color
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

// reapplyCargoDict — повторное применение словаря cargo неатрибутированным
// записям (первый раз — Stage 1, до carry-over; здесь по сути повтор, нужен
// юнит-тестам без Stage 1). Известный код → группа/имя/метка из словаря;
// пустой или неизвестный код — поля не трогаем.
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

// enrichFromMarka заполняет бизнес-атрибуцию из marka СТРОГО по ключу (ОКПО
// грузоотправителя, код станции отправления, ГРУППА груза) — совпасть должны все
// три поля. Частичного матча нет: на одной станции+группе бывают разные
// отправители с разной атрибуцией, гадать нельзя (несовпавшие закрывает
// наследование по составу S2-3d либо донорство S2-3c). Группа — из словаря cargo
// (шаг 1 applyMarkaEnrichment); пустая группа (порожний, код вне словаря) → false.
func enrichFromMarka(r *domain.Dislocation, dir *DirectoryCache) bool {
	if r.GruzotprOkpo == "" || r.CodeStationNach == "" || r.CargoGroup == "" {
		return false
	}
	okpo, err1 := strconv.ParseInt(r.GruzotprOkpo, 10, 64)
	st, err2 := strconv.ParseInt(r.CodeStationNach, 10, 64)
	if err1 != nil || err2 != nil {
		return false
	}
	m, ok := dir.GetMarkaByCompositeKey(okpo, st, r.CargoGroup)
	if !ok {
		return false
	}
	applyMarka(r, m)
	return true
}

// applyMarkaRefresh — принудительное переприменение словаря marka к УЖЕ
// атрибутированным записям (механизм «Обновить справочники», гибрид владельца).
// Строгий матч по сырым ключам потока и ПЕРЕНЕСЁННОЙ группе груза — код груза
// не участвует (мог испортиться в пути, а перенесённая группа достоверна):
// строка нашлась → атрибуция и цвет применяются заново (правки словаря доезжают
// до вагонов), sms_2 пересчитывается с перенесённой меткой; не нашлась (кривое
// ОКПО/станция, наследованная атрибуция) — запись не трогаем. Возвращает число
// фактически изменённых записей.
func applyMarkaRefresh(kept []domain.Dislocation, dir *DirectoryCache) int {
	changed := 0
	for i := range kept {
		r := &kept[i]
		if r.Gruzotpr == "" { // без атрибуции — путь applyMarkaEnrichment
			continue
		}
		if r.GruzotprOkpo == "" || r.CodeStationNach == "" || r.CargoGroup == "" {
			continue
		}
		okpo, err1 := strconv.ParseInt(r.GruzotprOkpo, 10, 64)
		st, err2 := strconv.ParseInt(r.CodeStationNach, 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		m, ok := dir.GetMarkaByCompositeKey(okpo, st, r.CargoGroup)
		if !ok {
			continue
		}
		before := [6]string{r.Gruzotpr, r.Client, r.Sms1, r.Sms2, r.Sms3, r.Color}
		applyMarka(r, m)
		r.Sms2 = joinNonEmpty(r.Sms1, r.CargoSms)
		if before != [6]string{r.Gruzotpr, r.Client, r.Sms1, r.Sms2, r.Sms3, r.Color} {
			changed++
		}
	}
	return changed
}

// applyMarka переносит непустую бизнес-атрибуцию записи marka в дислокацию
// (shipper/client/sms_1/sms_3 + цвет строки для UI). Груз-поля marka больше не
// даёт — они из словаря cargo; Sms2 — расчётный (applyMarkaEnrichment, шаг 3).
// Пустые поля не затирают.
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
	if m.Color != "" {
		r.Color = m.Color
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
