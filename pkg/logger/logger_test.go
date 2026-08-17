package logger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
)

// writeToFile поднимает логгер с файлом, пишет одну запись и отдаёт её текст.
func writeToFile(t *testing.T, cfg Config, write func(*zap.Logger)) string {
	t.Helper()

	// Не t.TempDir(): его уборка падает на Windows — lumberjack держит файл
	// открытым, закрыть его через zap нельзя (writer наружу не отдаётся).
	dir, err := os.MkdirTemp("", "logger-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	cfg.File = filepath.Join(dir, "service.log")
	log, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	write(log)
	//nolint:errcheck // Sync на stdout под Windows возвращает ошибку — не важна
	_ = log.Sync()

	data, err := os.ReadFile(cfg.File)
	if err != nil {
		t.Fatalf("лог-файл не создан: %v", err)
	}
	return strings.TrimSpace(string(data))
}

// Цвет — только для stdout. В файле «\x1b[34mINFO\x1b[0m» ломает grep, и раньше
// именно это и происходило при env=dev.
func TestFileLogHasNoANSIColor(t *testing.T) {
	for _, format := range []string{FormatText, FormatJSON} {
		line := writeToFile(t, Config{Level: "info", Env: "dev", Format: format},
			func(l *zap.Logger) { l.Info("проверка", Comp(CompStartup)) })

		if strings.Contains(line, "\x1b[") {
			t.Errorf("format=%s: в файл попали ANSI-коды цвета: %q", format, line)
		}
	}
}

// Формат задаётся полем конфига, а не env: при json файл разбирается сборщиком,
// при text — читается глазом, и в обоих случаях поля одни и те же.
func TestFormatSelectsEncoder(t *testing.T) {
	jsonLine := writeToFile(t, Config{Level: "info", Env: "prod", Format: FormatJSON},
		func(l *zap.Logger) {
			l.Info("проверка", Comp(CompDislocation), zap.String("ключ", "значение"))
		})

	var rec map[string]any
	if err := json.Unmarshal([]byte(jsonLine), &rec); err != nil {
		t.Fatalf("format=json не разбирается как JSON: %v\n%s", err, jsonLine)
	}
	if rec["msg"] != "проверка" || rec["ключ"] != "значение" {
		t.Errorf("поля записи потерялись: %v", rec)
	}
	// Служебные поля колонок в json остаются обычными полями — грепу доступно
	// то же самое, что в тексте.
	if rec["component"] != CompDislocation {
		t.Errorf("component не попал в json: %v", rec)
	}

	textLine := writeToFile(t, Config{Level: "info", Env: "prod", Format: FormatText},
		func(l *zap.Logger) {
			l.Info("проверка", Comp(CompDislocation), zap.String("ключ", "значение"))
		})

	if json.Valid([]byte(textLine)) {
		t.Errorf("format=text отдал JSON: %s", textLine)
	}
	if !strings.Contains(textLine, CompDislocation) || !strings.Contains(textLine, "ключ=значение") {
		t.Errorf("в текстовой строке нет компонента или поля: %s", textLine)
	}
}

// Пустой формат — не опечатка, а «не задано»: получаем текст для человека.
func TestEmptyFormatDefaultsToText(t *testing.T) {
	line := writeToFile(t, Config{Level: "info", Env: "prod"},
		func(l *zap.Logger) { l.Info("проверка", Comp(CompStartup)) })

	if json.Valid([]byte(line)) {
		t.Errorf("по умолчанию ожидался текст, получен JSON: %s", line)
	}
}

// Опечатка в уровне должна ронять старт, а не молча давать info: иначе стенд
// поднимается «работающим», нужных записей в файле нет, и сказать об этом
// некому — сообщить должен был как раз лог.
func TestUnknownLevelIsRejected(t *testing.T) {
	for _, lvl := range []string{"infoo", "verbose", "TRACE"} {
		if _, err := New(Config{Level: lvl, Env: "prod"}); err == nil {
			t.Errorf("уровень %q принят молча, ожидался отказ", lvl)
		} else if !strings.Contains(err.Error(), lvl) {
			t.Errorf("в ошибке нет самого значения %q: %v", lvl, err)
		}
	}
}

// Пустой уровень — не опечатка, а «не задано»: config.Load подставляет info,
// и собственные вызовы New (тесты, утилиты) не обязаны его указывать.
func TestEmptyLevelDefaultsToInfo(t *testing.T) {
	log, err := New(Config{Level: "", Env: "prod"})
	if err != nil {
		t.Fatalf("пустой уровень должен приниматься: %v", err)
	}
	if log.Core().Enabled(zap.DebugLevel) {
		t.Error("по умолчанию ожидался info, а debug оказался включён")
	}
}

// Пустой File — только stdout, файла быть не должно (шаблон/контейнер, где
// логи забирает docker).
func TestNoFileWhenPathEmpty(t *testing.T) {
	dir := t.TempDir()

	log, err := New(Config{Level: "info", Env: "prod", File: ""})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	log.Info("в stdout")
	_ = log.Sync()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("при пустом File файлы создаваться не должны: %v", entries)
	}
}
