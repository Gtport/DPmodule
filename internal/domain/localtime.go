package domain

import (
	"database/sql/driver"
	"fmt"
	"strings"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
//  LocalTime — время без метки часового пояса (без Z)
// ─────────────────────────────────────────────────────────────────────────────

// localTimeLayout — единый формат времени GTport: без суффикса Z и без смещения.
const localTimeLayout = "2006-01-02T15:04:05"

// LocalTime — обёртка над time.Time, которая в JSON и БД представляется БЕЗ метки
// часового пояса. В JSON сериализуется как "2006-01-02T15:04:05" (nil-указатель →
// null, нулевое время → null). В PostgreSQL пишется в колонку timestamp (without
// time zone) через driver.Valuer. Для арифметики/сравнений используйте .Time().
//
// Канон проекта: единая шкала АСУ, все даты берём/отдаём как пришли, без перевода
// зон и без Z (см. PROJECT_INSTRUCTIONS.md, раздел про время).
type LocalTime time.Time

// Time возвращает обычный time.Time (для .Add/.Sub/.Before/.Format и т.п.).
func (lt LocalTime) Time() time.Time { return time.Time(lt) }

// IsZero — обёртка над time.Time.IsZero.
func (lt LocalTime) IsZero() bool { return time.Time(lt).IsZero() }

// String — представление в локальном формате без Z.
func (lt LocalTime) String() string { return time.Time(lt).Format(localTimeLayout) }

// NewLocalTime создаёт *LocalTime из time.Time.
func NewLocalTime(t time.Time) *LocalTime { lt := LocalTime(t); return &lt }

// FromTimePtr конвертирует *time.Time → *LocalTime (nil → nil).
func FromTimePtr(t *time.Time) *LocalTime {
	if t == nil {
		return nil
	}
	lt := LocalTime(*t)
	return &lt
}

// TimePtr конвертирует *LocalTime → *time.Time (nil → nil).
func (lt *LocalTime) TimePtr() *time.Time {
	if lt == nil {
		return nil
	}
	t := time.Time(*lt)
	return &t
}

// MarshalJSON — формат без Z; нулевое время → null.
func (lt LocalTime) MarshalJSON() ([]byte, error) {
	t := time.Time(lt)
	if t.IsZero() {
		return []byte("null"), nil
	}
	return []byte(`"` + t.Format(localTimeLayout) + `"`), nil
}

// UnmarshalJSON принимает и с Z, и без Z, и с миллисекундами, и дату без времени.
func (lt *LocalTime) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	if s == "" || s == "null" {
		*lt = LocalTime(time.Time{})
		return nil
	}
	for _, layout := range []string{
		localTimeLayout,
		"2006-01-02T15:04:05.000",
		"2006-01-02T15:04:05.999999999",
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02 15:04:05",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			*lt = LocalTime(t)
			return nil
		}
	}
	return fmt.Errorf("LocalTime: не удалось разобрать время %q", s)
}

// Value — запись в БД как time.Time (колонка timestamp без таймзоны).
func (lt LocalTime) Value() (driver.Value, error) {
	t := time.Time(lt)
	if t.IsZero() {
		return nil, nil
	}
	return t, nil
}

// Scan — чтение из БД. NULL → нулевое значение (для *LocalTime database/sql
// сам выставит nil-указатель при NULL).
func (lt *LocalTime) Scan(v interface{}) error {
	if v == nil {
		*lt = LocalTime(time.Time{})
		return nil
	}
	switch x := v.(type) {
	case time.Time:
		*lt = LocalTime(x)
		return nil
	case []byte:
		return lt.UnmarshalJSON(append(append([]byte(`"`), x...), '"'))
	case string:
		return lt.UnmarshalJSON([]byte(`"` + x + `"`))
	default:
		return fmt.Errorf("LocalTime: неподдерживаемый тип %T", v)
	}
}
