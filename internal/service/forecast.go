package service

import (
	"time"

	"github.com/Gtport/DPmodule/internal/domain"
)

// defaultToGoHours — ход по умолчанию, если расстояние до назначения неизвестно/0
// или профиль скоростей не найден (3 суток), как в gtlogic.
const defaultToGoHours = 72.0

// applyForecast — Stage 3 (S2-5, §3.18): построчный расчёт хода до порта. Для каждой
// записи со статусом < 9 (в пути) считает ToGo (часы) по профилю route_speed → RaschMsk
// (расчётное прибытие МСК) → RaschJd (в ЖД-сутках). Статусы ≥ 9 (кандидат/прибыл/
// порожний в порту) пропускает. Возвращает число посчитанных RaschMsk. Идёт ПОСЛЕ
// донорства S2-3 (приёмник использует станцию отправления донора) и ДО подмены снимка.
func applyForecast(kept []domain.Dislocation, dir *DirectoryCache, cutoff int) int {
	if cutoff <= 0 {
		cutoff = 18
	}
	n := 0
	for i := range kept {
		r := &kept[i]
		if r.Status != nil && *r.Status >= 9 {
			continue // уже в порту / у цели — прогноз не нужен
		}
		computeToGo(r, dir)
		computeRaschMsk(r)
		computeRaschJd(r, cutoff)
		if r.RaschMsk != nil {
			n++
		}
	}
	return n
}

// computeToGo — часы хода до станции назначения по профилю скоростей route_speed.
// Расстояние nil/0 → дефолт 72 ч. Иначе суммирует время по участкам (остаток над
// границей FromKm едет на скорости участка). isBam — из маркера AlternativeMove,
// проставленного на Stage 1 по станции ОПЕРАЦИИ (текущий участок маршрута, §3.18).
func computeToGo(r *domain.Dislocation, dir *DirectoryCache) {
	if r.RasstStanNazn == nil || *r.RasstStanNazn == 0 {
		d := defaultToGoHours
		r.ToGo = &d
		return
	}
	isBam := r.AlternativeMove != 0
	segs, ok := dir.GetRouteSpeed(r.StationNach, isBam)
	if !ok || len(segs) == 0 {
		d := defaultToGoHours
		r.ToGo = &d
		return
	}
	remaining := float64(*r.RasstStanNazn)
	var total float64
	for _, seg := range segs { // по убыванию FromKm
		if seg.Speed <= 0 {
			continue
		}
		if remaining > float64(seg.FromKm) {
			total += (remaining - float64(seg.FromKm)) / seg.Speed
			remaining = float64(seg.FromKm)
		}
	}
	r.ToGo = &total
}

// computeRaschMsk — расчётное прибытие (МСК): TimeOp + ToGo + простои; +12 ч для
// статуса 0 (операционный буфер). Пустой/нулевой TimeOp → не считаем.
func computeRaschMsk(r *domain.Dislocation) {
	if r.TimeOp == nil || time.Time(*r.TimeOp).IsZero() {
		return
	}
	t := time.Time(*r.TimeOp)
	if r.ToGo != nil && *r.ToGo > 0 {
		t = t.Add(time.Duration(*r.ToGo * float64(time.Hour)))
	}
	if r.ProstDn != nil && *r.ProstDn > 0 {
		t = t.Add(time.Duration(*r.ProstDn) * 24 * time.Hour)
	}
	if r.ProstCh != nil && *r.ProstCh > 0 {
		t = t.Add(time.Duration(*r.ProstCh) * time.Hour)
	}
	if r.Status != nil && *r.Status == 0 {
		t = t.Add(12 * time.Hour)
	}
	v := domain.LocalTime(t)
	r.RaschMsk = &v
}

// computeRaschJd — RaschMsk в ЖД-сутках: +24 ч, если час ≥ cutoff (та же операционная
// граница, что и date_op_jd).
func computeRaschJd(r *domain.Dislocation, cutoff int) {
	if r.RaschMsk == nil || time.Time(*r.RaschMsk).IsZero() {
		return
	}
	t := time.Time(*r.RaschMsk)
	if t.Hour() >= cutoff {
		t = t.Add(24 * time.Hour)
	}
	v := domain.LocalTime(t)
	r.RaschJd = &v
}
