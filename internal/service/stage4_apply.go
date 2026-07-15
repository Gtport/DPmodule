package service

import (
	"time"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/service/stage4"
)

// applyStage4 — Stage 4: прогноз прибытия на порт (ProgMsk) + производные ProgJd/
// DelayProg/Mistake. Агрегирует записи в поезда (ключ IdDisl|StanNazn), раскладывает
// беспланные по ниткам станции (пакет stage4), пишет результат в записи. Настройки —
// из cfg (расписание/пороги/допуски), станция и pc_* по площадке — из dir. Идёт ПОСЛЕ
// Stage 3 (нужен RaschMsk). Возвращает число записей с проставленным ProgMsk.
func applyStage4(kept []domain.Dislocation, dir *DirectoryCache, cfg *ConfigCache, cutoff int) int {
	if dir == nil || cfg == nil {
		return 0
	}
	if cutoff <= 0 {
		cutoff = 18
	}
	pol := cfg.Settings().Stage4
	if pol.MinVagonCount <= 0 {
		pol.MinVagonCount = 20 // дефолты эталона, если настройка пуста
	}
	if pol.MinVagonBros <= 0 {
		pol.MinVagonBros = 10
	}
	brosPenalty := time.Duration(pol.BrosPenaltyH) * time.Hour
	if brosPenalty <= 0 {
		brosPenalty = 72 * time.Hour
	}

	// 1. Агрегация записей в поезда.
	trains := map[string]*stage4.Train{}
	for i := range kept {
		r := &kept[i]
		if r.Status != nil && *r.Status == 10 {
			continue // прибывшие
		}
		if r.IdDisl == "" || r.StanNazn == "" {
			continue
		}
		key := r.IdDisl + "|" + r.StanNazn
		t, ok := trains[key]
		if !ok {
			station, pc := resolveStationPc(dir, r.Naznach, r.CargoGroup)
			t = &stage4.Train{
				Key:      key,
				Station:  station,
				Group:    r.Naznach + "|" + cargoRod(r.CargoGroup),
				PlanMsk:  localTimePtr(r.PlanMsk),
				RaschMsk: localTimePtr(r.RaschMsk),
				Pc:       pc,
			}
			trains[key] = t
		}
		t.VagonCount++
		if r.Status != nil && *r.Status == 5 {
			t.Bros = true
		}
	}
	if len(trains) == 0 {
		return 0
	}
	list := make([]stage4.Train, 0, len(trains))
	for _, t := range trains {
		list = append(list, *t)
	}

	// 2. Расписания, допуски, метод раскладки, пороги.
	tol := map[string]time.Duration{}
	method := map[string]string{}
	for _, p := range cfg.PlanProfiles() {
		if p.SlotToleranceH > 0 {
			tol[p.StationCode] = time.Duration(p.SlotToleranceH * float64(time.Hour))
		}
		if p.DistributionMethod != "" {
			method[p.StationCode] = p.DistributionMethod
		}
	}
	scfg := stage4.Config{
		MinVagon: pol.MinVagonCount, MinVagonBros: pol.MinVagonBros,
		BrosPenalty: brosPenalty, Tolerance: tol, Method: method, Now: clock.Now().Time(),
	}

	// 3. Распределение.
	progByKey := stage4.Distribute(list, toStage4Schedules(cfg.NitkaSchedule()), scfg)

	// 4. Запись результата.
	n := 0
	for i := range kept {
		r := &kept[i]
		// сброс прог-полей (в т.ч. для прибывших и не прошедших порог)
		r.ProgMsk, r.ProgJd = nil, nil
		zi, zf := 0, 0.0
		r.DelayProg, r.Mistake = &zi, &zf
		if r.Status != nil && *r.Status == 10 {
			continue
		}
		prog, ok := progByKey[r.IdDisl+"|"+r.StanNazn]
		if !ok {
			// запасной вариант эталона: беспланный без слота, но с PlanMsk → PlanMsk.
			if r.PlanMsk != nil && !time.Time(*r.PlanMsk).IsZero() {
				prog = time.Time(*r.PlanMsk)
			} else {
				continue
			}
		}
		pm := domain.LocalTime(prog)
		r.ProgMsk = &pm
		computeProgDerived(r, brosPenalty, cutoff)
		n++
	}
	return n
}

// computeProgDerived — производные от ProgMsk (перенос calculateProgJdAndDelay эталона):
// ProgJd (+сутки если час ≥ cutoff), DelayProg (ProgMsk − DateDostav, дни ≥0),
// Mistake (ProgMsk − (RaschMsk + штраф броса), дни float, ЗНАК сохраняем).
func computeProgDerived(r *domain.Dislocation, brosPenalty time.Duration, cutoff int) {
	if r.ProgMsk == nil {
		return
	}
	prog := time.Time(*r.ProgMsk)

	jd := prog
	if prog.Hour() >= cutoff {
		jd = jd.Add(24 * time.Hour)
	}
	v := domain.LocalTime(jd)
	r.ProgJd = &v

	if r.DateDostav != nil && !time.Time(*r.DateDostav).IsZero() {
		days := int(prog.Sub(time.Time(*r.DateDostav)).Hours() / 24)
		if days < 0 {
			days = 0
		}
		r.DelayProg = &days
	}

	if r.RaschMsk != nil && !time.Time(*r.RaschMsk).IsZero() {
		eff := time.Time(*r.RaschMsk)
		if r.Status != nil && *r.Status == 5 {
			eff = eff.Add(brosPenalty)
		}
		mistake := prog.Sub(eff).Hours() / 24.0
		r.Mistake = &mistake
	}
}

// resolveStationPc — по площадке (Naznach = NameS) находит станцию (пул слотов) и
// перерабатывающую способность терминала по роду груза (для формулы интервала).
// Площадка не в справочнике → пустая станция и pc 0 (поезд не спейсим, слот ищем как есть).
func resolveStationPc(dir *DirectoryCache, naznach, cargoGroup string) (station string, pc int) {
	p, ok := dir.PortByNameS(naznach)
	if !ok {
		return "", 0
	}
	return p.StationCode, pcForRod(p, cargoGroup)
}

// pcForRod — перерабатывающая способность терминала по роду груза (nil → 0).
func pcForRod(p domain.Ports, cargoGroup string) int {
	var pc *int
	switch cargoRod(cargoGroup) {
	case "coal":
		pc = p.PcCoal
	case "metal":
		pc = p.PcMetal
	default:
		pc = p.PcOther
	}
	if pc == nil {
		return 0
	}
	return *pc
}

// cargoRod — нормализованный род груза для группировки/выбора pc.
func cargoRod(cargoGroup string) string {
	switch cargoGroup {
	case "УГОЛЬ":
		return "coal"
	case "МЕТАЛЛ":
		return "metal"
	default:
		return "other"
	}
}

// localTimePtr — *domain.LocalTime → *time.Time (nil/нулевое → nil).
func localTimePtr(lt *domain.LocalTime) *time.Time {
	if lt == nil {
		return nil
	}
	t := time.Time(*lt)
	if t.IsZero() {
		return nil
	}
	return &t
}

// toStage4Schedules переводит слоты расписания в формат ядра (station → []HM).
func toStage4Schedules(sched map[string][]domain.NitkaSlot) map[string][]stage4.HM {
	out := make(map[string][]stage4.HM, len(sched))
	for st, slots := range sched {
		hm := make([]stage4.HM, len(slots))
		for i, s := range slots {
			hm[i] = stage4.HM{H: s.Hour, M: s.Minute}
		}
		out[st] = hm
	}
	return out
}
