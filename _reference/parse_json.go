// server/internal/service/parse_json.go
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gtport/server/internal/models"

	"github.com/jmoiron/sqlx"
)

// JSONParser структура для парсинга JSON файлов дислокации (SPV4664).
//
// Принцип тот же, что и в ЛК-парсере: оба парсера дают ОДИН набор полей модели.
// JSON отдаёт голые коды — их и кладём. Поля, которые обогащение всё равно
// перезаписывает (DorogaNach/DorogaOper ← stations, Gruzotpr ← marka,
// Gruzpol ← ports, IndexMain ← Index), здесь НЕ парсятся.
type JSONParser struct {
	db                 *sqlx.DB
	dislocationService *DislocationService
}

func NewJSONParser(db *sqlx.DB, dislocationService *DislocationService) *JSONParser {
	return &JSONParser{
		db:                 db,
		dislocationService: dislocationService,
	}
}

// JSONResponse представляет полную структуру JSON ответа
type JSONResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    struct {
		GetReferenceSPV4664Response struct {
			ReferenceSPV4664 interface{} `json:"referenceSPV4664"`
			Amount           string      `json:"amount"`
			AmountRF         string      `json:"amountRF"`
			AmountNotRF      string      `json:"amountNotRF"`
			CodeTypeObject   string      `json:"codeTypeObject"`
			ReturnCode       string      `json:"returnCode"`
			ErrorCode        string      `json:"errorCode"`
			ErrorMessage     interface{} `json:"errorMessage"`
			Title            string      `json:"title"`
			DateIzm          interface{} `json:"dateIzm"`
			Vagons           []JSONVagon `json:"vagons"`
		} `json:"getReferenceSPV4664Response"`
	} `json:"data"`
}

// JSONVagon — только те поля источника, которые реально маппятся в Dislocation.
// Неиспользуемые поля выгрузки сюда не включены (парсить их в память незачем).
type JSONVagon struct {
	// Базовые / маршрут
	NOM_VAG     string `json:"NOM_VAG"`
	NOM_NAK     string `json:"NOM_NAK"`
	INDEX_POEZD string `json:"INDEX_POEZD"`

	// Отправление / назначение (коды)
	DATE_NACH string `json:"DATE_NACH"`
	DATE_OTPR string `json:"DATE_OTPR"`
	STAN_NACH string `json:"STAN_NACH"`
	STR_NACH  string `json:"STR_NACH"`
	STAN_NAZN string `json:"STAN_NAZN"`
	DOR_NAZN  string `json:"DOR_NAZN"`
	STR_NAZN  string `json:"STR_NAZN"`
	STAN_OP   string `json:"STAN_OP"`

	// ОКПО (имена проставит обогащение)
	GRUZOTPR_OKPO string `json:"GRUZOTPR_OKPO"`
	GRUZPOL_OKPO  string `json:"GRUZPOL_OKPO"`

	// Груз
	KOD_GRZ_UCH  string      `json:"KOD_GRZ_UCH"`
	KOD_GRZ_GNG  string      `json:"KOD_GRZ_GNG"`
	KOD_GRZ_VYGR string      `json:"KOD_GRZ_VYGR"`
	VES_GRZ      interface{} `json:"VES_GRZ"`
	PPV_POR      string      `json:"PPV_POR"`

	// Операция
	DATE_OP string `json:"DATE_OP"`
	KOP_VMD string `json:"KOP_VMD"`

	// Идентификаторы / порядок
	ID_OTPRK string      `json:"ID_OTPRK"`
	UNO      string      `json:"UNO"`
	NPP_VAG  interface{} `json:"NPP_VAG"`

	// Расстояния
	RASST_STAN_NAZN interface{} `json:"RASST_STAN_NAZN"`
	RASST_OB        string      `json:"RASST_OB"`
	RASST_STAN_OP   string      `json:"RASST_STAN_OP"`

	// Простой
	PROST_DN  interface{} `json:"PROST_DN"`
	PROST_CH  string      `json:"PROST_CH"`
	PROST_MIN string      `json:"PROST_MIN"`

	// Даты
	DATE_DOSTAV string `json:"DATE_DOSTAV"`
	DATE_PRIB   string `json:"DATE_PRIB"`

	// Прочее
	ROD_VAG_UCH string `json:"ROD_VAG_UCH"`

	// Новые поля эндпоинта
	INV_CLAIM_NUMBER   string `json:"INV_CLAIM_NUMBER"`
	FREIGHT_EXACT_NAME string `json:"FREIGHT_EXACT_NAME"`
	FREIGHT_GTD_NUMBER string `json:"FREIGHT_GTD_NUMBER"`
	CAR_OWNER_NAME     string `json:"CAR_OWNER_NAME"`
	CAR_OWNER_OKPO     string `json:"CAR_OWNER_OKPO"`
	CAR_TENANT_NAME    string `json:"CAR_TENANT_NAME"`
	CAR_TENANT_OKPO    string `json:"CAR_TENANT_OKPO"`
}

// ParseJSONFile парсит JSON файл и возвращает записи дислокации
func (p *JSONParser) ParseJSONFile(filePath string) ([]models.Dislocation, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("ошибка открытия JSON файла: %v", err)
	}
	defer file.Close()

	raw, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения JSON файла: %v", err)
	}

	vagons, err := p.extractVagons(raw)
	if err != nil {
		return nil, err
	}

	var records []models.Dislocation
	for _, jsonVagon := range vagons {
		record, err := p.convertJSONToDislocation(jsonVagon)
		if err != nil {
			log.Printf("Ошибка конвертации JSON записи: %v", err)
			continue
		}
		records = append(records, record)
	}

	log.Printf("[JSONParser] Успешно распаршено %d записей из %s", len(records), filePath)
	return records, nil
}

// extractVagons вытаскивает список вагонов из JSON, поддерживая два формата:
//   - плоский массив верхнего уровня:  [ {…}, {…} ]   (новый формат эндпоинта)
//   - обёртка JSONResponse: data.getReferenceSPV4664Response.vagons (старый)
//
// Формат определяется по первому непробельному символу.
func (p *JSONParser) extractVagons(raw []byte) ([]JSONVagon, error) {
	first := byte(0)
	for _, b := range raw {
		if b == ' ' || b == '\n' || b == '\r' || b == '\t' {
			continue
		}
		first = b
		break
	}

	// Плоский массив
	if first == '[' {
		var vagons []JSONVagon
		if err := json.Unmarshal(raw, &vagons); err != nil {
			return nil, fmt.Errorf("ошибка парсинга плоского JSON-массива: %v", err)
		}
		return vagons, nil
	}

	// Обёртка JSONResponse (обратная совместимость)
	var jsonResponse JSONResponse
	if err := json.Unmarshal(raw, &jsonResponse); err != nil {
		return nil, fmt.Errorf("ошибка парсинга JSON: %v", err)
	}
	if jsonResponse.Status != "" && jsonResponse.Status != "success" {
		return nil, fmt.Errorf("JSON response status is not success: %s", jsonResponse.Status)
	}
	return jsonResponse.Data.GetReferenceSPV4664Response.Vagons, nil
}

// convertJSONToDislocation конвертирует JSON запись в модель Dislocation
func (p *JSONParser) convertJSONToDislocation(jsonVagon JSONVagon) (models.Dislocation, error) {
	var record models.Dislocation

	// ── Идентификаторы ──────────────────────────────────────────────────────
	record.Vagon = strings.TrimSpace(jsonVagon.NOM_VAG)
	record.Invoice = normalizeCyrillic(strings.TrimSpace(jsonVagon.NOM_NAK))

	// ── Станции / коды (имена станций и дорог даст обогащение) ───────────────
	record.CodeStationNach = strings.TrimSpace(jsonVagon.STAN_NACH)
	record.CodeStanNazn = strings.TrimSpace(jsonVagon.STAN_NAZN)
	record.CodeStationOper = strings.TrimSpace(jsonVagon.STAN_OP)
	record.StrNach = strings.TrimSpace(jsonVagon.STR_NACH)
	record.DorogaNazn = strings.TrimSpace(jsonVagon.DOR_NAZN)
	record.StrNazn = strings.TrimSpace(jsonVagon.STR_NAZN)

	// ── ОКПО (имена грузоотправителя/грузополучателя проставит обогащение) ───
	record.GruzotprOkpo = strings.TrimSpace(jsonVagon.GRUZOTPR_OKPO)
	record.GruzpolOkpo = strings.TrimSpace(jsonVagon.GRUZPOL_OKPO)

	// ── Груз ────────────────────────────────────────────────────────────────
	record.CodeCargo = strings.TrimSpace(jsonVagon.KOD_GRZ_UCH)
	record.CodeCargoGng = strings.TrimSpace(jsonVagon.KOD_GRZ_GNG)
	record.CodeCargoVygr = strings.TrimSpace(jsonVagon.KOD_GRZ_VYGR)
	record.PorozhPriznak = strings.TrimSpace(jsonVagon.PPV_POR) // уже код «1»/«0»
	if weight, err := p.parseWeight(jsonVagon.VES_GRZ); err == nil && weight > 0 {
		t := weight / 1000.0
		record.Ves = &t
	}

	// ── Операция ────────────────────────────────────────────────────────────
	record.TimeOp = p.parseLocalDateTime(jsonVagon.DATE_OP)
	record.DateOp = record.TimeOp
	record.CodeOper = strings.TrimSpace(jsonVagon.KOP_VMD)

	// ── Индекс (Index из INDEX_POEZD; IndexMain заполнит обогащение) ─────────
	record.Index = p.parseJSONIndex(jsonVagon.INDEX_POEZD, record.CodeStationNach, record.CodeStationOper)

	// ── Идентификаторы отправки / порядок ───────────────────────────────────
	record.IdOtprk = strings.TrimSpace(jsonVagon.ID_OTPRK)
	record.Uno = strings.TrimSpace(jsonVagon.UNO) // в JSON уже 12 знаков
	if npp, err := p.parseInt(jsonVagon.NPP_VAG); err == nil && npp > 0 {
		record.NppVag = &npp
	}

	// ── Расстояния ──────────────────────────────────────────────────────────
	if r, err := p.parseInt(jsonVagon.RASST_STAN_NAZN); err == nil && r > 0 {
		record.RasstStanNazn = &r
	}
	record.RasstOb = p.parseIntPtr(jsonVagon.RASST_OB)
	record.RasstStanOp = p.parseIntPtr(jsonVagon.RASST_STAN_OP)

	// ── Простой ─────────────────────────────────────────────────────────────
	if d, err := p.parseInt(jsonVagon.PROST_DN); err == nil && d > 0 {
		record.ProstDn = &d
	}
	record.ProstCh = p.parseProstCh(jsonVagon.PROST_CH)
	record.ProstMin = p.parseIntPtr(jsonVagon.PROST_MIN)

	// ── Даты (LocalTime, без таймзоны) ──────────────────────────────────────
	dateNachT := p.parseJSONDateWithTimeAdjustment(jsonVagon.DATE_NACH) // *time.Time для ID
	record.DateNach = models.FromTimePtr(dateNachT)
	record.DateOtpr = p.parseLocalDateTime(jsonVagon.DATE_OTPR)
	record.DatePrib = p.parseLocalDateTime(jsonVagon.DATE_PRIB) // прибытие на ст. назначения — достоверная дата
	record.DateDostav = p.parseLocalDate(jsonVagon.DATE_DOSTAV)

	// ── Род вагона (код) — включено ─────────────────────────────────────────
	record.RodVagUch = strings.TrimSpace(jsonVagon.ROD_VAG_UCH)

	// ── Новые поля эндпоинта ────────────────────────────────────────────────
	record.Zayavka = strings.TrimSpace(jsonVagon.INV_CLAIM_NUMBER) // заявка ГУ-12
	record.FreightExactName = strings.TrimSpace(jsonVagon.FREIGHT_EXACT_NAME)
	record.GtdNumber = strings.TrimSpace(jsonVagon.FREIGHT_GTD_NUMBER)
	record.CarOwnerName = strings.TrimSpace(jsonVagon.CAR_OWNER_NAME)
	record.CarOwnerOkpo = strings.TrimSpace(jsonVagon.CAR_OWNER_OKPO)
	record.CarTenantName = strings.TrimSpace(jsonVagon.CAR_TENANT_NAME)
	record.CarTenantOkpo = strings.TrimSpace(jsonVagon.CAR_TENANT_OKPO)

	// ── Служебное ───────────────────────────────────────────────────────────
	record.ID = generateDeterministicID(record.Vagon, record.CodeStationNach, dateNachT)
	record.CreatedAt = models.LocalTime(time.Now())
	record.UpdatedAt = models.LocalTime(time.Now())

	return record, nil
}

// parseWeight парсит вес из interface{}
func (p *JSONParser) parseWeight(weight interface{}) (float64, error) {
	if weight == nil {
		return 0, fmt.Errorf("weight is nil")
	}
	switch v := weight.(type) {
	case float64:
		return v, nil
	case string:
		cleanStr := strings.ReplaceAll(v, " ", "")
		cleanStr = strings.ReplaceAll(cleanStr, ",", ".")
		return strconv.ParseFloat(cleanStr, 64)
	default:
		return 0, fmt.Errorf("unknown weight type: %T", weight)
	}
}

// parseInt парсит целое число из interface{} (строки с ведущими нулями допустимы)
func (p *JSONParser) parseInt(value interface{}) (int, error) {
	if value == nil {
		return 0, fmt.Errorf("value is nil")
	}
	switch v := value.(type) {
	case int:
		return v, nil
	case float64:
		return int(v), nil
	case string:
		cleanStr := strings.ReplaceAll(v, " ", "")
		if cleanStr == "" {
			return 0, fmt.Errorf("empty")
		}
		return strconv.Atoi(cleanStr)
	default:
		return 0, fmt.Errorf("unknown value type: %T", value)
	}
}

// parseIntPtr — *int (nil при пустом/ошибке; 0 — допустимое значение)
func (p *JSONParser) parseIntPtr(value interface{}) *int {
	n, err := p.parseInt(value)
	if err != nil {
		return nil
	}
	return &n
}

// parseLocalDateTime — *models.LocalTime из дата-времени без корректировки (как есть)
func (p *JSONParser) parseLocalDateTime(s string) *models.LocalTime {
	return models.FromTimePtr(p.parseJSONDateTime(s))
}

// parseLocalDate — *models.LocalTime из даты
func (p *JSONParser) parseLocalDate(s string) *models.LocalTime {
	return models.FromTimePtr(p.parseJSONDate(s))
}

// parseJSONDateWithTimeAdjustment парсит дату с правилом 18:00 → +1 день, возвращает только дату
func (p *JSONParser) parseJSONDateWithTimeAdjustment(dateTimeStr string) *time.Time {
	if dateTimeStr == "" {
		return nil
	}
	cleanDateStr := strings.TrimSpace(dateTimeStr)
	formats := []string{
		"2006-01-02T15:04:05.000",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05.000",
		"2006-01-02 15:04:05",
	}
	for _, format := range formats {
		t, err := time.Parse(format, cleanDateStr)
		if err == nil {
			if t.Hour() >= 18 {
				t = t.Add(24 * time.Hour)
			}
			dateOnly := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
			return &dateOnly
		}
	}
	return nil
}

// parseJSONDateTime парсит дату-время (как есть, без корректировки)
func (p *JSONParser) parseJSONDateTime(dateTimeStr string) *time.Time {
	if dateTimeStr == "" {
		return nil
	}
	cleanDateStr := strings.TrimSpace(dateTimeStr)
	formats := []string{
		"2006-01-02T15:04:05.000",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05.000",
		"2006-01-02 15:04:05",
	}
	for _, format := range formats {
		t, err := time.Parse(format, cleanDateStr)
		if err == nil {
			return &t
		}
	}
	return nil
}

// parseJSONDate парсит только дату (отрезает T/пробел)
func (p *JSONParser) parseJSONDate(dateStr string) *time.Time {
	if dateStr == "" {
		return nil
	}
	cleanDateStr := strings.TrimSpace(dateStr)
	if strings.Contains(cleanDateStr, "T") {
		if parts := strings.Split(cleanDateStr, "T"); len(parts) > 0 {
			cleanDateStr = parts[0]
		}
	}
	if strings.Contains(cleanDateStr, " ") {
		if parts := strings.Split(cleanDateStr, " "); len(parts) > 0 {
			cleanDateStr = parts[0]
		}
	}
	formats := []string{"2006-01-02", "02.01.2006"}
	for _, format := range formats {
		t, err := time.Parse(format, cleanDateStr)
		if err == nil {
			dateOnly := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
			return &dateOnly
		}
	}
	return nil
}

// parseJSONIndex парсит индекс поезда из 15-значной строки INDEX_POEZD
func (p *JSONParser) parseJSONIndex(indexStr, codeStationNach, codeStationOper string) string {
	if indexStr == "" {
		return "Б/И"
	}
	re := regexp.MustCompile(`\D`)
	cleanStr := re.ReplaceAllString(indexStr, "")
	if len(cleanStr) != 15 {
		return "Б/И"
	}

	cleanNach := strings.TrimSpace(codeStationNach)
	cleanOper := strings.TrimSpace(codeStationOper)

	if cleanNach != "" && cleanOper != "" && cleanNach == cleanOper {
		if cleanStr[:6] != cleanNach {
			return "Б/И"
		}
		if cleanStr[9:15] == cleanNach {
			return "Б/И"
		}
	}

	formattedFirst := safeSubstring(cleanStr[:6], 0, 4)
	formattedSecond := safeSubstring(cleanStr[6:9], 0, 3)
	formattedThird := safeSubstring(cleanStr[9:15], 0, 4)

	if formattedFirst == "" || formattedSecond == "" || formattedThird == "" {
		return "Б/И"
	}
	return fmt.Sprintf("%s-%s-%s", formattedFirst, formattedSecond, formattedThird)
}

// parseProstCh парсит время простоя — берёт первый элемент (формат JSON: «часы»)
func (p *JSONParser) parseProstCh(prostChStr string) *int {
	if prostChStr == "" {
		return nil
	}
	parts := strings.Split(prostChStr, ":")
	if len(parts) >= 1 {
		hours, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err == nil {
			return &hours
		}
	}
	return nil
}

// ProcessJSONDirectory обрабатывает все JSON файлы в директории
func (p *JSONParser) ProcessJSONDirectory(ctx context.Context, jsonDirPath string) ([]models.Dislocation, error) {
	jsonDir := filepath.Join(jsonDirPath, "dj")
	files, err := filepath.Glob(filepath.Join(jsonDir, "*.json"))
	if err != nil {
		return nil, fmt.Errorf("ошибка поиска JSON файлов: %v", err)
	}

	var allRecords []models.Dislocation
	for _, file := range files {
		records, err := p.ParseJSONFile(file)
		if err != nil {
			log.Printf("Ошибка парсинга файла %s: %v", file, err)
			continue
		}
		allRecords = append(allRecords, records...)
	}
	return allRecords, nil
}

// normalizeCyrillic заменяет латинские омоглифы на кириллические символы
func normalizeCyrillic(text string) string {
	if text == "" {
		return text
	}
	replacements := map[rune]rune{
		'A': 'А', 'B': 'В', 'C': 'С', 'E': 'Е', 'H': 'Н', 'K': 'К', 'M': 'М',
		'O': 'О', 'P': 'Р', 'T': 'Т', 'X': 'Х', 'Y': 'У',
		'a': 'а', 'c': 'с', 'e': 'е', 'o': 'о', 'p': 'р', 'x': 'х', 'y': 'у',
	}
	return strings.Map(func(r rune) rune {
		if replacement, exists := replacements[r]; exists {
			return replacement
		}
		return r
	}, text)
}
