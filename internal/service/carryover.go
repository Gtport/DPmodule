package service

import (
	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

// CarryOverStats — диагностика S2-2.
type CarryOverStats struct {
	Matched int // вагонов найдено в актуальной (перенос из снимка)
	New     int // новых вагонов (первичная установка index/invoice)
	Sticky  int // статус удержан (4/5/10 на той же станции операции)
}

// applyCarryOver — Stage 2 (S2-2): для вагонов, найденных в актуальном снимке,
// переносит поля из прошлого снимка (§ enrichFromActual gtport); для новых —
// первичная установка index_main/index_last/invoice_main. Мутирует kept на месте.
// Идёт ПОСЛЕ Stage 1 и ДО reconcileCandidates (может держать статус 4/5/10). Marka —
// отдельный шаг S2-3 (новые вагоны + оставшиеся пустые груз-поля).
func applyCarryOver(kept []domain.Dislocation, actual *ActualCache) CarryOverStats {
	now := clock.Now()
	var st CarryOverStats
	for i := range kept {
		r := &kept[i]
		if r.Vagon == "" {
			continue
		}
		if ex, ok := actual.FindVagonInActual(r.Vagon); ok {
			if enrichFromActual(r, &ex, now) {
				st.Sticky++
			}
			st.Matched++
		} else {
			initNewVagon(r)
			st.New++
		}
	}
	return st
}

// enrichFromActual переносит данные из актуальной записи ex в новую newRec. Возвращает
// true, если сработал sticky-статус (4/5/10). Заморозки на статусе 10 нет (отход от
// эталона gtport, решение владельца): прибывший обновляется свежими данными РЖД и
// после выгрузки честно переходит 10 → 12 (веха выгрузки уходит в vagon_history).
func enrichFromActual(newRec, ex *domain.Dislocation, now domain.LocalTime) bool {
	preserveCoordinates(newRec, ex)

	exStatus := 0
	if ex.Status != nil {
		exStatus = *ex.Status
	}

	// Sticky 4/5: пока станция операции та же — держим статус (брошен / долгий простой).
	sticky := false
	if (exStatus == 5 || exStatus == 4) && newRec.CodeStationOper == ex.CodeStationOper {
		s := exStatus
		newRec.Status = &s
		sticky = true
	}

	// Sticky 10: вагон прибыл и стоит там же, но свежая выгрузка потеряла date_prib
	// (новый статус 9). Держим факт прибытия: статус 10, date_prib и date_kon.
	if exStatus == 10 && newRec.Status != nil && *newRec.Status == 9 &&
		newRec.CodeStationOper == ex.CodeStationOper {
		s := 10
		newRec.Status = &s
		newRec.DatePrib = ex.DatePrib
		newRec.DateKon = newRec.DateOpJd // как computeDateKon для статуса 10
		sticky = true
	}

	copySelectedFromActual(newRec, ex, now) // выборочный перенос
	fixZeroRasst(newRec, ex)
	return sticky
}

// copySelectedFromActual — переносим выбранные поля из актуальной, свежие данные
// РЖД оставляем.
func copySelectedFromActual(newRec, ex *domain.Dislocation, now domain.LocalTime) {
	newRec.ID = ex.ID
	newRec.IndexMain = determineIndexMain(newRec, ex)
	newRec.IndexLast = determineIndexLast(newRec, ex)
	newRec.IndexPp = ex.IndexPp

	newRec.Gruzpol = ex.Gruzpol
	newRec.GruzpolS = ex.GruzpolS
	newRec.Naznach = ex.Naznach
	newRec.PlanJd = ex.PlanJd
	newRec.PlanMsk = ex.PlanMsk

	// Переадресация — операторское решение: поток РЖД её не знает, переносим
	// безусловно; снимается только явной отменой (очистка полей в снимке).
	newRec.PereadrType = ex.PereadrType
	newRec.PereadrPort = ex.PereadrPort

	// Груз-поля из актуальной, только если там заполнен грузоотправитель (иначе
	// оставляем как есть — заполнит marka в S2-3).
	if ex.Gruzotpr != "" {
		newRec.Gruzotpr = ex.Gruzotpr
		newRec.CargoS = ex.CargoS
		newRec.CargoSms = ex.CargoSms
		newRec.CargoGroup = ex.CargoGroup
		newRec.Client = ex.Client
		newRec.Sms1, newRec.Sms2, newRec.Sms3 = ex.Sms1, ex.Sms2, ex.Sms3
		newRec.Sprav1, newRec.Sprav2, newRec.Sprav3 = ex.Sprav1, ex.Sprav2, ex.Sprav3
		newRec.Color = ex.Color
		newRec.Param1, newRec.Param2, newRec.Param3 = ex.Param1, ex.Param2, ex.Param3
	}

	// invoice_main стабилен (не меняется после первого появления).
	if ex.InvoiceMain != "" {
		newRec.InvoiceMain = ex.InvoiceMain
	} else if newRec.InvoiceMain == "" {
		newRec.InvoiceMain = newRec.Invoice
	}

	carryNewFields(newRec, ex)
	newRec.CreatedAt = ex.CreatedAt // момент первого появления вагона
	newRec.UpdatedAt = now
}

// initNewVagon — первичная установка для нового вагона (нет в актуальной): index_main/
// index_last = текущий index, invoice_main = текущая накладная. Груз — marka в S2-3.
func initNewVagon(r *domain.Dislocation) {
	if r.InvoiceMain == "" {
		r.InvoiceMain = r.Invoice
	}
	if r.IndexMain == "" {
		r.IndexMain = r.Index
	}
	if r.IndexLast == "" {
		r.IndexLast = r.Index
	}
}

// determineIndexMain: у актуальной пусто/«Б/И» → текущий index; иначе актуальный
// index_main (родительский индекс фиксируется после первого появления).
func determineIndexMain(newRec, ex *domain.Dislocation) string {
	if ex.IndexMain == "Б/И" || ex.IndexMain == "" {
		return newRec.Index
	}
	return ex.IndexMain
}

// determineIndexLast: отслеживает предыдущий индекс поезда.
func determineIndexLast(newRec, ex *domain.Dislocation) string {
	if ex.Index == "Б/И" || ex.Index == "" {
		return newRec.Index
	}
	if newRec.Index == ex.Index {
		return ex.IndexLast
	}
	return ex.Index
}

// preserveCoordinates: пустые/нулевые координаты новой берём из актуальной.
func preserveCoordinates(newRec, ex *domain.Dislocation) {
	newEmpty := isBlankCoord(newRec.Latitude) || isBlankCoord(newRec.Longitude)
	if !newEmpty {
		return
	}
	if !isBlankCoord(ex.Latitude) && !isBlankCoord(ex.Longitude) {
		newRec.Latitude = ex.Latitude
		newRec.Longitude = ex.Longitude
	}
}

func isBlankCoord(v string) bool {
	return v == "" || v == "0" || v == "0.000000"
}

// fixZeroRasst: RasstStanNazn=0, но вагон НЕ на станции назначения (StanNazn ≠
// StationOper = в пути) → ошибочный ноль, берём валидное из актуальной.
func fixZeroRasst(newRec, ex *domain.Dislocation) {
	if newRec.RasstStanNazn != nil && *newRec.RasstStanNazn == 0 &&
		newRec.StanNazn != newRec.StationOper &&
		ex.RasstStanNazn != nil && *ex.RasstStanNazn > 0 {
		newRec.RasstStanNazn = ex.RasstStanNazn
	}
}

// carryNewFields: новые поля (которых не было в gtport) — всегда из актуальной, если
// там не пусто (важно для запасного ЛК, где эти поля не приходят).
func carryNewFields(newRec, ex *domain.Dislocation) {
	if ex.CarOwnerName != "" {
		newRec.CarOwnerName = ex.CarOwnerName
	}
	if ex.CarOwnerOkpo != "" {
		newRec.CarOwnerOkpo = ex.CarOwnerOkpo
	}
	if ex.CarTenantName != "" {
		newRec.CarTenantName = ex.CarTenantName
	}
	if ex.CarTenantOkpo != "" {
		newRec.CarTenantOkpo = ex.CarTenantOkpo
	}
	if ex.CarTrustedName != "" {
		newRec.CarTrustedName = ex.CarTrustedName
	}
	if ex.CarTrustedOkpo != "" {
		newRec.CarTrustedOkpo = ex.CarTrustedOkpo
	}
	if ex.GtdNumber != "" {
		newRec.GtdNumber = ex.GtdNumber
	}
	if ex.FreightExactName != "" {
		newRec.FreightExactName = ex.FreightExactName
	}
	if ex.Zayavka != "" {
		newRec.Zayavka = ex.Zayavka
	}
}
