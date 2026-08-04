// Package unloadsim — симуляция выгрузки терминала по суткам («прогноз ГТ»).
//
// Порт эталона gtlogic client/src/components/gt/simulation.ts (чистые функции,
// считались в браузере). Поведение закреплено golden-фикстурами в testdata/,
// сгенерированными прогоном самого эталона (scripts/golden/gen_gtsim_fixtures.mts).
//
// Все времена — московские naive (без часового пояса), как везде в DPmodule.
// Пакет не ходит в БД и не смотрит на часы: все входы — параметрами.
package unloadsim

import "time"

// Симуляция считает в «расчётной шкале»: ЖД-сутки 18:00→18:00 сдвинуты так,
// чтобы совпасть с календарными сутками 00:00→24:00. ЖД-времена (prog_jd и
// прочие *_jd) уже несут правило «час ≥ 18 → +1 сутки», поэтому сдвиг не
// трогает дату: 19:41 → 01:41 тех же суток, 03:00 → 09:00 тех же суток.
//
// Эталон обнуляет миллисекунды при каждом сдвиге (setUTCHours(..., 0)) —
// сохраняем это поведение (усечение до секунды).

// RailwayToCalc переводит ЖД-время в расчётную шкалу: ≥18:00 → −18ч, иначе +6ч.
func RailwayToCalc(t time.Time) time.Time {
	t = t.Truncate(time.Second)
	if t.Hour() >= 18 {
		return t.Add(-18 * time.Hour)
	}
	return t.Add(6 * time.Hour)
}

// CalcToRailway — обратный сдвиг: <06:00 → +18ч, иначе −6ч.
func CalcToRailway(t time.Time) time.Time {
	t = t.Truncate(time.Second)
	if t.Hour() < 6 {
		return t.Add(18 * time.Hour)
	}
	return t.Add(-6 * time.Hour)
}
