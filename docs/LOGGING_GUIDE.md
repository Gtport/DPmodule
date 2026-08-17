# Человекочитаемые логи в Go-микросервисе

Практическое руководство: как привести логи сервиса к виду, который читает
дежурный, а не только автор кода. Написано по итогам переработки боевого
сервиса на Go + zap, где 1298 строк за 13 часов ужались до ~180 при том, что
информации стало больше.

Всё ниже применимо к любому сервису на `go.uber.org/zap`. Код можно копировать
как есть; места, требующие адаптации, помечены.

---

## 1. Как понять, что руководство про вас

Признаки, по которым видно, что лог пишется «для компилятора»:

- **Одинаковые строки идут пачками.** Внешний источник лёг — и каждый цикл
  поллера пишет одно и то же по каждому клиенту. За сутки простоя это сотни
  строк с нулевой информацией.
- **Успешные запросы занимают половину объёма.** Поллер фронта ходит каждые
  5 минут, каждый заход — строка INFO с полным UUID запроса.
- **В логе нет исходящих вызовов.** Пока внешняя система жива, следов
  обращений к ней нет; когда падает — узнаёте из текста чужой обёрнутой ошибки.
- **Префикс подсистемы пишется по-разному** в каждом файле: `pps:`, `gu2b:`,
  `dislocation ` (без двоеточия), `getInvoice`, — и грепом не собирается.
- **Есть сообщения `ok`, `done`, `failed`** без подлежащего.
- **Длительности с наносекундами**: `232.196248ms`, `1.840651494s`.
- **Формат зависит от `env`**: в dev — console, в prod — JSON, хотя читают
  их одни и те же люди.

### Замер за пять минут

```bash
# Топ повторяющихся сообщений
sed -E 's/^[^ \t]+[ \t]+([A-Z]+)[ \t]+([^ \t]+)[ \t]+([^{]*).*$/\1 | \3/' app.log \
  | sed -E 's/[ \t]+$//' | sort | uniq -c | sort -rn | head -20

# Доля объёма по категориям
awk '{ n=length($0); s+=n; if (/<подстрока категории>/) c+=n }
     END { printf "всего %d Б; категория %d Б (%d%%)\n", s, c, c*100/s }' app.log
```

Если верхние три строки дают больше половины объёма — дальше по тексту.

---

## 2. Формат строки

```
<время> · <уровень> · <component> · <направление+цель> · <сообщение> · <поля>
```

```
15:20:00.340  INFO   dislocation  → шлюз АСУ 10.0.0.5:50049  дислокация получена   client=attis bytes=1.2МБ took=340мс
15:20:00.572  INFO   http         ← attis-ui 10.0.0.14      запрос обработан      method=GET path=/api/v1/x status=200 took=232мс
17:40:12.001  INFO   dislocation                            источник восстановлен: ЛК → АСУ  простой=10ч
```

Смысл фиксированных колонок один: **глаз идёт по вертикали**. У стандартного
console-энкодера zap поля уезжают в JSON-хвост переменной длины, и значение
каждый раз оказывается на новой позиции — читать такое можно только грепом.

### Словарь `component`

Заведите закрытый список предметных областей. Пример из реального сервиса:

```
dislocation · pps · gu2b · invoice · history · http · proxy · worker · auth · startup · db
```

**Ключевое правило: `component` — это то, что мы делаем, а не то, куда ходим.**
Если один и тот же бизнес-процесс умеет ходить в два источника (основной и
резервный), у него один `component`, а источник живёт в колонке цели. Иначе
цепочка одной операции размазывается по двум «компонентам» и не собирается
грепом.

### Направление и цель

`→` исходящий, `←` входящий. В цели — **и имя, и адрес**: имя чтобы читать,
адрес чтобы понимать, в какой контур ушёл запрос.

```
→ шлюз АСУ 10.0.0.5:50049
→ ЛК РЖД cargolk.rzd.ru
← attis-ui 10.0.0.14
← аноним 203.0.113.9
```

### Уровни

| уровень | когда |
|---|---|
| `DEBUG` | подробности для разбора: дедуп, пустые ответы, веерные вызовы |
| `INFO` | штатные события: получено, записано, переключено, запущено |
| `WARN` | штатно обработанное отклонение: источник недоступен, пусто, 4xx |
| `ERROR` | потеря данных или неработающая функция: не записалось в БД, паника |
| `FATAL` | не стартовали |

Отдельно сверьтесь, что одинаковые по смыслу события имеют одинаковый уровень.
В исходном сервисе провал пробы источника был `INFO`, а провал обычного забора
— `WARN`, хотя причина одна и та же.

---

## 3. Девять правил

**П1. Смена состояния, а не итерация.** Переход пишется всегда, повтор
состояния — никогда.

**П2. Пульс раз в час, пока состояние держится.** Отвечает на вопрос «оно живое
и знает, что лежит?».

**П3. Пустой цикл не пишем.** `cycle done` с нулевым итогом молчит.

**П4. Ретраи — каждую попытку, но только для попыток сделать работу.**
Периодическая проба живости в состоянии «источник лежит» — это health-check, а
не работа: она уходит в пульс. Без этой границы простой в 13 часов даёт больше
строк, чем было до правки.

**П5. Ошибка — словами, сырьё в поле.** В сообщении `соединение отклонено`,
полный текст — в поле `error`.

**П6. Медленный вызов — две строки** (старт и финиш с `waited=`), быстрый —
одна по факту завершения с `took=`.

**П7. `X failed` → утверждение о результате.** «марки угля не записаны», а не
«вставка марок провалилась». Читателю важно, чего теперь нет в базе.

**П8. Крупные списки — отпечатком.** Вместо 136 имён колонок — `схема=a3f1c2`;
полный список только при изменении и сразу с дельтой.

**П9. Секреты не логируем.** Хосты, имена баз, метки ключей — да. Пароли, DSN
целиком, подставленные значения из vault — нет.

---

## 4. Энкодер

Полностью переносимый файл. Меняйте `msk` под свой часовой пояс и ширины
колонок под свои значения.

```go
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

const (
	FieldComponent = "component"
	FieldDir       = "dir"
	FieldTarget    = "target"

	DirOut = "out"
	DirIn  = "in"
)

// АДАПТИРОВАТЬ: ширины под самые длинные реальные значения.
const (
	widthComponent = 11
	widthTarget    = 30
	widthMessage   = 38
)

// АДАПТИРОВАТЬ: свой пояс. Фиксированная зона, а не LoadLocation: в контейнере
// может не быть tzdata, и LoadLocation тогда МОЛЧА вернёт UTC — время в логе
// уедет, и никто не заметит.
var msk = time.FixedZone("MSK", 3*60*60)

var bufPool = buffer.NewPool()

// Поля, которые читаются первыми (кто) и последними (сколько стоило).
var (
	fieldsFirst = []string{"client", "user", "ip"}
	fieldsLast  = []string{"took", "waited", "error"}
)

type textEncoder struct {
	*zapcore.MapObjectEncoder
	colored bool
}

// newTextEncoder: colored включает ANSI-цвет уровня — только для stdout.
// Файл и SSE-поток должны получать чистый текст.
func newTextEncoder(colored bool) zapcore.Encoder {
	return &textEncoder{MapObjectEncoder: zapcore.NewMapObjectEncoder(), colored: colored}
}

func (e *textEncoder) Clone() zapcore.Encoder {
	cloned := zapcore.NewMapObjectEncoder()
	for k, v := range e.Fields {
		cloned.Fields[k] = v
	}
	return &textEncoder{MapObjectEncoder: cloned, colored: e.colored}
}

func (e *textEncoder) EncodeEntry(ent zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	// Поля логгера (zap.With) плюс поля этой записи.
	merged := zapcore.NewMapObjectEncoder()
	for k, v := range e.Fields {
		merged.Fields[k] = v
	}
	for _, f := range fields {
		f.AddTo(merged)
	}

	line := bufPool.Get()

	line.AppendString(ent.Time.In(msk).Format("02.01 15:04:05.000"))
	line.AppendString("  ")
	e.appendLevel(line, ent.Level)
	line.AppendString("  ")
	pad(line, takeString(merged.Fields, FieldComponent), widthComponent)
	line.AppendString("  ")
	pad(line, takeTarget(merged.Fields), widthTarget)
	line.AppendString("  ")

	// Сообщение выравниваем, только если за ним что-то есть: иначе в файле
	// остаётся хвост из пробелов на каждой строке без полей.
	if len(merged.Fields) > 0 {
		pad(line, ent.Message, widthMessage)
		appendFields(line, merged.Fields)
	} else {
		line.AppendString(ent.Message)
	}

	// Caller нужен разработчику и мешает дежурному.
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

// takeTarget собирает колонку направления. Без target колонка пустая — у
// событий, не связанных с сетью, её нет.
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
		// Объём хранится числом (в json должен остаться числом), человеку
		// показывается как 1.2МБ.
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
	default:
		return 0, false
	}
}

func formatValue(v any) string {
	switch x := v.(type) {
	case string:
		if strings.ContainsAny(x, " \t") {
			return `"` + x + `"`
		}
		return x
	case time.Duration:
		return FormatDuration(x)
	case time.Time:
		return x.In(msk).Format("15:04")
	case []string:
		return strings.Join(x, ",")
	// zap.Strings/zap.Ints складываются в ObjectEncoder как []interface{},
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
// Наносекундная точность zap.Duration читателю не нужна ни в одном сценарии.
func FormatDuration(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%.1fмкс", float64(d.Microseconds()))
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

func FormatBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fМБ", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0fКБ", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%dБ", n)
	}
}

// pad дополняет колонку пробелами ПО ЧИСЛУ РУН, а не байтов: в UTF-8 кириллица
// занимает два байта, и выравнивание по len() разъедется.
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
```

### Подключение

```go
const (
	FormatText = "text" // колонки для человека (по умолчанию)
	FormatJSON = "json" // для сборщика
)

func buildEncoder(format string, colored bool) zapcore.Encoder {
	if format == FormatJSON {
		return zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig())
	}
	return newTextEncoder(colored)
}

// stdout — цветной, файл и прочие приёмники — чистый текст.
cores := []zapcore.Core{
	zapcore.NewCore(buildEncoder(cfg.Format, true), zapcore.AddSync(os.Stdout), level),
	zapcore.NewCore(buildEncoder(cfg.Format, false), zapcore.AddSync(fileWriter), level),
}
```

**Формат задаётся отдельным полем конфига (`log.format`), а не выводится из
`app.env`.** Оба энкодера пишут один и тот же набор полей, поэтому переключение
на json для сборщика ничего не теряет — а зависимость от `env` означает, что
смена окружения незаметно меняет формат логов, которые читают одни и те же люди.

---

## 5. Паттерн: логирование исходящих вызовов

Типичная дыра: адаптеры к внешним системам молчат и только возвращают
обёрнутую ошибку наверх. Общий помощник на пакет адаптеров:

```go
type callLog struct {
	log       *zap.Logger
	component string // предметная область
	target    string // "шлюз АСУ 10.0.0.5:50049"
	gate      *failGate
}

func newCallLog(log *zap.Logger, component, name, baseURL string) callLog {
	if log == nil {
		log = zap.NewNop()
	}
	return callLog{log: log, component: component,
		target: name + " " + hostOf(baseURL),
		gate:   &failGate{seen: map[string]*failRun{}}}
}

// WithLogger — отдельный сеттер, а не параметр конструктора: подключать логгер
// нужно только в бою, а тесты создают адаптер прежним вызовом.
func (h *HTTPFetcher) WithLogger(log *zap.Logger) *HTTPFetcher {
	h.call = h.call.withLogger(log)
	return h
}

func (c callLog) success(msg string, start time.Time, size int, extra ...zap.Field) {
	c.gate.reset()
	c.log.Info(msg, c.fields(start, append(extra, zap.Int("bytes", size))...)...)
}

// successQuiet — для ВЕЕРНЫХ вызовов (документ за документом, объект за
// объектом): успех в DEBUG, иначе один цикл даёт сотни строк INFO. Итог веера
// сервис пишет одной строкой. Отказы остаются в WARN.
func (c callLog) successQuiet(msg string, start time.Time, size int, extra ...zap.Field) {
	c.gate.reset()
	c.log.Debug(msg, c.fields(start, append(extra, zap.Int("bytes", size))...)...)
}

func (c callLog) failure(start time.Time, err error, extra ...zap.Field) {
	msg := reason(err)

	// Ключ включает клиента и номер попытки: адаптер один на всех клиентов, и
	// отказ по второму не должен потеряться за отказом по первому; ретраи
	// внутри одного вызова различаются номером и глушиться не должны.
	key := msg
	for _, f := range extra {
		if f.Key == "client" || f.Key == "attempt" {
			key += "|" + f.String
		}
	}
	write, suppressed := c.gate.allow(key)
	if !write {
		return
	}
	if suppressed > 0 {
		extra = append(extra, zap.Int("повторов_за_час", suppressed))
	}
	c.log.Warn(msg, c.fields(start, append(extra, zap.String("error", err.Error()))...)...)
}

func (c callLog) fields(start time.Time, extra ...zap.Field) []zap.Field {
	out := []zap.Field{
		zap.String(logger.FieldComponent, c.component),
		zap.String(logger.FieldDir, logger.DirOut),
		zap.String(logger.FieldTarget, c.target),
		zap.Duration("took", time.Since(start)),
	}
	return append(out, extra...)
}
```

### Глушилка повторов — обязательна

Без неё правка делает лог **хуже**. Если поллер дёргает внешнюю систему каждый
цикл, а она лежит сутками, транспортный слой напишет строку на каждый цикл по
каждому клиенту.

```go
const repeatWindow = time.Hour

type failGate struct {
	mu   sync.Mutex
	seen map[string]*failRun
}

type failRun struct {
	lastLogged time.Time
	count      int
}

// allow: писать ли отказ и сколько таких же подавлено.
func (g *failGate) allow(key string) (bool, int) {
	g.mu.Lock()
	defer g.mu.Unlock()

	run, ok := g.seen[key]
	if !ok {
		g.seen[key] = &failRun{lastLogged: time.Now()}
		return true, 0
	}
	run.count++
	if time.Since(run.lastLogged) < repeatWindow {
		return false, run.count
	}
	suppressed := run.count
	run.lastLogged, run.count = time.Now(), 0
	return true, suppressed
}

// reset: после успеха серия начинается заново — следующий сбой снова заметен
// немедленно.
func (g *failGate) reset() {
	g.mu.Lock()
	clear(g.seen)
	g.mu.Unlock()
}
```

### Причины по-человечески

```go
func reason(err error) string {
	switch {
	case err == nil:
		return "вызов не удался"
	case errors.Is(err, context.Canceled):
		return "вызов отменён"
	case errors.Is(err, context.DeadlineExceeded):
		return "таймаут"
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "Client.Timeout"), strings.Contains(s, "i/o timeout"):
		return "таймаут"
	case strings.Contains(s, "connection refused"):
		return "соединение отклонено"
	case strings.Contains(s, "connection reset"):
		return "соединение разорвано"
	case strings.Contains(s, "no such host"):
		return "хост не найден"
	case strings.Contains(s, "network is unreachable"), strings.Contains(s, "no route to host"):
		return "сеть недоступна"
	case strings.Contains(s, "x509"), strings.Contains(s, "certificate"):
		return "сертификат не принят"
	default:
		return "вызов не удался"
	}
}

// hostOf оставляет host:port — контур важнее пути.
func hostOf(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return strings.TrimRight(baseURL, "/")
	}
	return u.Host
}
```

---

## 6. Паттерн: трекер состояния источника

Закрывает П1 и П2. Подходит любому сервису с фолбэком, circuit breaker'ом или
переключением между источниками.

```go
type sourceMode int

const (
	modeUnknown sourceMode = iota
	modePrimary
	modeFallback
	modePaused
)

const pulseInterval = time.Hour

// onTransition вызывается на каждой смене состояния — на неё вешаются
// уведомления в мессенджер. Отдельного счётчика «уже сообщили» у бота быть
// НЕ должно: и лог, и бот питаются от одного состояния и не могут разойтись.
type onTransition func(client string, to sourceMode, downtime time.Duration)

type sourceTracker struct {
	log       *zap.Logger
	component string
	notify    onTransition

	mu     sync.Mutex
	states map[string]*clientSource // ключ "" = состояние общее на всех
}

type clientSource struct {
	mode      sourceMode
	since     time.Time
	lastPulse time.Time
}

func (t *sourceTracker) toFallback(client string, streak, threshold int) {
	t.transition(client, modeFallback, zapcore.WarnLevel,
		"источник переключён: основной → резервный", 0,
		zap.String("причина", fmt.Sprintf("порог %d неудач подряд", threshold)),
		zap.Int("неудач", streak))
}

func (t *sourceTracker) toPrimary(client string) {
	t.mu.Lock()
	st := t.state(client)
	var downtime time.Duration
	if st.mode == modeFallback || st.mode == modePaused {
		downtime = time.Since(st.since)
	}
	first := st.mode == modeUnknown
	t.mu.Unlock()

	// Первый успех после старта — не «восстановление», просто работа.
	if first {
		t.setMode(client, modePrimary)
		return
	}
	t.transition(client, modePrimary, zapcore.InfoLevel,
		"источник восстановлен: резервный → основной", downtime,
		zap.Duration("простой", downtime))
}

// pulse напоминает раз в час, что состояние всё ещё то же.
func (t *sourceTracker) pulse(client string, fails int, nextFetch time.Time) {
	t.mu.Lock()
	st := t.state(client)
	if st.mode != modeFallback && st.mode != modePaused {
		t.mu.Unlock()
		return
	}
	if !st.lastPulse.IsZero() && time.Since(st.lastPulse) < pulseInterval {
		t.mu.Unlock()
		return
	}
	st.lastPulse = time.Now()
	down := time.Since(st.since)
	t.mu.Unlock()

	fields := t.fields(client)
	if fails > 0 {
		fields = append(fields, zap.Int("неудач", fails))
	}
	if !nextFetch.IsZero() {
		fields = append(fields, zap.Time("следующий_забор", nextFetch))
	}
	t.log.Info(fmt.Sprintf("основной источник недоступен %s, работаем от резервного",
		logger.FormatDuration(down)), fields...)
}

// transition пишет строку, только если состояние ДЕЙСТВИТЕЛЬНО сменилось.
func (t *sourceTracker) transition(client string, mode sourceMode, level zapcore.Level,
	msg string, downtime time.Duration, extra ...zap.Field) {

	t.mu.Lock()
	st := t.state(client)
	if st.mode == mode {
		t.mu.Unlock()
		return
	}
	st.mode, st.since, st.lastPulse = mode, time.Now(), time.Time{}
	t.mu.Unlock()

	if ce := t.log.Check(level, msg); ce != nil {
		ce.Write(append(t.fields(client), extra...)...)
	}
	if t.notify != nil {
		t.notify(client, mode, downtime)
	}
}
```

### Что удаляется на стороне сервиса

- `X: LK throttled` на каждой итерации → пульс с полем `следующий_забор=`
- `X: probe failed, staying on fallback` → пульс
- `X: circuit open, paused` на каждой итерации → один переход `toPaused` + пульс
- `X: cycle done {total: 0}` → молчит при нуле
- `X: ASU failed` на уровне сервиса → его уже пишет транспорт с причиной и
  адресом; на уровне сервиса остаётся только переход через порог

### Если в сервисе есть алерты в мессенджер

Часто рядом заводится второй счётчик вида `alreadyNotified map[string]bool`.
Это второе хранилище одного факта: со временем лог и бот разойдутся. Вешайте
уведомления на `onTransition` — одно состояние, один источник правды, и бот
бесплатно получает длительность простоя.

---

## 7. Паттерн: входящие запросы с личностью вызывающего

Обычно middleware логирует метод, путь, статус и латентность — но не то, **кто
пришёл**, хотя личность уже лежит в контексте после auth-middleware.

```go
func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		// Клеймы читаем ПОСЛЕ обработки: их кладёт auth-middleware ниже по цепочке.
		claims := auth.ClaimsFromContext(c.Request.Context())
		status := c.Writer.Status()

		fields := []zap.Field{
			zap.String(logger.FieldComponent, "http"),
			zap.String(logger.FieldDir, logger.DirIn),
			zap.String(logger.FieldTarget, claims.Caller()+" "+c.ClientIP()),
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.Int("status", status),
			zap.String("req", shortRequestID(c)), // 8 символов, не полный UUID
			zap.Duration("took", time.Since(start)),
		}
		if reason := RejectReason(c); reason != "" {
			fields = append(fields, zap.String("причина", reason))
		}

		log := logger.FromContext(c.Request.Context())
		if status >= 400 {
			log.Warn("запрос отклонён", fields...)
			return
		}
		log.Info("запрос обработан", fields...)
	}
}
```

Две правки, дающие больше, чем выглядит:

**Причина отказа переезжает в http-строку.** Обычно отвергнутый запрос даёт две
записи, причём первая — в `DEBUG`, то есть в проде виден голый 401 без
объяснения. Складывайте причину в контекст (`c.Set`) и печатайте полем: одно
событие — одна строка, а видимость наоборот растёт.

**`request_id` укорачивается до 8 символов.** Полный UUID — 36 знаков на каждой
строке; связать строки внутри файла хватает восьми. Полное значение уходит в
заголовок ответа и в json-формат.

---

## 8. Стартовый блок

Пишется один раз, читается при разборе «с какими настройками оно поднялось».
Здесь важнее полнота, чем краткость. Единый шаблон `подсистема: состояние + параметры`:

```
12.08 15:00:00.000  INFO  startup  myservice 1.4.2 (a3f1c2, собран 10.08.2026 14:22)  env=prod
12.08 15:00:00.001  INFO  startup  логи: уровень=info формат=text файл=/var/log/app.log (100МБ × 5, 30 дней)
12.08 15:00:00.140  INFO  startup  postgres: подключён  host=10.0.0.5:5432  db=main
12.08 15:00:00.141  INFO  startup  redis: выключен
12.08 15:00:00.152  INFO  startup  шлюз: 10.0.0.5:50049  таймаут=120с
12.08 15:00:00.201  INFO  startup  готов: слушаю :8080  TLS=да
```

Три вещи, которых обычно нет, а нужны они первыми:

1. **Версия и сборка.** Первый вопрос при инциденте — какой билд крутится.
   ```go
   var (
       version   = "dev"
       commit    = "неизвестно"
       buildTime = "неизвестно"
   )
   ```
   ```makefile
   VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
   COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
   BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
   LDFLAGS    := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildTime=$(BUILD_TIME)

   build:
   	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/server/...
   ```
   Без `-ldflags` строка выглядит осмысленной, ничего не сообщая — заведите
   сразу и Makefile, и Dockerfile (`ARG VERSION` + `--build-arg` из CI).

2. **Адреса подключений.** `postgres connected` без host и db бесполезно на
   сервисе, у которого есть prod и dev.

3. **Конфигурация логгера.** Без неё нельзя отличить «ничего не происходило» от
   «уровень скрыл всё».

---

## 9. Порядок внедрения

1. **Энкодер** — первым. Пока не зафиксированы колонки, любую формулировку
   придётся переписывать дважды.
2. **Исходящие вызовы** — самостоятельная ценность, ни от чего не зависят.
   Сразу с глушилкой повторов.
3. **Машина состояний** — основной выигрыш по объёму.
4. **Входящие + личность вызывающего.**
5. **Механические замены** остальных сообщений.
6. **Стартовый блок.**

Перед началом полезно составить каталог: все точки логирования с указанием
файла, строки, уровня и частоты в образце. Дальше по нему идти и вычёркивать.

```bash
grep -rn '\.\(Info\|Warn\|Error\|Debug\|Fatal\)("[^"]*"' --include=*.go . | grep -v _test.go
```

Учтите, что этот греп **не поймает** формы вида
`log.Check(lvl, "...")` и `logger.FromContext(c.Request.Context()).Warn(...)`
— проверьте их отдельно, иначе несколько точек останутся неприведёнными.

---

## 10. Грабли

**Выравнивание по байтам.** `len(s)` на кириллице даёт вдвое больше символов, и
колонки разъезжаются на каждой русской строке. Только `utf8.RuneCountInString`.
Заведите на это тест.

**`time.LoadLocation` в контейнере.** Без tzdata молча вернёт UTC — время в логе
уедет, и никто не заметит. Фиксированная зона надёжнее.

**Двойное логирование транспорт/сервис.** Добавив запись вызовов в адаптеры,
пройдитесь по сервисам и удалите их собственные «не удалось получить X» — иначе
каждый сбой даст две строки.

**Поллер, который дёргает источник каждый цикл.** Проверьте, как часто адаптер
реально вызывается в состоянии «источник лежит». Если каждый цикл — без
глушилки повторов вы получите больше строк, чем было.

**Висячие пробелы.** Выравнивание сообщения добивает пробелами даже там, где
полей нет. В файле это мусор на каждой второй строке — добивайте только когда
за колонкой что-то есть.

**nil-guard на выключенной функциональности.** Если что-то включается флагом и
представлено nil-указателем, проверку на nil нужно поставить во ВСЕХ методах, а
не только в тех, что вызываются из горячего пути. У меня периодический
`flush()` такой проверки не имел — выключенный аудит уронил бы процесс на
первом же часовом тике. Поймал тест, а не прод.

**Запуск, а не только сборка.** Два дефекта (висячие пробелы и заглушка вместо
версии) не видны ни компилятору, ни тестам — только глазами на реальном выводе.
Поднимите сервис с минимальным конфигом и посмотрите стартовый блок.

---

## 11. Чек-лист

- [ ] Формат задан полем конфига, а не выведен из `env`
- [ ] `log.format: text|json`, оба пишут одинаковые поля
- [ ] Словарь `component` закрытый, префиксы из текстов сообщений убраны
- [ ] `component` = предметная область, адресат = отдельная колонка
- [ ] Выравнивание по рунам, есть тест
- [ ] Часовой пояс фиксированной зоной
- [ ] Длительности округлены, объёмы человекочитаемы
- [ ] Исходящие вызовы логируются, с именем и адресом цели
- [ ] Глушилка повторяющихся отказов на месте
- [ ] Причины ошибок словами, сырьё в поле `error`
- [ ] Веерные вызовы в DEBUG, их итог — одной строкой в INFO
- [ ] Переходы состояния пишутся один раз, повтор — пульсом раз в час
- [ ] Пустые циклы молчат
- [ ] Уведомления в мессенджер питаются от того же состояния, что лог
- [ ] В http-строке есть личность вызывающего
- [ ] Причина отказа авторизации в http-строке, а не отдельной DEBUG-записью
- [ ] `request_id` укорочен
- [ ] Паники пишутся со стеком
- [ ] Стартовый блок: версия, адреса, конфигурация логгера
- [ ] `-ldflags` прописаны в Makefile и Dockerfile
- [ ] Секретов в логах нет
- [ ] Сервис поднят и стартовый блок просмотрен глазами
