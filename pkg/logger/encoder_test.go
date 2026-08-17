package logger

import (
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// encodeLine прогоняет одну запись через текстовый энкодер и отдаёт строку без
// перевода. Зона фиксированная, чтобы тест не зависел от машины.
func encodeLine(t *testing.T, msg string, fields ...zap.Field) string {
	t.Helper()

	enc := newTextEncoder(false, time.FixedZone("MSK", 3*60*60))
	ent := zapcore.Entry{
		Level: zapcore.InfoLevel,
		Time:  time.Date(2026, 8, 17, 15, 20, 0, 340_000_000, time.UTC),
		// 15:20 UTC → 18:20 MSK: заодно проверяем, что зона реально применяется.
		Message: msg,
	}
	buf, err := enc.EncodeEntry(ent, fields)
	if err != nil {
		t.Fatalf("EncodeEntry: %v", err)
	}
	return strings.TrimSuffix(buf.String(), "\n")
}

// Выравнивание считается по РУНАМ. У нас весь лог русский, в UTF-8 кириллица
// занимает два байта, и len() увёл бы колонки на каждой строке.
func TestPadCountsRunesNotBytes(t *testing.T) {
	ru := encodeLine(t, "сообщение", Comp(CompDislocation), zap.Int("вагонов", 1))
	en := encodeLine(t, "message__", Comp(CompDislocation), zap.Int("вагонов", 1))

	posRU := strings.Index(ru, "вагонов=")
	posEN := strings.Index(en, "вагонов=")
	if posRU < 0 || posEN < 0 {
		t.Fatalf("поле не найдено:\n%s\n%s", ru, en)
	}
	// Позиции в РУНАХ должны совпасть: сообщения равной длины в рунах.
	runesRU := len([]rune(ru[:posRU]))
	runesEN := len([]rune(en[:posEN]))
	if runesRU != runesEN {
		t.Errorf("колонка полей разъехалась: русская строка %d рун, латинская %d\n%s\n%s",
			runesRU, runesEN, ru, en)
	}
}

// Строка без полей не должна оканчиваться пробелами: выравнивание сообщения
// добивало бы хвост на каждой второй строке файла.
func TestNoTrailingSpacesWhenNoFields(t *testing.T) {
	line := encodeLine(t, "приложение остановлено", Comp(CompStartup))
	if line != strings.TrimRight(line, " ") {
		t.Errorf("висячие пробелы в конце строки: %q", line)
	}
}

// Колонка цели собирается из dir+target и показывает направление стрелкой.
func TestTargetColumnDirection(t *testing.T) {
	out := encodeLine(t, "срез получен", Out(CompDislocation, "АСУ 10.0.0.5:443")...)
	if !strings.Contains(out, "→ АСУ 10.0.0.5:443") {
		t.Errorf("нет исходящей цели:\n%s", out)
	}
	in := encodeLine(t, "запрос обработан", In(CompHTTP, "ivanov 10.0.0.14")...)
	if !strings.Contains(in, "← ivanov 10.0.0.14") {
		t.Errorf("нет входящей цели:\n%s", in)
	}
	// Служебные поля ушли в колонки, в хвосте их быть не должно.
	if strings.Contains(out, "component=") || strings.Contains(out, "dir=") || strings.Contains(out, "target=") {
		t.Errorf("служебные поля продублированы в хвосте:\n%s", out)
	}
}

// Событие без сети колонку цели оставляет пустой, но ширину держит — иначе
// сообщения соседних строк не выстроятся друг под другом.
func TestEmptyTargetKeepsColumns(t *testing.T) {
	withTarget := encodeLine(t, "срез получен", append(Out(CompDislocation, "АСУ"), zap.Int("вагонов", 5))...)
	noTarget := encodeLine(t, "снимок пересобран", Comp(CompDislocation), zap.Int("вагонов", 5))

	col := func(s string) int { return len([]rune(s[:strings.Index(s, "вагонов=")])) }
	if col(withTarget) != col(noTarget) {
		t.Errorf("колонки не совпали:\n%s\n%s", withTarget, noTarget)
	}
}

// Время печатается в переданной зоне, а не в зоне сервера.
func TestTimeUsesGivenLocation(t *testing.T) {
	line := encodeLine(t, "старт", Comp(CompStartup))
	if !strings.HasPrefix(line, "17.08 18:20:00.340") {
		t.Errorf("время не в московской зоне: %q", line)
	}
}

func TestFormatDuration(t *testing.T) {
	cases := map[time.Duration]string{
		340 * time.Microsecond:                   "340мкс",
		232 * time.Millisecond:                   "232мс",
		2 * time.Second:                          "2с",
		2500 * time.Millisecond:                  "2.5с",
		53700 * time.Millisecond:                 "53.7с",
		5 * time.Minute:                          "5м",
		5*time.Minute + 30*time.Second:           "5м 30с",
		3 * time.Hour:                            "3ч",
		13*time.Hour + 20*time.Minute:            "13ч 20м",
		time.Duration(0):                         "0мкс",
		-(2*time.Minute + 3*time.Second):         "-2м 3с",
		time.Hour + 30*time.Minute + time.Second: "1ч 30м",
	}
	for d, want := range cases {
		if got := FormatDuration(d); got != want {
			t.Errorf("FormatDuration(%v) = %q, ожидалось %q", d, got, want)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	cases := map[int]string{
		512:              "512Б",
		2048:             "2КБ",
		1258291:          "1.2МБ",
		11 * 1024 * 1024: "11.0МБ",
	}
	for n, want := range cases {
		if got := FormatBytes(n); got != want {
			t.Errorf("FormatBytes(%d) = %q, ожидалось %q", n, got, want)
		}
	}
}

// bytes хранится числом (json-формат должен получить число), а человеку
// показывается объёмом.
func TestBytesFieldRenderedHumanReadable(t *testing.T) {
	line := encodeLine(t, "срез получен", Comp(CompDislocation), zap.Int("bytes", 1258291))
	if !strings.Contains(line, "bytes=1.2МБ") {
		t.Errorf("объём не переведён в человеческий вид:\n%s", line)
	}
}

// Порядок полей: сначала «кто», потом по алфавиту, в конце «сколько стоило».
// Одинаковые события должны давать одинаковую раскладку строки.
func TestFieldOrder(t *testing.T) {
	line := encodeLine(t, "запрос",
		zap.Duration("took", 100*time.Millisecond),
		zap.String("status", "ok"),
		zap.String("client", "attis"),
		zap.Int("count", 3),
	)
	idx := func(s string) int { return strings.Index(line, s) }
	if !(idx("client=") < idx("count=") && idx("count=") < idx("status=") && idx("status=") < idx("took=")) {
		t.Errorf("порядок полей нарушен:\n%s", line)
	}
}

// Словарь component закрыт: значение вне списка — ошибка разработчика, и тест
// это ловит. Проверка живёт здесь, чтобы не заводить отдельный файл.
func TestComponentDictionaryClosed(t *testing.T) {
	for _, c := range components {
		if !IsComponent(c) {
			t.Errorf("компонент %q не признан своим", c)
		}
	}
	for _, c := range []string{"", "асу", "601", "reference", "ЛК РЖД"} {
		if IsComponent(c) {
			t.Errorf("значение %q не должно проходить как компонент", c)
		}
	}
}
