package logger

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// CallLog — запись исходящих вызовов, общая на все адаптеры внешних систем
// (АСУ, ЛК РЖД, памятки, Keycloak, MAX).
//
// Зачем один помощник на всех: до него адаптеры молчали и только возвращали
// наверх обёрнутую ошибку. Пока провайдер жив, следов обращений к нему в логе
// не было вовсе; когда падал — в файле оказывался чужой текст вроде
// «Get "https://…": dial tcp: connect: connection refused», без ответа на
// вопрос «в какой контур мы вообще ходили».
//
// Ноль-значение бесполезно — собирать через NewCallLog.
type CallLog struct {
	log       *zap.Logger
	component string // предметная область: ЧТО мы делаем (не куда ходим)
	target    string // «ЛК РЖД cargolk.rzd.ru» — имя читать, адрес различать контур
	gate      *failGate
}

// NewCallLog собирает помощник. name — имя системы для человека, baseURL — её
// адрес, из него берётся host:port.
//
// component — предметная ОБЛАСТЬ, а не имя системы: у забора дислокации из АСУ
// и из ЛК компонент один (CompDislocation), различаются они колонкой цели.
// Иначе одна операция размазывается по двум «компонентам» и не собирается
// грепом.
func NewCallLog(log *zap.Logger, component, name, baseURL string) CallLog {
	if log == nil {
		log = zap.NewNop()
	}
	target := name
	if h := hostOf(baseURL); h != "" {
		target = name + " " + h
	}
	return CallLog{
		log:       log,
		component: component,
		target:    target,
		gate:      &failGate{seen: map[string]*failRun{}},
	}
}

// WithLogger подменяет логгер, сохраняя остальное. Отдельный сеттер, а не
// параметр конструктора: логгер нужен только в бою, а тесты собирают адаптеры
// прежними вызовами и не должны о нём знать.
func (c CallLog) WithLogger(log *zap.Logger) CallLog {
	if log == nil {
		return c
	}
	c.log = log
	return c
}

// Enabled сообщает, подключён ли настоящий логгер. Нужен адаптерам, которые
// готовят поля дороже, чем стоит сама запись.
func (c CallLog) Enabled() bool { return c.log != nil && c.log.Core().Enabled(zap.InfoLevel) }

// Success — вызов удался. Сбрасывает глушилку: следующий сбой снова заметен
// немедленно, а не через час.
func (c CallLog) Success(msg string, start time.Time, size int, extra ...zap.Field) {
	c.gate.reset()
	c.write(zapcore.InfoLevel, msg, start, append(extra, zap.Int("bytes", size))...)
}

// SuccessQuiet — то же, но записью DEBUG. Для ВЕЕРНЫХ вызовов: очередь 601
// ходит по вагонам пачками 50, памятки — документ за документом, и на INFO
// один проход даёт сотни строк. Итог веера сервис пишет одной строкой сам.
// Отказы при этом остаются на WARN — тишины по ошибке не наступает.
func (c CallLog) SuccessQuiet(msg string, start time.Time, size int, extra ...zap.Field) {
	c.gate.reset()
	c.write(zapcore.DebugLevel, msg, start, append(extra, zap.Int("bytes", size))...)
}

// Failure — вызов не удался. Причина переводится на человеческий язык в
// сообщение, полный текст уходит в поле error.
//
// Повторы глушатся (см. failGate): забор АСУ идёт кроном каждые 5 минут, и
// лежащий сутки провайдер иначе дал бы ~290 одинаковых строк на клиента.
func (c CallLog) Failure(start time.Time, err error, extra ...zap.Field) {
	msg := Reason(err)

	// Ключ глушилки включает клиента и номер попытки: адаптер один на всех
	// клиентов, и отказ по второму не должен потеряться за отказом по первому;
	// ретраи внутри одного вызова различаются номером и глушиться не должны.
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
		extra = append(extra, zap.Int("повторов", suppressed))
	}
	c.write(zapcore.WarnLevel, msg, start, append(extra, zap.String("error", err.Error()))...)
}

func (c CallLog) write(level zapcore.Level, msg string, start time.Time, extra ...zap.Field) {
	fields := []zap.Field{
		zap.String(FieldComponent, c.component),
		zap.String(FieldDir, DirOut),
		zap.String(FieldTarget, c.target),
	}
	if !start.IsZero() {
		fields = append(fields, zap.Duration("took", time.Since(start)))
	}
	fields = append(fields, extra...)

	if ce := c.log.Check(level, msg); ce != nil {
		ce.Write(fields...)
	}
}

// --- Глушилка повторяющихся отказов -----------------------------------------

// repeatWindow — как часто повторяющийся отказ всё-таки попадает в лог.
//
// Без глушилки правка сделала бы лог ХУЖЕ, чем был: логирования исходящих
// раньше не было вовсе, а кроны у нас частые (АСУ — 5 минут, памятки — час,
// очередь 601 — непрерывно). Час выбран так, чтобы простой оставался видимым
// в любой развёртке файла, но не вытеснял из него работу диспетчера.
const repeatWindow = time.Hour

type failGate struct {
	mu   sync.Mutex
	seen map[string]*failRun
}

type failRun struct {
	lastLogged time.Time
	count      int
}

// allow: писать ли этот отказ и сколько таких же было подавлено с прошлой записи.
func (g *failGate) allow(key string) (bool, int) {
	if g == nil {
		return true, 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	run, ok := g.seen[key]
	if !ok {
		g.seen[key] = &failRun{lastLogged: time.Now()}
		return true, 0
	}
	// Счётчик растёт ТОЛЬКО у проглоченных строк: «повторов=N» читается как
	// «столько же отказов в лог не попало». Инкремент до проверки окна
	// завышал бы N на единицу — записываемый отказ считал бы подавленным себя.
	if time.Since(run.lastLogged) < repeatWindow {
		run.count++
		return false, run.count
	}
	suppressed := run.count
	run.lastLogged, run.count = time.Now(), 0
	return true, suppressed
}

// reset: после успеха серия начинается заново.
func (g *failGate) reset() {
	if g == nil {
		return
	}
	g.mu.Lock()
	clear(g.seen)
	g.mu.Unlock()
}

// --- Причины по-человечески ---------------------------------------------------

// Reason переводит ошибку транспорта в короткую фразу для колонки сообщения.
// Полный текст при этом не теряется — он уходит в поле error.
func Reason(err error) string {
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
	case strings.Contains(s, "unexpected EOF"), strings.Contains(s, "EOF"):
		return "ответ оборван"
	default:
		return "вызов не удался"
	}
}

// hostOf оставляет host:port — контур важнее пути. Наш бой, стенд в контейнерах
// и машина разработчика ходят к разным адресам одного и того же провайдера, и
// без адреса в строке не понять, чей отказ читаешь.
func hostOf(baseURL string) string {
	if baseURL == "" {
		return ""
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return strings.TrimRight(baseURL, "/")
	}
	return u.Host
}
