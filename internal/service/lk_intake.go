package service

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/Gtport/DPmodule/internal/domain"
)

// LKIntake — шаг 1 двухшаговой загрузки ЛК: приём xlsx-файла, лёгкая инспекция
// (тип/порт/метка формирования) и сохранение в локальную папку <baseDir>/lk/.
// Дислокацию НЕ трогает (её перестраивает отдельный шаг «обработка»). Настройки
// (расширения, лимит, ОКПО-мэппинг, маркеры) читаются из ConfigCache (источник
// 'lk'). Время — Московское naive, без сдвигов (§3.11).
type LKIntake struct {
	cfg     *ConfigCache
	baseDir string
}

func NewLKIntake(cfg *ConfigCache, baseDir string) *LKIntake {
	return &LKIntake{cfg: cfg, baseDir: baseDir}
}

// Ошибки приёма (хендлер маппит их в HTTP-коды).
var (
	ErrNoLKSource        = errors.New("источник 'lk' не настроен")
	ErrBadExt            = errors.New("недопустимое расширение файла")
	ErrTooLarge          = errors.New("файл слишком большой")
	ErrNotLK             = errors.New("файл не похож на выгрузку дислокации из ЛК")
	ErrInspect           = errors.New("не удалось разобрать файл ЛК")
	ErrUnknownOkpo       = errors.New("неизвестный грузополучатель (ОКПО)")
	ErrOlderThanExisting = errors.New("загружаемый файл старше уже сохранённого")
)

const defaultOkpoColumn = "Грузополучатель (ОКПО)"

// LKStored — результат сохранения.
type LKStored struct {
	Port        string
	FormationTS domain.LocalTime
	Filename    string
	Replaced    bool // заменил более старую версию того же порта
}

// Store принимает файл (origName — для расширения, data — содержимое), валидирует,
// инспектирует и сохраняет в <baseDir>/lk/<PORT>_<ДДММГГ-ЧЧММ>.xlsx.
func (s *LKIntake) Store(origName string, data []byte) (LKStored, error) {
	ds, ok := s.cfg.DataSource("lk")
	if !ok || !ds.Enabled {
		return LKStored{}, ErrNoLKSource
	}
	cfg := ds.Config

	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(origName), "."))
	if len(cfg.AllowedExt) > 0 && !containsStr(cfg.AllowedExt, ext) {
		return LKStored{}, fmt.Errorf("%w: .%s", ErrBadExt, ext)
	}
	if cfg.MaxMB > 0 && len(data) > cfg.MaxMB*1024*1024 {
		return LKStored{}, fmt.Errorf("%w: > %d МБ", ErrTooLarge, cfg.MaxMB)
	}

	port, ft, err := inspectLK(data, cfg)
	if err != nil {
		return LKStored{}, err
	}

	filename, replaced, err := s.save(port, ft, data)
	if err != nil {
		return LKStored{}, err
	}
	return LKStored{Port: port, FormationTS: ft, Filename: filename, Replaced: replaced}, nil
}

// inspectLK — лёгкое чтение: маркер «Личный кабинет», ОКПО→порт, дата формирования.
// Повторяет раскладку GTport: маркер найден сканированием первых ячеек последнего
// листа; дата — в ячейке (col-1, row+1) относительно маркера; ОКПО — в колонке
// okpo_column ниже строки заголовка. Без сдвигов времени.
func inspectLK(data []byte, cfg domain.DataSourceConfig) (string, domain.LocalTime, error) {
	var zero domain.LocalTime
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return "", zero, fmt.Errorf("%w: %v", ErrNotLK, err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return "", zero, ErrNotLK
	}
	rows, err := f.GetRows(sheets[len(sheets)-1]) // ЛК-данные на последнем листе
	if err != nil {
		return "", zero, fmt.Errorf("%w: %v", ErrInspect, err)
	}

	// 1. Маркер «Личный кабинет» — первая непустая ячейка (скан 5×10).
	text, r, c := firstNonEmptyCell(rows, 5, 10)
	if text == "" || !containsAny(text, cfg.Detect) {
		return "", zero, ErrNotLK
	}

	// 2. Дата формирования — ячейка (col-1, row+1) относительно маркера.
	ft, ok := zero, false
	if r+1 < len(rows) && c-1 >= 0 && c-1 < len(rows[r+1]) {
		ft, ok = parseFormationTS(rows[r+1][c-1])
	}
	if !ok {
		return "", zero, fmt.Errorf("%w: не удалось прочитать дату формирования", ErrInspect)
	}

	// 3. Строка заголовка по маркеру (header_marker) → колонка ОКПО → значение ОКПО.
	headerMarker := cfg.HeaderMarker
	if headerMarker == "" {
		headerMarker = "Номер вагона"
	}
	h := rowIndexContaining(rows, headerMarker, 25)
	if h < 0 {
		return "", zero, fmt.Errorf("%w: не найдена строка заголовка %q", ErrInspect, headerMarker)
	}
	okpoColMarker := cfg.OkpoColumn
	if okpoColMarker == "" {
		okpoColMarker = defaultOkpoColumn
	}
	okpoCol := colIndexContaining(rows[h], okpoColMarker)
	if okpoCol < 0 {
		return "", zero, fmt.Errorf("%w: не найдена колонка %q", ErrInspect, okpoColMarker)
	}
	okpo := firstNonEmptyInColumn(rows, okpoCol, h+1, 20)
	if okpo == "" {
		return "", zero, fmt.Errorf("%w: пустая колонка ОКПО", ErrInspect)
	}

	port, ok := cfg.PortByOkpo(okpo)
	if !ok {
		return "", zero, fmt.Errorf("%w: %s", ErrUnknownOkpo, okpo)
	}
	return port, ft, nil
}

// save кладёт файл в <baseDir>/lk/, оставляя одну актуальную версию на порт:
// если существующая версия того же порта НОВЕЕ — отказ; иначе старую заменяем.
func (s *LKIntake) save(port string, ft domain.LocalTime, data []byte) (string, bool, error) {
	dir := filepath.Join(s.baseDir, "lk")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", false, err
	}
	filename := port + "_" + time.Time(ft).Format("020106-1504") + ".xlsx"

	matches, _ := filepath.Glob(filepath.Join(dir, port+"_*.xlsx"))
	for _, m := range matches {
		if ets, ok := tsFromFilename(filepath.Base(m), port); ok && time.Time(ft).Before(ets) {
			return "", false, fmt.Errorf("%w: %s", ErrOlderThanExisting, filepath.Base(m))
		}
	}
	for _, m := range matches {
		_ = os.Remove(m)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), data, 0o644); err != nil {
		return "", false, err
	}
	return filename, len(matches) > 0, nil
}

// ─────────────────────────── helpers ───────────────────────────

func parseFormationTS(s string) (domain.LocalTime, bool) {
	s = strings.TrimSpace(s)
	for _, f := range []string{"02.01.2006 15:04", "02.01.2006 15:04:05", "02.01.2006"} {
		if t, err := time.Parse(f, s); err == nil {
			return domain.LocalTime(t), true
		}
	}
	return domain.LocalTime{}, false
}

func tsFromFilename(name, port string) (time.Time, bool) {
	s := strings.TrimSuffix(strings.TrimPrefix(name, port+"_"), ".xlsx")
	if t, err := time.Parse("020106-1504", s); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func firstNonEmptyCell(rows [][]string, maxRows, maxCols int) (string, int, int) {
	for i := 0; i < len(rows) && i < maxRows; i++ {
		for j := 0; j < len(rows[i]) && j < maxCols; j++ {
			if v := strings.TrimSpace(rows[i][j]); v != "" {
				return v, i, j
			}
		}
	}
	return "", -1, -1
}

func rowIndexContaining(rows [][]string, marker string, maxRows int) int {
	for i := 0; i < len(rows) && i < maxRows; i++ {
		for _, cell := range rows[i] {
			if strings.Contains(cell, marker) {
				return i
			}
		}
	}
	return -1
}

func colIndexContaining(row []string, marker string) int {
	for j, cell := range row {
		if strings.Contains(cell, marker) {
			return j
		}
	}
	return -1
}

func firstNonEmptyInColumn(rows [][]string, col, startRow, maxRows int) string {
	for i := startRow; i < len(rows) && i < startRow+maxRows; i++ {
		if col < len(rows[i]) {
			if v := strings.TrimSpace(rows[i][col]); v != "" {
				return v
			}
		}
	}
	return ""
}

func containsAny(text string, markers []string) bool {
	for _, m := range markers {
		if m != "" && strings.Contains(text, m) {
			return true
		}
	}
	return false
}

func containsStr(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
