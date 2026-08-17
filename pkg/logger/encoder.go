package logger

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
)

// Колоночный текстовый энкодер: строка лога читается глазом по вертикали.
//
//	<время> · <уровень> · <component> · <направление+цель> · <сообщение> · <поля>
//
// Зачем свой, а не встроенный console-энкодер zap: тот сваливает поля в хвост
// переменной длины, и одно и то же значение каждый раз оказывается на новой
// позиции — сравнить две строки взглядом нельзя, только грепом. Фиксированные
// колонки дают и то, и другое: `grep 'dislocation'` по колонке компонента,
// `grep '→ АСУ'` по колонке цели.

// Служебные поля, которые энкодер вынимает из набора и печатает колонками, а не
// в общем хвосте. Их имена — часть контракта: в JSON-формате они остаются
// обычными полями, поэтому грепу по json доступно то же самое.
const (
	FieldComponent = "component"
	FieldDir       = "dir"
	FieldTarget    = "target"

	DirOut = "out"
	DirIn  = "in"
)

// Ширины колонок подобраны по самым длинным реальным значениям проекта:
// component — "dislocation" (11), target — "→ ЛК РЖД lk.rzd.ru" и адреса вида
// "→ АСУ 212.113.99.3:443" (30), сообщение — "источник переключён: ЛК → АСУ"
// (38). Значение длиннее колонку не режет, а раздвигает: терять смысл ради
// вида нельзя, а разъезжается при этом одна строка, не весь файл.
const (
	widthComponent = 11
	widthTarget    = 30
	widthMessage   = 38
)

var bufPool = buffer.NewPool()

// Поля читаются в порядке «кто → что → сколько стоило»: первым интересно, по
// какому клиенту событие, последним — цена и текст ошибки. Середина по алфавиту,
// чтобы одинаковые события давали одинаковый порядок полей и различались только
// значениями.
var (
	fieldsFirst = []string{"client", "user", "vagon", "req"}
	fieldsLast  = []string{"took", "waited", "error"}
)

type textEncoder struct {
	*zapcore.MapObjectEncoder
	colored bool
	loc     *time.Location
}

// newTextEncoder: colored включает ANSI-цвет уровня — только для stdout.
// В файл цвет писать нельзя: «\x1b[34mINFO\x1b[0m» ломает grep.
func newTextEncoder(colored bool, loc *time.Location) zapcore.Encoder {
	return &textEncoder{
		MapObjectEncoder: zapcore.NewMapObjectEncoder(),
		colored:          colored,
		loc:              loc,
	}
}

func (e *textEncoder) Clone() zapcore.Encoder {
	cloned := zapcore.NewMapObjectEncoder()
	for k, v := range e.Fields {
		cloned.Fields[k] = v
	}
	return &textEncoder{MapObjectEncoder: cloned, colored: e.colored, loc: e.loc}
}

func (e *textEncoder) EncodeEntry(ent zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	// Поля логгера (zap.With — например request_id) плюс поля этой записи.
	merged := zapcore.NewMapObjectEncoder()
	for k, v := range e.Fields {
		merged.Fields[k] = v
	}
	for _, f := range fields {
		f.AddTo(merged)
	}

	line := bufPool.Get()

	line.AppendString(ent.Time.In(e.loc).Format("02.01 15:04:05.000"))
	line.AppendString("  ")
	e.appendLevel(line, ent.Level)
	line.AppendString("  ")
	pad(line, takeString(merged.Fields, FieldComponent), widthComponent)
	line.AppendString("  ")
	pad(line, takeTarget(merged.Fields), widthTarget)
	line.AppendString("  ")

	// Сообщение добиваем пробелами, только если за ним что-то есть: иначе в
	// файле остаётся висячий хвост пробелов на каждой строке без полей.
	if len(merged.Fields) > 0 {
		pad(line, ent.Message, widthMessage)
		appendFields(line, merged.Fields)
	} else {
		line.AppendString(ent.Message)
	}

	// Caller нужен разработчику и мешает дежурному — оставляем только в debug.
	if ent.Level == zapcore.DebugLevel && ent.Caller.Defined {
		line.AppendString("  (")
		line.AppendString(ent.Caller.TrimmedPath())
		line.AppendString(")")
	}
	if ent.Stack != "" {
		line.AppendString("\n")
		line.AppendString(ent.Stack)
	}

	line.AppendString("\n")
	return line, nil
}

// Уровни дополнены до пяти символов, чтобы колонка компонента не гуляла.
var levelNames = map[zapcore.Level]string{
	zapcore.DebugLevel:  "DEBUG",
	zapcore.InfoLevel:   "INFO ",
	zapcore.WarnLevel:   "WARN ",
	zapcore.ErrorLevel:  "ERROR",
	zapcore.DPanicLevel: "PANIC",
	zapcore.PanicLevel:  "PANIC",
	zapcore.FatalLevel:  "FATAL",
}

var levelColors = map[zapcore.Level]string{
	zapcore.DebugLevel: "\x1b[90m",
	zapcore.InfoLevel:  "\x1b[34m",
	zapcore.WarnLevel:  "\x1b[33m",
	zapcore.ErrorLevel: "\x1b[31m",
	zapcore.FatalLevel: "\x1b[35m",
}

func (e *textEncoder) appendLevel(line *buffer.Buffer, lvl zapcore.Level) {
	name, ok := levelNames[lvl]
	if !ok {
		name = strings.ToUpper(lvl.String())
	}
	if e.colored {
		if c, ok := levelColors[lvl]; ok {
			line.AppendString(c)
			line.AppendString(name)
			line.AppendString("\x1b[0m")
			return
		}
	}
	line.AppendString(name)
}

// takeTarget собирает колонку направления: «→ АСУ 10.0.0.5:443» для исходящих,
// «← иванов 10.0.0.14» для входящих. У событий, не связанных с сетью (пересборка
// снимка, старт), цели нет и колонка пустая.
func takeTarget(fields map[string]any) string {
	target := takeString(fields, FieldTarget)
	dir := takeString(fields, FieldDir)
	if target == "" {
		return ""
	}
	switch dir {
	case DirOut:
		return "→ " + target
	case DirIn:
		return "← " + target
	default:
		return target
	}
}

func takeString(fields map[string]any, key string) string {
	v, ok := fields[key]
	if !ok {
		return ""
	}
	delete(fields, key)
	s, _ := v.(string)
	return s
}

func appendFields(line *buffer.Buffer, fields map[string]any) {
	for _, key := range orderKeys(fields) {
		line.AppendString("  ")
		line.AppendString(key)
		line.AppendString("=")
		// Объём хранится числом (в JSON-формате должен остаться числом), а
		// человеку показывается как 1.2МБ.
		if key == "bytes" {
			if n, ok := asInt(fields[key]); ok {
				line.AppendString(FormatBytes(n))
				continue
			}
		}
		line.AppendString(formatValue(fields[key]))
	}
}

// orderKeys: сначала «кто», потом по алфавиту, в конце «сколько стоило».
func orderKeys(fields map[string]any) []string {
	var first, middle, last []string
	for k := range fields {
		switch {
		case contains(fieldsFirst, k):
			first = append(first, k)
		case contains(fieldsLast, k):
			last = append(last, k)
		default:
			middle = append(middle, k)
		}
	}
	sort.Slice(first, func(i, j int) bool { return indexOf(fieldsFirst, first[i]) < indexOf(fieldsFirst, first[j]) })
	sort.Strings(middle)
	sort.Slice(last, func(i, j int) bool { return indexOf(fieldsLast, last[i]) < indexOf(fieldsLast, last[j]) })
	return append(append(first, middle...), last...)
}

func asInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case uint64:
		return int(x), true
	default:
		return 0, false
	}
}

func formatValue(v any) string {
	switch x := v.(type) {
	case string:
		if x == "" {
			return `""`
		}
		if strings.ContainsAny(x, " \t") {
			return `"` + x + `"`
		}
		return x
	case time.Duration:
		return FormatDuration(x)
	case time.Time:
		return x.Format("02.01 15:04")
	case []string:
		return strings.Join(x, ",")
	// zap.Strings/zap.Ints складываются в ObjectEncoder как []interface{} —
	// до конкретного []string дело не доходит.
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = formatValue(e)
		}
		return strings.Join(parts, ",")
	case error:
		return `"` + x.Error() + `"`
	default:
		return fmt.Sprint(v)
	}
}

// FormatDuration печатает длительность так, как её произносят вслух.
// Наносекундная точность zap.Duration («232.196248ms») читателю не нужна ни в
// одном сценарии, а по ширине это половина колонки полей.
func FormatDuration(d time.Duration) string {
	if d < 0 {
		return "-" + FormatDuration(-d)
	}
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%dмкс", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%dмс", d.Milliseconds())
	case d < time.Minute:
		if d%time.Second == 0 {
			return fmt.Sprintf("%dс", int(d.Seconds()))
		}
		return fmt.Sprintf("%.1fс", d.Seconds())
	case d < time.Hour:
		if s := int(d.Seconds()) % 60; s != 0 {
			return fmt.Sprintf("%dм %dс", int(d.Minutes()), s)
		}
		return fmt.Sprintf("%dм", int(d.Minutes()))
	default:
		if m := int(d.Minutes()) % 60; m != 0 {
			return fmt.Sprintf("%dч %dм", int(d.Hours()), m)
		}
		return fmt.Sprintf("%dч", int(d.Hours()))
	}
}

// FormatBytes — объём человеку: «1.2МБ» вместо «1258291».
func FormatBytes(n int) string {
	switch {
	case n < 0:
		return "-" + FormatBytes(-n)
	case n >= 1<<20:
		return fmt.Sprintf("%.1fМБ", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0fКБ", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%dБ", n)
	}
}

// pad дополняет колонку пробелами ПО ЧИСЛУ РУН, а не байтов: у нас весь лог
// русский, в UTF-8 кириллица занимает два байта, и выравнивание по len()
// разъедется на каждой строке.
func pad(buf *buffer.Buffer, s string, width int) {
	buf.AppendString(s)
	if n := width - utf8.RuneCountInString(s); n > 0 {
		buf.AppendString(strings.Repeat(" ", n))
	}
}

func contains(list []string, s string) bool { return indexOf(list, s) >= 0 }

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}
