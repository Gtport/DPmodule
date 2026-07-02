// Package clock — единственный источник «сейчас» в приложении.
//
// Инвариант проекта (TARGET.md §3.11, CLAUDE.md): всё время — Московское без
// часового пояса. Это ЕДИНСТВЕННОЕ место, где допустимо обращение к часовому
// поясу; остальной код TZ-free и не зависит от часового пояса сервера (VPS во
// Франкфурте ≠ Москва). Now() отдаёт московские настенные часы как naive
// LocalTime — в той же «нейтральной» форме (носитель time.UTC), что даёт
// time.Parse без зоны, поэтому значения clock.Now() и распарсенных дат
// однородны в JSON и в БД.
package clock

import (
	"time"

	"github.com/Gtport/DPmodule/internal/domain"
)

// moscow — местоположение «Europe/Moscow», загружается один раз. При отсутствии
// tzdata в системе — фиксированный UTC+3 (Москва без перехода на летнее время с
// 2014 г.), чтобы приложение не зависело от наличия базы часовых поясов.
var moscow = loadMoscow()

func loadMoscow() *time.Location {
	if loc, err := time.LoadLocation("Europe/Moscow"); err == nil {
		return loc
	}
	return time.FixedZone("MSK", 3*60*60)
}

// nowFn — точка подмены времени в тестах (см. SetForTest). По умолчанию —
// системные московские настенные часы в naive-форме.
var nowFn = systemNow

// Now возвращает текущее московское время как LocalTime (без часового пояса).
// Использовать вместо time.Now() везде, где нужен «сейчас»: штампы
// CreatedAt/UpdatedAt, проверки свежести/устаревания, «сегодня» и т.п.
func Now() domain.LocalTime {
	return domain.LocalTime(nowFn())
}

// systemNow берёт московские настенные часы и пересобирает их как naive-время:
// те же настенные значения, но нейтральный носитель time.UTC — идентично тому,
// что возвращает time.Parse без указания зоны. Так шкала едина и без Z.
func systemNow() time.Time {
	m := time.Now().In(moscow)
	return time.Date(m.Year(), m.Month(), m.Day(), m.Hour(), m.Minute(), m.Second(), m.Nanosecond(), time.UTC)
}

// SetForTest фиксирует «сейчас» указанным значением и возвращает функцию отката.
// Только для тестов. Значение трактуется как московское naive-время.
func SetForTest(t time.Time) (restore func()) {
	prev := nowFn
	nowFn = func() time.Time { return t }
	return func() { nowFn = prev }
}
