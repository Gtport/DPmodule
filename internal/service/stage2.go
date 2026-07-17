package service

import (
	"context"
	"time"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

// applyStatus6Transition — Stage 2 (§3.16): при ПЕРЕХОДЕ вагона на статус 6 (в новом
// батче 6, в актуальной вагон есть и его статус ≠ 6 и < 10) фиксируем донора перегруза.
// Переход из 10/12 донором не считается: груз доехал и выгружен у нас, порожний
// отъезд после выгрузки — штатный, передавать нечего.
// Донор хранит полную запись (для матча по станции операции + вес + срок и передачи
// груза/назначения в S2-3). gruzpol_s/naznach обнуляются («0») ТОЛЬКО в снимке (kept) —
// вагон к нам не доедет, в выборки не попадает; в самой записи status6 они реальные.
// Вызывается ПОСЛЕ carry-over (у записи полные данные) и ДО подмены снимка.
func applyStatus6Transition(ctx context.Context, kept []domain.Dislocation, actual *ActualCache, cache *Status6Cache) (int, error) {
	var donors []domain.Dislocation
	for i := range kept {
		r := &kept[i]
		if r.Vagon == "" || r.Status == nil || *r.Status != 6 {
			continue
		}
		ex, ok := actual.FindVagonInActual(r.Vagon)
		if !ok || (ex.Status != nil && *ex.Status == 6) {
			continue // нет в актуальной (новый сразу 6) или уже был 6 — не переход
		}
		if ex.Status != nil && *ex.Status >= 10 {
			continue // прибыл/выгружен у нас — груз доехал, порожняк уезжает штатно, не донор
		}
		donor := *r // после carry-over — полные данные груза и назначения
		donors = append(donors, donor)
		// Обнуляем ТОЛЬКО в снимке — к нам не доедет, в выборки не попадает. В самой
		// записи status6 gruzpol_s/naznach хранятся реальными: нужны при передаче
		// приёмнику (S2-3 донорство, §3.17).
		r.GruzpolS = "0"
		r.Naznach = "0"
	}
	return cache.Upsert(ctx, donors)
}

// applyStatus6Donorship — Stage 2 (S2-3c, §3.17): для новых вагонов, которым marka не
// дала груз (Gruzotpr пусто), ищем донора перегруза в status6 по трём параметрам:
// станция операции донора == станция погрузки нового (code_station_oper == code_station_nach),
// вес ±0.1 т, точный срок доставки. При совпадении приёмник НАСЛЕДУЕТ груз/назначение
// донора, оставаясь собой физически (номер, позиция, операция, время, индексы — свои);
// номер донора пишется в peregruz, донор удаляется из status6. Один донор — одному
// приёмнику. Идёт ПОСЛЕ applyStatus6Transition (свежие доноры этого батча доступны) и
// ДО подмены снимка.
func applyStatus6Donorship(ctx context.Context, kept []domain.Dislocation, cache *Status6Cache) (int, error) {
	donors := cache.Donors()
	if len(donors) == 0 {
		return 0, nil
	}
	used := make(map[string]struct{}, len(donors))
	var usedVagons []string
	for i := range kept {
		r := &kept[i]
		if r.Vagon == "" || r.Gruzotpr != "" {
			continue // нет номера или груз уже есть (marka заполнила / перенёсся)
		}
		for j := range donors {
			d := &donors[j]
			if d.Vagon == "" {
				continue
			}
			if _, taken := used[d.Vagon]; taken {
				continue
			}
			if donorMatches(d, r) {
				transferFromDonor(r, d)
				r.Peregruz = d.Vagon
				used[d.Vagon] = struct{}{}
				usedVagons = append(usedVagons, d.Vagon)
				break
			}
		}
	}
	if len(usedVagons) == 0 {
		return 0, nil
	}
	return cache.DeleteByVagons(ctx, usedVagons)
}

// donorMatches — три параметра совпадения донора и приёмника (§3.17): станция операции
// донора == станция погрузки/отправления нового; вес ±0.1 т; точный срок доставки (по
// дате). Отсутствие любого из сравниваемых значений → не совпадает.
func donorMatches(d, r *domain.Dislocation) bool {
	if d.CodeStationOper == "" || d.CodeStationOper != r.CodeStationNach {
		return false
	}
	if d.Ves == nil || r.Ves == nil {
		return false
	}
	if diff := *d.Ves - *r.Ves; diff > 0.1 || diff < -0.1 {
		return false
	}
	if d.DateDostav == nil || r.DateDostav == nil {
		return false
	}
	dd, rd := time.Time(*d.DateDostav), time.Time(*r.DateDostav)
	return dd.Year() == rd.Year() && dd.YearDay() == rd.YearDay()
}

// transferFromDonor переносит на приёмника груз, назначение и станцию отправления донора
// (§3.17, п.4a). НЕ трогает физическую идентичность приёмника: номер, позицию, операцию,
// время, статус, индексы, накладные, таймстемпы.
func transferFromDonor(r, d *domain.Dislocation) {
	// груз
	r.Gruzotpr = d.Gruzotpr
	r.GruzotprOkpo = d.GruzotprOkpo
	r.CodeCargo = d.CodeCargo
	r.CargoS = d.CargoS
	r.CargoSms = d.CargoSms
	r.CargoGroup = d.CargoGroup
	r.Ves = d.Ves
	r.Client = d.Client
	r.Sms1 = d.Sms1
	r.Sms2 = d.Sms2
	r.Sms3 = d.Sms3
	r.Sprav1 = d.Sprav1
	r.Sprav2 = d.Sprav2
	r.Sprav3 = d.Sprav3
	r.Color = d.Color
	// назначение
	r.Gruzpol = d.Gruzpol
	r.GruzpolS = d.GruzpolS
	r.Naznach = d.Naznach
	r.CodeStanNazn = d.CodeStanNazn
	r.Code4StanNazn = d.Code4StanNazn
	r.StanNazn = d.StanNazn
	// станция отправления (груз изначально ехал со станции донора)
	r.CodeStationNach = d.CodeStationNach
	r.StationNach = d.StationNach
	r.DorogaNach = d.DorogaNach
	// срок доставки (совпал при матче — фиксируем донорский)
	r.DateDostav = d.DateDostav
}

// Status9Stats — диагностика согласования таблицы кандидатов (S2-1).
type Status9Stats struct {
	Inserted int // новых живых кандидатов статуса 9 (первое появление)
	Removed  int // снято (вагон вернулся в поток / сменил статус)
	Missing8 int // пропавших записано/обновлено (статус 8)
}

// reconcileCandidates — Stage 2 (S2-1): согласование таблицы кандидатов в прибытие
// (status9) с новым батчем и актуальным снимком. Вызывается ПОСЛЕ Stage 1 (у записей
// есть статус) и ДО подмены снимка (actual — прежний снимок). §3.14.
//
// Живой батч:
//   - статус 9, первое появление (в актуальной ∉ {9}): вагона нет в таблице → insert;
//     лежал как 8 (пропадал) → снять 8 и записать 9 (вернулся живым кандидатом);
//   - статус ≠ 9, вагон в таблице → снять (вернулся в поток / сменил статус).
//
// Пропавшие (в актуальной, нет в батче):
//   - статус актуального 6 / 10 / 12 (порожний в пути, прибыл, выгружен) → выбыл
//     штатно, ничего;
//   - иначе → копия актуальной + статус 8 (при conflict — перевод 9→8, правки целы).
//
// Статус-9 записи ОСТАЮТСЯ в наборе kept (в снимке). Статус-8 в снимок НЕ идёт.
func reconcileCandidates(
	ctx context.Context,
	kept []domain.Dislocation,
	actual *ActualCache,
	cache *Status9Cache,
) (Status9Stats, error) {
	inTable := cache.Statuses() // из RAM
	var err error

	seen := make(map[string]struct{}, len(kept))
	var toInsert9 []domain.Dislocation
	var toDelete []string

	for i := range kept {
		r := kept[i]
		if r.Vagon == "" {
			continue
		}
		seen[r.Vagon] = struct{}{}
		tblStatus, has := inTable[r.Vagon]

		if r.Status != nil && *r.Status == 9 {
			prev, ok := actual.FindVagonInActual(r.Vagon)
			first := !ok || prev.Status == nil || *prev.Status != 9
			switch {
			case has && tblStatus == 8:
				// вернулся живым 9 из «пропавших» → снять защищённый 8, записать 9
				toDelete = append(toDelete, r.Vagon)
				toInsert9 = append(toInsert9, r)
			case !has && first:
				// первое появление живого кандидата
				toInsert9 = append(toInsert9, r)
			}
			// иначе (в таблице уже 9, либо не первое появление) — оставляем как есть
		} else if has {
			// вернулся в поток / сменил статус → снять кандидата (8 или 9)
			toDelete = append(toDelete, r.Vagon)
		}
	}

	// Пропавшие: были в актуальной, нет в новом батче.
	now := clock.Now()
	var toMissing8 []domain.Dislocation
	for _, v := range actual.All() {
		if v.Vagon == "" {
			continue
		}
		if _, present := seen[v.Vagon]; present {
			continue
		}
		if v.Status != nil && (*v.Status == 6 || *v.Status >= 10) {
			continue // выбыл штатно: порожний в пути (6) / прибыл (10) / выгружен (12)
		}
		rec := v
		s8 := 8
		rec.Status = &s8
		rec.UpdatedAt = now
		toMissing8 = append(toMissing8, rec)
	}

	var st Status9Stats
	if st.Removed, err = cache.DeleteByVagons(ctx, toDelete); err != nil {
		return Status9Stats{}, err
	}
	if st.Inserted, err = cache.InsertNew(ctx, toInsert9); err != nil {
		return Status9Stats{}, err
	}
	if st.Missing8, err = cache.UpsertMissing(ctx, toMissing8); err != nil {
		return Status9Stats{}, err
	}
	return st, nil
}
