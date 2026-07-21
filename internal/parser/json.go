package parser

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

// JSONParser разбирает выгрузку дислокации АСУ РЖД (SPV4664) в []domain.Dislocation.
// Чистое преобразование: без БД и обогащения. JSON отдаёт голые коды — их и кладём;
// имена станций/дорог/грузоотправителя и IndexMain проставит обогащение позже.
type JSONParser struct{}

func NewJSONParser() *JSONParser { return &JSONParser{} }

// jsonResponse — обёртка старого формата эндпоинта (data.getReferenceSPV4664Response).
type jsonResponse struct {
	Status string `json:"status"`
	Data   struct {
		GetReferenceSPV4664Response struct {
			Vagons []jsonVagon `json:"vagons"`
		} `json:"getReferenceSPV4664Response"`
	} `json:"data"`
}

// jsonEnvelope — новый формат ответа АСУ: метка формирования и count в теле,
// массив вагонов под ключом "wagons" (не "vagons"). Count — interface{}, т.к.
// источник шлёт его то числом (264), то строкой ("264").
type jsonEnvelope struct {
	Count     interface{} `json:"count"`
	Timestamp string      `json:"timestamp"`
	Wagons    []jsonVagon `json:"wagons"`
}

// ParseResult — записи + метаданные конверта нового формата: метка формирования
// (тело.timestamp) и заявленный count (тело.count). Для форматов без метаданных
// FormationTS/DeclaredCount = nil. Слой приёма использует их для проверки свежести
// и целостности (len(Records) против DeclaredCount).
type ParseResult struct {
	Records       []domain.Dislocation
	FormationTS   *domain.LocalTime
	DeclaredCount *int
}

// jsonVagon — только поля источника, которые реально маппятся в Dislocation.
type jsonVagon struct {
	NOM_VAG     string `json:"NOM_VAG"`
	NOM_NAK     string `json:"NOM_NAK"`
	INDEX_POEZD string `json:"INDEX_POEZD"`

	DATE_NACH string `json:"DATE_NACH"`
	DATE_OTPR string `json:"DATE_OTPR"`
	STAN_NACH string `json:"STAN_NACH"`
	STR_NACH  string `json:"STR_NACH"`
	STAN_NAZN string `json:"STAN_NAZN"`
	DOR_NAZN  string `json:"DOR_NAZN"`
	STR_NAZN  string `json:"STR_NAZN"`
	STAN_OP   string `json:"STAN_OP"`

	GRUZOTPR_OKPO string `json:"GRUZOTPR_OKPO"`
	GRUZPOL_OKPO  string `json:"GRUZPOL_OKPO"`

	KOD_GRZ_UCH  string      `json:"KOD_GRZ_UCH"`
	KOD_GRZ_GNG  string      `json:"KOD_GRZ_GNG"`
	KOD_GRZ_VYGR string      `json:"KOD_GRZ_VYGR"`
	VES_GRZ      interface{} `json:"VES_GRZ"`
	PPV_POR      string      `json:"PPV_POR"`

	DATE_OP string `json:"DATE_OP"`
	KOP_VMD string `json:"KOP_VMD"`

	ID_OTPRK string      `json:"ID_OTPRK"`
	UNO      string      `json:"UNO"`
	NPP_VAG  interface{} `json:"NPP_VAG"`

	RASST_STAN_NAZN interface{} `json:"RASST_STAN_NAZN"`
	RASST_OB        string      `json:"RASST_OB"`
	RASST_STAN_OP   string      `json:"RASST_STAN_OP"`

	PROST_DN  interface{} `json:"PROST_DN"`
	PROST_CH  string      `json:"PROST_CH"`
	PROST_MIN string      `json:"PROST_MIN"`

	DATE_DOSTAV string `json:"DATE_DOSTAV"`
	DATE_PRIB   string `json:"DATE_PRIB"`

	ROD_VAG_UCH string `json:"ROD_VAG_UCH"`

	INV_CLAIM_NUMBER   string `json:"INV_CLAIM_NUMBER"`
	FREIGHT_EXACT_NAME string `json:"FREIGHT_EXACT_NAME"`
	FREIGHT_GTD_NUMBER string `json:"FREIGHT_GTD_NUMBER"`
	CAR_OWNER_NAME     string `json:"CAR_OWNER_NAME"`
	CAR_OWNER_OKPO     string `json:"CAR_OWNER_OKPO"`
	CAR_TENANT_NAME    string `json:"CAR_TENANT_NAME"`
	CAR_TENANT_OKPO    string `json:"CAR_TENANT_OKPO"`

	// Доверенное лицо (третий набор сведений о собственнике). В текущем фиде
	// провайдера ключей ещё нет; документация обещает camelCase (carTrustedOKPO),
	// остальные ключи фида — UPPER_SNAKE. Маппим оба варианта, берём непустой.
	CAR_TRUSTED_NAME string `json:"CAR_TRUSTED_NAME"`
	CAR_TRUSTED_OKPO string `json:"CAR_TRUSTED_OKPO"`
	CarTrustedNameCC string `json:"carTrustedName"`
	CarTrustedOkpoCC string `json:"carTrustedOKPO"`
}

// ParseBytes разбирает сырой JSON (плоский массив вагонов ИЛИ обёртка
// JSONResponse) в записи дислокации. Битые записи логировать здесь нельзя (парсер
// без побочных эффектов) — они пропускаются, а счётчик пропусков вернуть отдельно
// незачем: формат стабилен, ошибка конвертации возможна лишь при пустом vagon.
func (p *JSONParser) ParseBytes(raw []byte) ([]domain.Dislocation, error) {
	r, err := p.Parse(raw)
	return r.Records, err
}

// Parse разбирает сырой JSON в записи + метаданные конверта. Поддерживает три
// формата: плоский массив вагонов; обёртку SPV4664
// (data.getReferenceSPV4664Response.vagons); новый конверт {count, timestamp,
// wagons}. Метаданные (метка формирования, count) заполняются только для нового
// формата — из тела ответа.
func (p *JSONParser) Parse(raw []byte) (ParseResult, error) {
	vagons, ts, count, err := extractEnvelope(raw)
	if err != nil {
		return ParseResult{}, err
	}
	records := make([]domain.Dislocation, 0, len(vagons))
	for _, v := range vagons {
		records = append(records, p.convert(v))
	}
	return ParseResult{Records: records, FormationTS: ts, DeclaredCount: count}, nil
}

// ParseFile читает и разбирает один JSON-файл.
func (p *JSONParser) ParseFile(filePath string) ([]domain.Dislocation, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("открытие JSON %q: %w", filePath, err)
	}
	defer f.Close()

	raw, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("чтение JSON %q: %w", filePath, err)
	}
	return p.ParseBytes(raw)
}

// ProcessDirectory разбирает все *.json из подкаталога dj указанной директории.
// Ошибка отдельного файла не прерывает обработку остальных (собирается в err).
func (p *JSONParser) ProcessDirectory(dirPath string) ([]domain.Dislocation, error) {
	files, err := filepath.Glob(filepath.Join(dirPath, "dj", "*.json"))
	if err != nil {
		return nil, fmt.Errorf("поиск JSON-файлов: %w", err)
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

// extractEnvelope определяет формат по содержимому и возвращает вагоны + метаданные
// конверта (метка формирования, заявленный count) там, где они есть:
//   - плоский массив верхнего уровня → без метаданных;
//   - новый конверт {count, timestamp, wagons} → с метаданными из тела;
//   - обёртка SPV4664 (data.getReferenceSPV4664Response.vagons) → без метаданных.
func extractEnvelope(raw []byte) ([]jsonVagon, *domain.LocalTime, *int, error) {
	if firstNonSpace(raw) == '[' {
		var vagons []jsonVagon
		if err := json.Unmarshal(raw, &vagons); err != nil {
			return nil, nil, nil, fmt.Errorf("парсинг плоского JSON-массива: %w", err)
		}
		return vagons, nil, nil, nil
	}

	// Новый формат: распознаём по наличию его полей (count/timestamp/wagons).
	// Обёртка SPV4664 в эту структуру распарсится «пустой» → guard отсечёт её.
	var env jsonEnvelope
	if err := json.Unmarshal(raw, &env); err == nil &&
		(len(env.Wagons) > 0 || env.Count != nil || env.Timestamp != "") {
		return env.Wagons, parseLocalDateTime(env.Timestamp), parseIntPtrFromAny(env.Count), nil
	}

	// Старая обёртка SPV4664.
	var resp jsonResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, nil, nil, fmt.Errorf("парсинг JSON: %w", err)
	}
	if resp.Status != "" && resp.Status != "success" {
		return nil, nil, nil, fmt.Errorf("JSON response status is not success: %s", resp.Status)
	}
	return resp.Data.GetReferenceSPV4664Response.Vagons, nil, nil, nil
}

// firstNonSpace возвращает первый непробельный байт (0, если такого нет).
func firstNonSpace(raw []byte) byte {
	for _, b := range raw {
		if b == ' ' || b == '\n' || b == '\r' || b == '\t' {
			continue
		}
		return b
	}
	return 0
}

// firstNonEmpty — первое непустое значение (для полей с двумя вариантами ключа).
func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// cleanNone обрезает пробелы и трактует строковый null-сентинел "None"/"null"
// (встречается в новом формате) как пустую строку.
func cleanNone(s string) string {
	s = strings.TrimSpace(s)
	if s == "None" || s == "null" {
		return ""
	}
	return s
}

// convert конвертирует одну запись выгрузки в domain.Dislocation.
func (p *JSONParser) convert(v jsonVagon) domain.Dislocation {
	var r domain.Dislocation

	// Идентификаторы
	r.Vagon = cleanNone(v.NOM_VAG)
	r.Invoice = normalizeCyrillic(cleanNone(v.NOM_NAK))

	// Станции / коды (имена станций и дорог даст обогащение)
	r.CodeStationNach = cleanNone(v.STAN_NACH)
	r.CodeStanNazn = cleanNone(v.STAN_NAZN)
	r.CodeStationOper = cleanNone(v.STAN_OP)
	r.StrNach = cleanNone(v.STR_NACH)
	r.DorogaNazn = cleanNone(v.DOR_NAZN)
	r.StrNazn = cleanNone(v.STR_NAZN)

	// ОКПО (имена проставит обогащение)
	r.GruzotprOkpo = cleanNone(v.GRUZOTPR_OKPO)
	r.GruzpolOkpo = cleanNone(v.GRUZPOL_OKPO)

	// Груз
	r.CodeCargo = cleanNone(v.KOD_GRZ_UCH)
	r.CodeCargoGng = cleanNone(v.KOD_GRZ_GNG)
	r.CodeCargoVygr = cleanNone(v.KOD_GRZ_VYGR)
	r.PorozhPriznak = cleanNone(v.PPV_POR)
	if w, err := parseWeight(v.VES_GRZ); err == nil && w > 0 {
		t := w / 1000.0 // источник в килограммах
		r.Ves = &t
	}

	// Операция
	r.TimeOp = parseLocalDateTime(v.DATE_OP)
	r.DateOp = r.TimeOp
	r.CodeOper = cleanNone(v.KOP_VMD)

	// Индекс (IndexMain заполнит обогащение)
	r.Index = parseJSONIndex(v.INDEX_POEZD, r.CodeStationNach, r.CodeStationOper)

	// Идентификаторы отправки / порядок
	r.IdOtprk = cleanNone(v.ID_OTPRK)
	r.Uno = cleanNone(v.UNO)
	if npp, err := parseIntFromAny(v.NPP_VAG); err == nil && npp > 0 {
		r.NppVag = &npp
	}

	// Расстояния
	if d, err := parseIntFromAny(v.RASST_STAN_NAZN); err == nil && d > 0 {
		r.RasstStanNazn = &d
	}
	r.RasstOb = parseIntPtrFromAny(v.RASST_OB)
	r.RasstStanOp = parseIntPtrFromAny(v.RASST_STAN_OP)

	// Простой
	if d, err := parseIntFromAny(v.PROST_DN); err == nil && d > 0 {
		r.ProstDn = &d
	}
	r.ProstCh = parseProstCh(v.PROST_CH)
	r.ProstMin = parseIntPtrFromAny(v.PROST_MIN)

	// Даты (LocalTime, без таймзоны)
	dateNachT := parseJSONDateWithTimeAdjustment(v.DATE_NACH) // *time.Time для ID
	r.DateNach = domain.FromTimePtr(dateNachT)
	r.DateOtpr = parseLocalDateTime(v.DATE_OTPR)
	r.DatePrib = parseLocalDateTime(v.DATE_PRIB)
	r.DateDostav = parseLocalDate(v.DATE_DOSTAV)

	// Род вагона (код)
	r.RodVagUch = cleanNone(v.ROD_VAG_UCH)

	// Новые поля эндпоинта
	r.Zayavka = cleanNone(v.INV_CLAIM_NUMBER) // заявка ГУ-12
	r.FreightExactName = cleanNone(v.FREIGHT_EXACT_NAME)
	r.GtdNumber = cleanNone(v.FREIGHT_GTD_NUMBER)
	r.CarOwnerName = cleanNone(v.CAR_OWNER_NAME)
	r.CarOwnerOkpo = cleanNone(v.CAR_OWNER_OKPO)
	r.CarTenantName = cleanNone(v.CAR_TENANT_NAME)
	r.CarTenantOkpo = cleanNone(v.CAR_TENANT_OKPO)
	r.CarTrustedName = cleanNone(firstNonEmpty(v.CAR_TRUSTED_NAME, v.CarTrustedNameCC))
	r.CarTrustedOkpo = cleanNone(firstNonEmpty(v.CAR_TRUSTED_OKPO, v.CarTrustedOkpoCC))

	// Служебное
	r.ID = generateDeterministicID(r.Vagon, r.CodeStationNach, dateNachT)
	now := clock.Now() // московское naive-время (§3.11), не time.Now()
	r.CreatedAt = now
	r.UpdatedAt = now

	return r
}

// ── JSON-специфичные хелперы ───────────────────────────────────────────────

func parseWeight(weight interface{}) (float64, error) {
	if weight == nil {
		return 0, fmt.Errorf("weight is nil")
	}
	switch v := weight.(type) {
	case float64:
		return v, nil
	case string:
		clean := strings.ReplaceAll(v, " ", "")
		clean = strings.ReplaceAll(clean, ",", ".")
		return strconv.ParseFloat(clean, 64)
	default:
		return 0, fmt.Errorf("unknown weight type: %T", weight)
	}
}

func parseIntFromAny(value interface{}) (int, error) {
	if value == nil {
		return 0, fmt.Errorf("value is nil")
	}
	switch v := value.(type) {
	case int:
		return v, nil
	case float64:
		return int(v), nil
	case string:
		clean := strings.ReplaceAll(v, " ", "")
		if clean == "" {
			return 0, fmt.Errorf("empty")
		}
		return strconv.Atoi(clean)
	default:
		return 0, fmt.Errorf("unknown value type: %T", value)
	}
}

func parseIntPtrFromAny(value interface{}) *int {
	n, err := parseIntFromAny(value)
	if err != nil {
		return nil
	}
	return &n
}

func parseLocalDateTime(s string) *domain.LocalTime { return domain.FromTimePtr(parseJSONDateTime(s)) }
func parseLocalDate(s string) *domain.LocalTime     { return domain.FromTimePtr(parseJSONDate(s)) }

var jsonDateTimeFormats = []string{
	"2006-01-02T15:04:05.000",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05.000",
	"2006-01-02 15:04:05",
}

// parseJSONDateWithTimeAdjustment — правило «час ≥ 18 → +1 сутки», возвращает только дату.
func parseJSONDateWithTimeAdjustment(s string) *time.Time {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	for _, f := range jsonDateTimeFormats {
		if t, err := time.Parse(f, strings.TrimSpace(s)); err == nil {
			if t.Hour() >= 18 {
				t = t.Add(24 * time.Hour)
			}
			d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
			return &d
		}
	}
	return nil
}

// parseJSONDateTime — дата-время как есть (без корректировки).
func parseJSONDateTime(s string) *time.Time {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	for _, f := range jsonDateTimeFormats {
		if t, err := time.Parse(f, strings.TrimSpace(s)); err == nil {
			return &t
		}
	}
	return nil
}

// parseJSONDate — только дата (отрезает часть после T/пробела).
func parseJSONDate(s string) *time.Time {
	clean := strings.TrimSpace(s)
	if clean == "" {
		return nil
	}
	if i := strings.IndexAny(clean, "T "); i >= 0 {
		clean = clean[:i]
	}
	for _, f := range []string{"2006-01-02", "02.01.2006"} {
		if t, err := time.Parse(f, clean); err == nil {
			d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
			return &d
		}
	}
	return nil
}

// formattedIndexRe — индекс, уже отформатированный источником (новый контракт
// АСУ, dislocation_contract.example.json: INDEX_POEZD = "1234-567-8901").
var formattedIndexRe = regexp.MustCompile(`^\d{4}-\d{3}-\d{4}$`)

// FormatTrainIndex — нормализация индекса поезда для показа (трейл продвижения,
// запрос 601): 15 цифр → XXXX-XXX-XXXX, «Б/И» — пусто либо не 15 цифр. Проверок
// по станциям здесь нет: в операции 601 станций отправления/операции индекса нет,
// а сам индекс хранится сырым (нормализуем на выходе, старые строки не трогаем).
func FormatTrainIndex(indexStr string) string {
	return parseJSONIndex(indexStr, "", "")
}

// parseJSONIndex форматирует 15-значный INDEX_POEZD в XXXX-XXX-XXXX. «Б/И» —
// без индекса (вагон не в поезде) либо индекс не проходит проверки. Уже
// отформатированный источником индекс 4-3-4 принимается как есть.
func parseJSONIndex(indexStr, codeStationNach, codeStationOper string) string {
	if indexStr == "" {
		return "Б/И"
	}
	if s := strings.TrimSpace(indexStr); formattedIndexRe.MatchString(s) {
		return s
	}
	clean := regexp.MustCompile(`\D`).ReplaceAllString(indexStr, "")
	if len(clean) != 15 {
		return "Б/И"
	}

	nach := strings.TrimSpace(codeStationNach)
	oper := strings.TrimSpace(codeStationOper)
	if nach != "" && oper != "" && nach == oper {
		if clean[:6] != nach {
			return "Б/И"
		}
		if clean[9:15] == nach {
			return "Б/И"
		}
	}

	first := safeSubstring(clean[:6], 0, 4)
	second := safeSubstring(clean[6:9], 0, 3)
	third := safeSubstring(clean[9:15], 0, 4)
	if first == "" || second == "" || third == "" {
		return "Б/И"
	}
	return fmt.Sprintf("%s-%s-%s", first, second, third)
}

// parseProstCh — простой в часах: берёт первый элемент до «:».
func parseProstCh(s string) *int {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ":")
	if h, err := strconv.Atoi(strings.TrimSpace(parts[0])); err == nil {
		return &h
	}
	return nil
}
