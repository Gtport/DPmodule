package logger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
)

// Лог-файл читают grep и сборщики логов, поэтому в нём всегда JSON — даже при
// env=dev, когда консоль красит уровни ANSI-кодами. Раньше цвет попадал в файл
// («\x1b[34mINFO\x1b[0m») и ломал разбор.
func TestFileLogIsPlainJSONEvenInDevEnv(t *testing.T) {
	// Не t.TempDir(): его уборка падает на Windows — lumberjack держит файл
	// открытым, закрыть его через zap нельзя (writer наружу не отдаётся).
	dir, err := os.MkdirTemp("", "logger-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	path := filepath.Join(dir, "service.log")

	log, err := New(Config{Level: "info", Env: "dev", File: path})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	//nolint:errcheck // Sync на stdout под Windows возвращает ошибку — не важна
	log.Info("проверка", zap.String("ключ", "значение"))
	_ = log.Sync()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("лог-файл не создан: %v", err)
	}
	line := strings.TrimSpace(string(data))

	if strings.Contains(line, "\x1b[") {
		t.Errorf("в файл попали ANSI-коды цвета: %q", line)
	}

	var rec map[string]any
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("строка файла не разбирается как JSON: %v\n%s", err, line)
	}
	if rec["msg"] != "проверка" || rec["ключ"] != "значение" {
		t.Errorf("поля записи потерялись: %v", rec)
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
