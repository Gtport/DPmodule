package parser

import (
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/Gtport/DPmodule/internal/domain"
)

// LKParser разбирает выгрузку дислокации из ЛК РЖД (SPV4664, Excel) в
// []domain.Dislocation. Как и JSON-парсер, кормит одну доменную модель и должен
// давать ТОТ ЖЕ набор полей: из ЛК берём только КОДЫ (из скобок «Имя (код)»),
// а имена дорог/грузоотправителя/грузополучателя и IndexMain проставит
// обогащение из справочников. Поля, которые обогащение всё равно перезаписывает
// (DorogaNach/DorogaOper, Gruzotpr, Gruzpol, IndexMain), здесь НЕ парсятся.
//
// Чистое преобразование: без БД, без обогащения, без логирования. Битые строки
// молча пропускаются (формат стабилен), файл/лист с ошибкой — тоже.
type LKParser struct {
	profile SourceProfile
}

// NewLKParser создаёт парсер ЛК с заданным профилем источника. Для поведения
// GTport «как есть» передавайте DefaultProfile().
func NewLKParser(profile SourceProfile) *LKParser {
	return &LKParser{profile: profile}
}

// предкомпилированные регэкспы (парсинг идёт построчно по тысячам строк).
var (
	reLKSixDigits      = regexp.MustCompile(`\((\d{6})\)`)
	reLKDigitsBrackets = regexp.MustCompile(`\((\d+)\)`)
	reLKParens         = regexp.MustCompile(`\(.*?\)`)
	reLKDigits         = regexp.MustCompile(`\d+`)
)

// ParseBytes разбирает содержимое xlsx-файла (все листы) в записи дислокации.
func (p *LKParser) ParseBytes(raw []byte) ([]domain.Dislocation, error) {
	f, err := excelize.OpenReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("чтение xlsx: %w", err)
	}
	defer f.Close()
	return p.parseWorkbook(f)
}

// ParseFile читает и разбирает один xlsx-файл ЛК.
func (p *LKParser) ParseFile(filePath string) ([]domain.Dislocation, error) {
	f, err := OpenXLSX(filePath)
	if err != nil {
		return nil, fmt.Errorf("открытие xlsx %q: %w", filePath, err)
	}
	defer f.Close()
	return p.parseWorkbook(f)
}

// ProcessDirectory разбирает все *.xlsx из указанной директории. Ошибка
// отдельного файла не прерывает обработку остальных (собирается в firstErr).
func (p *LKParser) ProcessDirectory(dirPath string) ([]domain.Dislocation, error) {
	files, err := filepath.Glob(filepath.Join(dirPath, "*.xlsx"))
	if err != nil {
		return nil, fmt.Errorf("поиск xlsx-файлов: %w", err)
	}
	var all []domain.Dislocation
	var firstErr error
	for _, file := range files {
		records, err := p.ParseFile(file)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		all = append(all, records...)
	}
	return all, firstErr
}

// parseWorkbook обходит все листы книги. Лист без строки заголовка пропускается.
func (p *LKParser) parseWorkbook(f *excelize.File) ([]domain.Dislocation, error) {
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("файл не содержит листов")
	}
	var all []domain.Dislocation
	for _, sheet := range sheets {
		records, err := p.parseSheet(f, sheet)
		if err != nil {
			continue // лист без нужного заголовка — не ошибка книги
		}
		all = append(all, records...)
	}
	return all, nil
}

// parseSheet разбирает один лист: ищет строку заголовка по маркеру из профиля,
// определяет индексы колонок и парсит строки данных.
func (p *LKParser) parseSheet(f *excelize.File, sheet string) ([]domain.Dislocation, error) {
	rows, err := f.GetRows(sheet)
	if err != nil {
		return nil, fmt.Errorf("чтение строк листа %q: %w", sheet, err)
	}

	headerRowIndex := -1
	var headerRow []string
	for i, row := range rows {
		if rowContains(row, p.profile.HeaderMarker) {
			headerRowIndex = i
			headerRow = row
			break
		}
	}
	if headerRowIndex == -1 {
		return nil, fmt.Errorf("не найдена строка заголовка %q", p.profile.HeaderMarker)
	}

	colIndexes := p.getColumnIndexes(headerRow)
	if len(colIndexes) == 0 {
		return nil, fmt.Errorf("не найдены необходимые заголовки столбцов")
	}

	var records []domain.Dislocation
	for i := headerRowIndex + 1; i < len(rows); i++ {
		if len(rows[i]) == 0 {
			continue
		}
		record := p.parseRow(rows[i], colIndexes)
		if record.Vagon != "" {
			records = append(records, record)
		}
	}
	return records, nil
}

// getColumnIndexes определяет индексы столбцов по заголовкам: сначала точные
// совпадения, затем частичные для недостающих (подстроки подобраны без пересечений).
func (p *LKParser) getColumnIndexes(headerRow []string) map[string]int {
	indexes := make(map[string]int)

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

	for colIndex, header := range headerRow {
		header = strings.TrimSpace(header)
		for pattern, identifier := range exactMatches {
			if strings.EqualFold(header, pattern) {
				indexes[identifier] = colIndex
				break
			}
		}
	}

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

// parseRow разбирает одну строку данных в domain.Dislocation.
func (p *LKParser) parseRow(row []string, colIndexes map[string]int) domain.Dislocation {
	var r domain.Dislocation

	getValue := func(key string) string {
		if index, exists := colIndexes[key]; exists && index < len(row) {
			return strings.TrimSpace(row[index])
		}
		return ""
	}

	// ── Ключевые поля + детерминированный ID ────────────────────────────────
	r.Vagon = getValue("vagon_c")
	dateNachT := p.parseDateWithTimeAdjustment(getValue("date_nach_c")) // *time.Time для ID
	r.CodeStationNach = p.extractSixDigits(getValue("code_station_nach_c"))
	r.ID = generateDeterministicID(r.Vagon, r.CodeStationNach, dateNachT)
	r.DateNach = domain.FromTimePtr(dateNachT)

	r.Invoice = getValue("invoice_c")

	// Станция операции — ДО parseIndex
	r.CodeStationOper = p.extractSixDigits(getValue("code_station_oper_c"))
	r.Index = p.parseIndex(getValue("index_c"), r.CodeStationNach, r.CodeStationOper)
	r.CodeStanNazn = p.extractSixDigits(getValue("code_stan_nazn_c"))

	// ОКПО (имена грузоотправителя/грузополучателя проставит обогащение)
	r.GruzotprOkpo = getValue("gruzotpr_okpo_c")
	r.GruzpolOkpo = getValue("gruzpol_okpo_c")

	// ── Груз: код ЕТСНГ из скобок (имя груза даст обогащение из marka) ───────
	r.CodeCargo = p.extractDigitsFromBrackets(getValue("code_cargo_c"))
	r.CodeCargoGng = getValue("code_cargo_gng_c") // голый код, без скобок
	r.CodeCargoVygr = p.extractDigitsFromBrackets(getValue("code_cargo_vygr_c"))

	r.Ves = p.parseWeight(getValue("ves_c"))
	r.PorozhPriznak = p.porozhToCode(getValue("porozh_c")) // слово → код «1»/«0»

	// ── Операция ────────────────────────────────────────────────────────────
	r.TimeOp = p.parseLocalDateTime(getValue("time_op_c"))
	r.DateOp = r.TimeOp
	r.CodeOper = p.extractDigitsFromBrackets(getValue("code_oper_c"))

	// ── Дороги/страны: только КОДЫ из скобок ────────────────────────────────
	// DorogaNach/DorogaOper НЕ парсим — их кладёт обогащение (stations.Road, имя).
	r.StrNach = p.extractDigitsFromBrackets(getValue("str_nach_c"))
	r.DorogaNazn = p.extractDigitsFromBrackets(getValue("doroga_nazn_c"))
	r.StrNazn = p.extractDigitsFromBrackets(getValue("str_nazn_c"))

	// ── Расстояния ──────────────────────────────────────────────────────────
	r.RasstStanNazn = p.parseInt(getValue("rasst_stan_nazn_c"))
	r.RasstOb = p.parseInt(getValue("rasst_ob_c"))
	r.RasstStanOp = p.parseInt(getValue("rasst_stan_op_c"))

	// ── Простой: сутки / часы / минуты ──────────────────────────────────────
	r.ProstDn = p.parseInt(getValue("prost_dn_c"))
	prostCompound := getValue("prost_ch_c") // «сутки:часы:минуты»
	r.ProstCh = p.parseProstCh(prostCompound)
	r.ProstMin = p.parseProstMin(prostCompound)

	r.NppVag = p.parseInt(getValue("npp_vag_c"))

	// ── Идентификаторы ──────────────────────────────────────────────────────
	r.IdOtprk = getValue("id_otprk_c")
	r.Uno = p.padUno(getValue("uno_c")) // паддинг нулями до 12 (Excel срезает нули)

	// ── Даты (LocalTime, без таймзоны) ──────────────────────────────────────
	r.DateDostav = p.parseLocalDate(getValue("date_dostav_c"))
	r.DateOtpr = p.parseLocalDateTime(getValue("date_otpr_c"))
	r.DatePrib = p.parseLocalDateTime(getValue("date_prib_c"))

	// ── Род вагона: код из скобок ───────────────────────────────────────────
	r.RodVagUch = p.extractDigitsFromBrackets(getValue("rod_vag_uch_c"))

	// Поля, которых нет в выгрузке ЛК (заполняются из JSON/эндпоинта):
	//   FreightExactName, GtdNumber, Zayavka (ГУ-12), CarOwnerName/Okpo, CarTenantName/Okpo.

	now := domain.LocalTime(time.Now())
	r.CreatedAt = now
	r.UpdatedAt = now

	return r
}

// ── ЛК-специфичные хелперы (методы, чтобы не пересекаться с JSON-парсером) ───

// parseIndex форматирует индекс поезда в XXXX-XXX-XXXX с проверкой на совпадение
// станции начала и операции. «Б/И» — без индекса либо не проходит проверки.
func (p *LKParser) parseIndex(indexStr, codeStationNach, codeStationOper string) string {
	if indexStr == "" {
		return "Б/И"
	}

	cleanStr := strings.TrimSpace(reLKParens.ReplaceAllString(indexStr, ""))
	parts := strings.Fields(cleanStr)
	if len(parts) < 3 {
		return "Б/И"
	}

	cleanNach := strings.TrimSpace(codeStationNach)
	cleanOper := strings.TrimSpace(codeStationOper)

	if cleanNach != "" && cleanOper != "" && cleanNach == cleanOper {
		firstPartDigits := reLKDigits.FindString(parts[0])
		if firstPartDigits != cleanNach {
			return "Б/И"
		}
		thirdPartDigits := reLKDigits.FindString(parts[2])
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

// extractSixDigits извлекает 6-значный код станции из скобок «Имя (123456)».
func (p *LKParser) extractSixDigits(text string) string {
	if text == "" {
		return ""
	}
	if m := reLKSixDigits.FindStringSubmatch(text); len(m) > 1 {
		return m[1]
	}
	return ""
}

// extractDigitsFromBrackets извлекает код (любой длины) из скобок «Имя (код)».
func (p *LKParser) extractDigitsFromBrackets(text string) string {
	if text == "" {
		return ""
	}
	if m := reLKDigitsBrackets.FindStringSubmatch(text); len(m) > 1 {
		return m[1]
	}
	return ""
}

// porozhToCode переводит «Тип парка (П/Г)» в код как в JSON (PPV_POR): 1/0.
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

// parseWeight парсит вес и конвертирует кг → тонны.
func (p *LKParser) parseWeight(weightStr string) *float64 {
	if weightStr == "" {
		return nil
	}
	clean := strings.ReplaceAll(weightStr, " ", "")
	clean = strings.ReplaceAll(clean, ",", ".")
	weight, err := strconv.ParseFloat(clean, 64)
	if err != nil {
		return nil
	}
	t := weight / 1000.0
	return &t
}

var lkDateTimeFormats = []string{
	"02.01.2006 15:04",
	"02.01.2006",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// parseDateTime парсит дату-время как есть (без корректировки).
func (p *LKParser) parseDateTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	for _, f := range lkDateTimeFormats {
		if t, err := time.Parse(f, s); err == nil {
			return &t
		}
	}
	return nil
}

// parseDate парсит только дату.
func (p *LKParser) parseDate(s string) *time.Time {
	if s == "" {
		return nil
	}
	for _, f := range []string{"02.01.2006", "2006-01-02"} {
		if t, err := time.Parse(f, s); err == nil {
			return &t
		}
	}
	return nil
}

func (p *LKParser) parseLocalDateTime(s string) *domain.LocalTime {
	return domain.FromTimePtr(p.parseDateTime(s))
}

func (p *LKParser) parseLocalDate(s string) *domain.LocalTime {
	return domain.FromTimePtr(p.parseDate(s))
}

// parseDateWithTimeAdjustment применяет правило «час ≥ DateCutoffHour → +1 сутки»
// и возвращает только дату (для детерминированного ID рейса).
func (p *LKParser) parseDateWithTimeAdjustment(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse("02.01.2006 15:04", s)
	if err != nil {
		if t, err = time.Parse("02.01.2006", s); err != nil {
			return nil
		}
	}
	if t.Hour() >= p.profile.DateCutoffHour {
		t = t.Add(24 * time.Hour)
	}
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	return &d
}

// parseInt парсит целое число (пробелы-разделители убираются).
func (p *LKParser) parseInt(numStr string) *int {
	if numStr == "" {
		return nil
	}
	clean := strings.ReplaceAll(numStr, " ", "")
	n, err := strconv.Atoi(clean)
	if err != nil {
		return nil
	}
	return &n
}

// parseProstCh извлекает ЧАСЫ из «сутки:часы:минуты» (2-й элемент).
func (p *LKParser) parseProstCh(s string) *int {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ":")
	if len(parts) >= 2 {
		if h, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
			return &h
		}
	}
	return nil
}

// parseProstMin извлекает МИНУТЫ из «сутки:часы:минуты» (3-й элемент).
func (p *LKParser) parseProstMin(s string) *int {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ":")
	if len(parts) >= 3 {
		if m, err := strconv.Atoi(strings.TrimSpace(parts[2])); err == nil {
			return &m
		}
	}
	return nil
}

// rowContains проверяет, содержит ли какая-либо ячейка строки подстроку substr.
func rowContains(row []string, substr string) bool {
	for _, cell := range row {
		if strings.Contains(cell, substr) {
			return true
		}
	}
	return false
}
