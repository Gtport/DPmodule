// server/internal/service/parse_lk.go
package service

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gtport/server/internal/models"

	"github.com/jmoiron/sqlx"
	"github.com/xuri/excelize/v2"
)

// LKParser структура для парсинга файлов типа LK (выгрузка SPV4664 из ЛК РЖД).
//
// Принцип: ЛК-парсер и JSON-парсер кормят одну модель Dislocation и должны давать
// ОДИН набор полей. Из ЛК берём только КОДЫ (из скобок «Имя (код)») — имена дорог,
// грузоотправителя/грузополучателя и т.п. проставляет обогащение из справочников.
// Поля, которые обогащение всё равно перезаписывает (DorogaNach/DorogaOper ← stations,
// Gruzotpr ← marka, Gruzpol ← ports, IndexMain ← Index), здесь НЕ парсятся.
type LKParser struct {
	db                 *sqlx.DB
	dislocationService *DislocationService
}

// NewLKParser создает новый парсер LK файлов
func NewLKParser(db *sqlx.DB, dislocationService *DislocationService) *LKParser {
	return &LKParser{
		db:                 db,
		dislocationService: dislocationService,
	}
}

// generateDeterministicID создает детерминированный ID на основе ключевых полей
func generateDeterministicID(vagon, codeStationNach string, dateNach *time.Time) string {
	if vagon == "" || codeStationNach == "" || dateNach == nil || dateNach.IsZero() {
		return fmt.Sprintf("temp_%d", time.Now().UnixNano())
	}
	dateStr := dateNach.Format("02.01.2006")
	return fmt.Sprintf("%s/%s/%s", vagon, codeStationNach, dateStr)
}

// ParseLKDirectory парсит все файлы LK в указанной директории
func (p *LKParser) ParseLKDirectory(dirPath string) ([]models.Dislocation, error) {
	files, err := filepath.Glob(filepath.Join(dirPath, "*.xlsx"))
	if err != nil {
		return nil, fmt.Errorf("ошибка поиска файлов: %v", err)
	}

	var allRecords []models.Dislocation
	for _, file := range files {
		records, err := p.ParseLKFile(file)
		if err != nil {
			log.Printf("Ошибка парсинга файла %s: %v", file, err)
			continue
		}
		allRecords = append(allRecords, records...)
	}
	return allRecords, nil
}

// ParseLKFile парсит один файл LK
func (p *LKParser) ParseLKFile(filePath string) ([]models.Dislocation, error) {
	f, err := excelize.OpenFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("ошибка открытия файла: %v", err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("файл не содержит листов")
	}

	var allRecords []models.Dislocation
	for _, sheet := range sheets {
		records, err := p.parseSheet(f, sheet)
		if err != nil {
			log.Printf("Ошибка парсинга листа %s: %v", sheet, err)
			continue
		}
		allRecords = append(allRecords, records...)
	}
	return allRecords, nil
}

// parseSheet парсит один лист Excel
func (p *LKParser) parseSheet(f *excelize.File, sheet string) ([]models.Dislocation, error) {
	rows, err := f.GetRows(sheet)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения строк листа %s: %v", sheet, err)
	}

	headerRowIndex := -1
	var headerRow []string
	for i, row := range rows {
		if containsSubstring(row, "Номер вагона") {
			headerRowIndex = i
			headerRow = row
			break
		}
	}
	if headerRowIndex == -1 {
		return nil, fmt.Errorf("не найдена строка с заголовком 'Номер вагона'")
	}

	colIndexes := p.getColumnIndexes(headerRow)
	if len(colIndexes) == 0 {
		return nil, fmt.Errorf("не найдены необходимые заголовки столбцов")
	}

	var records []models.Dislocation
	for i := headerRowIndex + 1; i < len(rows); i++ {
		if len(rows[i]) == 0 {
			continue
		}
		record, err := p.parseRow(rows[i], colIndexes)
		if err != nil {
			log.Printf("Ошибка парсинга строки %d: %v", i+1, err)
			continue
		}
		if record.Vagon != "" {
			records = append(records, record)
		}
	}
	return records, nil
}

// getColumnIndexes определяет индексы столбцов по заголовкам с приоритетом точных совпадений
func (p *LKParser) getColumnIndexes(headerRow []string) map[string]int {
	indexes := make(map[string]int)

	// Точные совпадения заголовков
	exactMatches := map[string]string{
		// базовые
		"Номер вагона":                                  "vagon_c",
		"Номер накладной":                               "invoice_c",
		"Индекс поезда":                                 "index_c",
		"Дата и время начала рейса":                     "date_nach_c",
		"Станция отправления":                           "code_station_nach_c",
		"Грузоотправитель (ОКПО)":                       "gruzotpr_okpo_c",
		"Станция назначения":                            "code_stan_nazn_c",
		"Грузополучатель (ОКПО)":                        "gruzpol_okpo_c",
		"Наименование груза":                            "code_cargo_c",
		"Вес груза (кг)":                                "ves_c",
		"Дата и время операции":                         "time_op_c",
		"Операция с вагоном":                            "code_oper_c",
		"Станция операции":                              "code_station_oper_c",
		"Расстояние оставшееся (км)":                    "rasst_stan_nazn_c",
		"Время простоя под последней операцией (сутки)": "prost_dn_c",
		"Время простоя под последней операцией (сутки:часы:минуты)": "prost_ch_c",
		"Номер вагона в составе поезда":                             "npp_vag_c",
		"Нормативный срок доставки":                                 "date_dostav_c",
		"Род вагона":             "rod_vag_uch_c",
		"Государство назначения": "str_nazn_c",
		// добавленные (паритет с JSON)
		"Государство отправления":    "str_nach_c",
		"Дорога назначения":          "doroga_nazn_c",
		"Код груза ГНГ":              "code_cargo_gng_c",
		"Ранее выгруженный груз":     "code_cargo_vygr_c",
		"Тип парка (П/Г)":            "porozh_c",
		"Идентификатор отправки":     "id_otprk_c",
		"Идентификатор накладной":    "uno_c",
		"Расстояние общее (км)":      "rasst_ob_c",
		"Расстояние пройденное (км)": "rasst_stan_op_c",
	}

	// Частичные совпадения (если точных нет). Подстроки подобраны так, чтобы не пересекаться.
	partialMatches := map[string]string{
		"Накладной":                "invoice_c",
		"Индекс":                   "index_c",
		"Дата начала":              "date_nach_c",
		"Станция отпр":             "code_station_nach_c",
		"Грузоотправитель ОКПO":    "gruzotpr_okpo_c",
		"Станция назн":             "code_stan_nazn_c",
		"Грузополучатель ОКПO":     "gruzpol_okpo_c",
		"Наименование груза":       "code_cargo_c",
		"Вес груза":                "ves_c",
		"Дата и время опер":        "time_op_c",
		"Операция с":               "code_oper_c",
		"Станция опер":             "code_station_oper_c",
		"Расстояние оставшееся":    "rasst_stan_nazn_c",
		"Расстояние общее":         "rasst_ob_c",
		"Расстояние пройденное":    "rasst_stan_op_c",
		"Номер в составе":          "npp_vag_c",
		"Нормативный срок":         "date_dostav_c",
		"Род вагона":               "rod_vag_uch_c",
		"Государство назн":         "str_nazn_c",
		"Государство отпр":         "str_nach_c",
		"Дорога назн":              "doroga_nazn_c",
		"груза ГНГ":                "code_cargo_gng_c",
		"Ранее выгру":              "code_cargo_vygr_c",
		"Тип парка":                "porozh_c",
		"Идентификатор отправки":   "id_otprk_c",
		"Идентификатор накладной":  "uno_c",
		"Дата и время отправления": "date_otpr_c",
		"Дата и время прибытия":    "date_prib_c",
	}

	// Точные совпадения
	for colIndex, header := range headerRow {
		header = strings.TrimSpace(header)
		for pattern, identifier := range exactMatches {
			if strings.EqualFold(header, pattern) {
				indexes[identifier] = colIndex
				break
			}
		}
	}

	// Частичные совпадения для недостающих
	for colIndex, header := range headerRow {
		header = strings.TrimSpace(header)
		for pattern, identifier := range partialMatches {
			if _, exists := indexes[identifier]; !exists &&
				strings.Contains(strings.ToLower(header), strings.ToLower(pattern)) {
				indexes[identifier] = colIndex
				break
			}
		}
	}

	return indexes
}

// parseRow парсит одну строку данных в models.Dislocation
func (p *LKParser) parseRow(row []string, colIndexes map[string]int) (models.Dislocation, error) {
	var record models.Dislocation

	getValue := func(key string) string {
		if index, exists := colIndexes[key]; exists && index < len(row) {
			return strings.TrimSpace(row[index])
		}
		return ""
	}

	// ── Ключевые поля + детерминированный ID ────────────────────────────────
	record.Vagon = getValue("vagon_c")
	dateNachT := p.parseDateWithTimeAdjustmentDate(getValue("date_nach_c")) // *time.Time для ID
	record.CodeStationNach = p.extractSixDigits(getValue("code_station_nach_c"))
	record.ID = generateDeterministicID(record.Vagon, record.CodeStationNach, dateNachT)
	record.DateNach = models.FromTimePtr(dateNachT)

	record.Invoice = getValue("invoice_c")

	// Станция операции — ДО parseIndex
	record.CodeStationOper = p.extractSixDigits(getValue("code_station_oper_c"))
	record.Index = p.parseIndex(getValue("index_c"), record.CodeStationNach, record.CodeStationOper)
	record.CodeStanNazn = p.extractSixDigits(getValue("code_stan_nazn_c"))

	// ОКПО (имена грузоотправителя/грузополучателя проставит обогащение)
	record.GruzotprOkpo = getValue("gruzotpr_okpo_c")
	record.GruzpolOkpo = getValue("gruzpol_okpo_c")

	// ── Груз: код ЕТСНГ из скобок (имя груза даст обогащение из marka) ───────
	record.CodeCargo = p.extractDigitsFromBrackets(getValue("code_cargo_c"))
	record.CodeCargoGng = getValue("code_cargo_gng_c") // голый код, без скобок
	record.CodeCargoVygr = p.extractDigitsFromBrackets(getValue("code_cargo_vygr_c"))

	record.Ves = p.parseWeight(getValue("ves_c"))
	record.PorozhPriznak = p.porozhToCode(getValue("porozh_c")) // слово → код «1»/«0»

	// ── Операция ────────────────────────────────────────────────────────────
	record.TimeOp = p.parseLocalDateTime(getValue("time_op_c"))
	record.DateOp = record.TimeOp
	record.CodeOper = p.extractDigitsFromBrackets(getValue("code_oper_c"))

	// ── Дороги/страны: только КОДЫ из скобок ────────────────────────────────
	// DorogaNach/DorogaOper НЕ парсим — их кладёт обогащение (stations.Road, имя).
	record.StrNach = p.extractDigitsFromBrackets(getValue("str_nach_c"))
	record.DorogaNazn = p.extractDigitsFromBrackets(getValue("doroga_nazn_c"))
	record.StrNazn = p.extractDigitsFromBrackets(getValue("str_nazn_c"))

	// ── Расстояния ──────────────────────────────────────────────────────────
	record.RasstStanNazn = p.parseInt(getValue("rasst_stan_nazn_c"))
	record.RasstOb = p.parseInt(getValue("rasst_ob_c"))
	record.RasstStanOp = p.parseInt(getValue("rasst_stan_op_c"))

	// ── Простой: сутки / часы / минуты ──────────────────────────────────────
	record.ProstDn = p.parseInt(getValue("prost_dn_c"))
	prostCompound := getValue("prost_ch_c") // «сутки:часы:минуты»
	record.ProstCh = p.parseProstCh(prostCompound)
	record.ProstMin = p.parseProstMin(prostCompound)

	record.NppVag = p.parseInt(getValue("npp_vag_c"))

	// ── Идентификаторы ──────────────────────────────────────────────────────
	record.IdOtprk = getValue("id_otprk_c")
	record.Uno = p.padUno(getValue("uno_c")) // паддинг нулями до 12 (Excel срезает нули)

	// ── Даты (LocalTime, без таймзоны) ──────────────────────────────────────
	record.DateDostav = p.parseLocalDate(getValue("date_dostav_c"))
	record.DateOtpr = p.parseLocalDateTime(getValue("date_otpr_c"))
	record.DatePrib = p.parseLocalDateTime(getValue("date_prib_c")) // достоверная дата прибытия

	// ── Род вагона: код из скобок (включено) ────────────────────────────────
	record.RodVagUch = p.extractDigitsFromBrackets(getValue("rod_vag_uch_c"))

	// Поля, которых нет в выгрузке ЛК (заполняются из JSON/эндпоинта):
	//   FreightExactName, GtdNumber, Zayavka (ГУ-12), CarOwnerName/Okpo, CarTenantName/Okpo.

	record.CreatedAt = models.LocalTime(time.Now())
	record.UpdatedAt = models.LocalTime(time.Now())

	return record, nil
}

// parseIndex парсит индекс поезда с проверкой на совпадение с codeStationNach
func (p *LKParser) parseIndex(indexStr, codeStationNach, codeStationOper string) string {
	if indexStr == "" {
		return "Б/И"
	}

	re := regexp.MustCompile(`\(.*?\)`)
	cleanStr := strings.TrimSpace(re.ReplaceAllString(indexStr, ""))

	parts := strings.Fields(cleanStr)
	if len(parts) < 3 {
		return "Б/И"
	}

	cleanNach := strings.TrimSpace(codeStationNach)
	cleanOper := strings.TrimSpace(codeStationOper)

	if cleanNach != "" && cleanOper != "" && cleanNach == cleanOper {
		firstPartDigits := regexp.MustCompile(`\d+`).FindString(parts[0])
		if firstPartDigits != cleanNach {
			return "Б/И"
		}
		thirdPartDigits := regexp.MustCompile(`\d+`).FindString(parts[2])
		if len(thirdPartDigits) == 6 && thirdPartDigits == cleanNach {
			return "Б/И"
		}
	}

	firstPart := safeSubstring(parts[0], 0, 4)
	secondPart := safeSubstring(parts[1], 0, 3)
	thirdPartFormatted := safeSubstring(parts[2], 0, 4)

	if firstPart == "" || secondPart == "" || thirdPartFormatted == "" {
		return "Б/И"
	}

	return fmt.Sprintf("%s-%s-%s", firstPart, secondPart, thirdPartFormatted)
}

// extractSixDigits извлекает 6-значный код станции из скобок
func (p *LKParser) extractSixDigits(text string) string {
	if text == "" {
		return ""
	}
	re := regexp.MustCompile(`\((\d{6})\)`)
	matches := re.FindStringSubmatch(text)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// extractDigitsFromBrackets извлекает код (любой длины) из скобок
func (p *LKParser) extractDigitsFromBrackets(text string) string {
	if text == "" {
		return ""
	}
	re := regexp.MustCompile(`\((\d+)\)`)
	matches := re.FindStringSubmatch(text)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// porozhToCode переводит «Тип парка (П/Г)» в код, как в JSON (PPV_POR): 1/0
func (p *LKParser) porozhToCode(text string) string {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "порожний":
		return "1"
	case "груженый", "гружёный":
		return "0"
	default:
		return ""
	}
}

// padUno дополняет числовой идентификатор накладной нулями слева до 12 знаков
// (Excel срезает ведущие нули, в JSON UNO всегда 12 знаков).
func (p *LKParser) padUno(text string) string {
	s := strings.TrimSpace(text)
	if s == "" {
		return ""
	}
	if _, err := strconv.ParseUint(s, 10, 64); err != nil {
		return s // не число — оставляем как есть
	}
	if len(s) >= 12 {
		return s
	}
	return strings.Repeat("0", 12-len(s)) + s
}

// parseWeight парсит вес и конвертирует кг → тонны
func (p *LKParser) parseWeight(weightStr string) *float64 {
	if weightStr == "" {
		return nil
	}
	cleanStr := strings.ReplaceAll(weightStr, " ", "")
	cleanStr = strings.ReplaceAll(cleanStr, ",", ".")
	weight, err := strconv.ParseFloat(cleanStr, 64)
	if err != nil {
		return nil
	}
	result := weight / 1000.0
	return &result
}

// parseDateTime парсит дату и время (возвращает *time.Time)
func (p *LKParser) parseDateTime(dateTimeStr string) *time.Time {
	if dateTimeStr == "" {
		return nil
	}
	formats := []string{
		"02.01.2006 15:04",
		"02.01.2006",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, format := range formats {
		t, err := time.Parse(format, dateTimeStr)
		if err == nil {
			return &t
		}
	}
	return nil
}

// parseDate парсит только дату (возвращает *time.Time)
func (p *LKParser) parseDate(dateStr string) *time.Time {
	if dateStr == "" {
		return nil
	}
	formats := []string{"02.01.2006", "2006-01-02"}
	for _, format := range formats {
		t, err := time.Parse(format, dateStr)
		if err == nil {
			return &t
		}
	}
	return nil
}

// parseLocalDateTime — обёртка parseDateTime → *models.LocalTime
func (p *LKParser) parseLocalDateTime(s string) *models.LocalTime {
	return models.FromTimePtr(p.parseDateTime(s))
}

// parseLocalDate — обёртка parseDate → *models.LocalTime
func (p *LKParser) parseLocalDate(s string) *models.LocalTime {
	return models.FromTimePtr(p.parseDate(s))
}

// parseInt парсит целое число
func (p *LKParser) parseInt(numStr string) *int {
	if numStr == "" {
		return nil
	}
	cleanStr := strings.ReplaceAll(numStr, " ", "")
	num, err := strconv.Atoi(cleanStr)
	if err != nil {
		return nil
	}
	return &num
}

// parseProstCh извлекает ЧАСЫ из «сутки:часы:минуты» (2-й элемент)
func (p *LKParser) parseProstCh(prostChStr string) *int {
	if prostChStr == "" {
		return nil
	}
	parts := strings.Split(prostChStr, ":")
	if len(parts) >= 2 {
		hours, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err == nil {
			return &hours
		}
	}
	return nil
}

// parseProstMin извлекает МИНУТЫ из «сутки:часы:минуты» (3-й элемент)
func (p *LKParser) parseProstMin(prostStr string) *int {
	if prostStr == "" {
		return nil
	}
	parts := strings.Split(prostStr, ":")
	if len(parts) >= 3 {
		mins, err := strconv.Atoi(strings.TrimSpace(parts[2]))
		if err == nil {
			return &mins
		}
	}
	return nil
}

// safeSubstring безопасно извлекает подстроку
func safeSubstring(s string, start, length int) string {
	if start >= len(s) {
		return ""
	}
	end := start + length
	if end > len(s) {
		end = len(s)
	}
	return s[start:end]
}

// containsSubstring проверяет, содержит ли строка подстроку
func containsSubstring(row []string, substr string) bool {
	for _, cell := range row {
		if strings.Contains(cell, substr) {
			return true
		}
	}
	return false
}

// ProcessLKFiles обрабатывает все LK файлы в директории
func (p *LKParser) ProcessLKFiles(ctx context.Context, lkDirPath string, processedBy string) error {
	records, err := p.ParseLKDirectory(lkDirPath)
	if err != nil {
		return fmt.Errorf("ошибка парсинга LK файлов: %w", err)
	}
	return p.dislocationService.ProcessDislocation(ctx, records, "lk_file", filepath.Base(lkDirPath), 0, processedBy)
}

// parseDateWithTimeAdjustmentDate парсит дату с корректировкой 18:00 → +1 день и возвращает только дату
func (p *LKParser) parseDateWithTimeAdjustmentDate(dateTimeStr string) *time.Time {
	if dateTimeStr == "" {
		return nil
	}
	t, err := time.Parse("02.01.2006 15:04", dateTimeStr)
	if err != nil {
		t, err = time.Parse("02.01.2006", dateTimeStr)
		if err != nil {
			return nil
		}
	}
	if t.Hour() >= 18 {
		t = t.Add(24 * time.Hour)
	}
	dateOnly := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	return &dateOnly
}

// ParseLKFiles парсит конкретный список файлов
func (p *LKParser) ParseLKFiles(filePaths []string) ([]models.Dislocation, error) {
	var allRecords []models.Dislocation
	for _, filePath := range filePaths {
		records, err := p.ParseLKFile(filePath)
		if err != nil {
			log.Printf("Ошибка парсинга файла %s: %v", filePath, err)
			continue
		}
		allRecords = append(allRecords, records...)
	}
	return allRecords, nil
}
