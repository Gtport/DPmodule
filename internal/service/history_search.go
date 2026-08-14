package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
	"github.com/Gtport/DPmodule/internal/report"
)

// Экран «Работа с историческими данными» (перенос gtport OperatorToolsHistory,
// POST /history/universal). Вся выборка — из vagon_history; фильтры соединяются
// по И. Отходы от gtport (решения владельца 04.08.2026):
//   - серверные пагинация и сортировка (в gtport — захардкоженный limit 5000 без
//     листания, сортировал клиент по загруженному куску; в бою уже ~100 тыс.
//     записей — грузить всё нельзя);
//   - «не выгруж.» опирается на схему place_vigr NOT NULL DEFAULT '' (в gtport
//     NULL терялся);
//   - фильтр выгрузки — по date_vigr_d (ЖД-сутки), а не по timestamp date_vigr:
//     иначе последний день диапазона терял записи после 00:00;
//   - колонки: Собст = owner (не rod_vag_uch), Марка = freight_exact_name,
//     ГТД = gtd_number, «Перегруз» = peregruz (в gtport «Отгружен» ← info_3 —
//     подпись вводила в заблуждение) — прецедент «Поездов в движении» 31.07.2026;
//   - Excel строит сервер по ВСЕМУ фильтру (не по загруженной странице),
//     потолок HistoryExportMax с внятной ошибкой;
//   - словарь станций погрузки — DISTINCT по самой истории, не marka.
type HistorySearchService struct {
	repo port.HistoryRepository
	dir  *DirectoryCache
}

func NewHistorySearchService(repo port.HistoryRepository, dir *DirectoryCache) *HistorySearchService {
	return &HistorySearchService{repo: repo, dir: dir}
}

// ErrHistorySearchInvalid — ошибка валидации запроса; ручка отвечает 400 с
// текстом (внятным для диспетчера).
var ErrHistorySearchInvalid = errors.New("некорректный запрос истории")

// HistoryExportMax — потолок серверного Excel. 100 тыс. строк × 26 колонок
// StreamWriter собирает за секунды; больший набор — сигнал «сузьте фильтры».
const HistoryExportMax = 100_000

const (
	historyLimitDefault = 100
	historyLimitMax     = 1000
	historyListMax      = 10_000 // потолок списков вагонов/накладных/станций
)

// ── DTO (контракт фронта) ───────────────────────────────────────────────────

// HistorySearchFilterDTO — фильтр запроса; все поля опциональны.
type HistorySearchFilterDTO struct {
	DateNachDFrom string   `json:"date_nach_d_from"`
	DateNachDTo   string   `json:"date_nach_d_to"`
	DatePribDFrom string   `json:"date_prib_d_from"`
	DatePribDTo   string   `json:"date_prib_d_to"`
	DateVigrDFrom string   `json:"date_vigr_d_from"`
	DateVigrDTo   string   `json:"date_vigr_d_to"`
	GruzpolS      []string `json:"gruzpol_s"`
	Naznach       []string `json:"naznach"`
	PlaceVigr     []string `json:"place_vigr"`
	NotUnloaded   bool     `json:"not_unloaded"`
	Vagons        []string `json:"vagons"`
	Invoices      []string `json:"invoices"`
	StationNach   []string `json:"station_nach"`
	OnlyOverdue   bool     `json:"only_overdue"`
	// Только недоехавшие: рейсы, закрытые вручную удалением из пропавших
	// (vagon_history.not_arrived, миграция 000062).
	OnlyNotArrived bool `json:"only_not_arrived"`
}

type HistorySortDTO struct {
	By  string `json:"by"`
	Dir string `json:"dir"` // asc | desc
}

type HistoryPageDTO struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

type HistorySearchRequest struct {
	Filter HistorySearchFilterDTO `json:"filter"`
	Sort   HistorySortDTO         `json:"sort"`
	Page   HistoryPageDTO         `json:"page"`
}

// HistoryRowDTO — строка таблицы: 26 колонок экрана + id (ПКМ «История
// движения вагона» — vagon-trail-modal ждёт id строки vagon_history).
type HistoryRowDTO struct {
	ID          string            `json:"id"`
	Vagon       string            `json:"vagon"`
	InvoiceMain string            `json:"invoice_main"`
	Invoice     string            `json:"invoice"`
	IndexMain   string            `json:"index_main"`
	IndexPp     string            `json:"index_pp"`
	DateNachD   *domain.LocalTime `json:"date_nach_d"`
	StationNach string            `json:"station_nach"`
	Gruzotpr    string            `json:"gruzotpr"`
	GruzpolS    string            `json:"gruzpol_s"`
	Naznach     string            `json:"naznach"`
	CargoS      string            `json:"cargo_s"`
	Ves         *float64          `json:"ves"`
	Client      string            `json:"client"`
	Status      *int              `json:"status"`
	DateDostav  *domain.LocalTime `json:"date_dostav"`
	DatePribD   *domain.LocalTime `json:"date_prib_d"`
	PlanJd      *domain.LocalTime `json:"plan_jd"`
	Delay       *int              `json:"delay"`
	DateVigr    *domain.LocalTime `json:"date_vigr"`
	// ЖД-сутки выгрузки — рядом с МСК-фактом date_vigr, чтобы колонка
	// «Выгружен (ЖД)» была согласована с фильтром date_vigr_d (решение
	// владельца 14.08.2026: вечерняя выгрузка уходит в следующие ЖД-сутки,
	// одна дата на экране выглядела ошибкой фильтра).
	DateVigrD   *domain.LocalTime `json:"date_vigr_d"`
	PlaceVigr   string            `json:"place_vigr"`
	Frost       *int              `json:"frost"`
	Owner       string            `json:"owner"`
	Freight     string            `json:"freight"` // freight_exact_name (марка)
	GtdNumber   string            `json:"gtd_number"`
	Shipments   string            `json:"shipments"`
	Peregruz    string            `json:"peregruz"`
	NotArrived  bool              `json:"not_arrived"` // недоехавший (пометка в колонке «Статус»)
}

// HistorySearchDTO — страница выдачи (формат пагинации — канон bros).
type HistorySearchDTO struct {
	Rows    []HistoryRowDTO `json:"rows"`
	Total   int             `json:"total"`
	Limit   int             `json:"limit"`
	Offset  int             `json:"offset"`
	HasMore bool            `json:"has_more"`
}

// HistoryMetaDTO — данные панели фильтров: терминалы реестра с цветами и
// станции погрузки, встречающиеся в истории.
type HistoryMetaDTO struct {
	Targets  []TargetDTO `json:"targets"`
	Stations []string    `json:"stations"`
}

// historySortColumns — белый список сортировки: ключ API → колонка SQL.
// Единственное место, откуда имя колонки попадает в ORDER BY.
var historySortColumns = map[string]string{
	"vagon": "vagon", "invoice_main": "invoice_main", "invoice": "invoice",
	"index_main": "index_main", "index_pp": "index_pp",
	"date_nach_d": "date_nach_d", "station_nach": "station_nach",
	"gruzotpr": "gruzotpr", "gruzpol_s": "gruzpol_s", "naznach": "naznach",
	"cargo_s": "cargo_s", "ves": "ves", "client": "client", "status": "status",
	"date_dostav": "date_dostav", "date_prib_d": "date_prib_d",
	"plan_jd": "plan_jd", "delay": "delay", "date_vigr": "date_vigr",
	"date_vigr_d": "date_vigr_d",
	"place_vigr": "place_vigr", "frost": "frost", "owner": "owner",
	"freight": "freight_exact_name", "gtd_number": "gtd_number",
	"shipments": "shipments", "peregruz": "peregruz",
}

// ── Публичные методы ────────────────────────────────────────────────────────

// Search — страница истории по фильтру.
func (s *HistorySearchService) Search(ctx context.Context, req HistorySearchRequest) (HistorySearchDTO, error) {
	f, err := buildHistoryFilter(req.Filter)
	if err != nil {
		return HistorySearchDTO{}, err
	}
	col, desc, err := historySortColumn(req.Sort)
	if err != nil {
		return HistorySearchDTO{}, err
	}
	limit, offset, err := historyPage(req.Page)
	if err != nil {
		return HistorySearchDTO{}, err
	}

	rows, total, err := s.repo.SearchRows(ctx, f, col, desc, limit, offset)
	if err != nil {
		return HistorySearchDTO{}, err
	}
	out := make([]HistoryRowDTO, len(rows))
	for i := range rows {
		out[i] = toHistoryRowDTO(&rows[i])
	}
	return HistorySearchDTO{
		Rows: out, Total: total, Limit: limit, Offset: offset,
		HasMore: offset+limit < total,
	}, nil
}

// Meta — данные панели фильтров (терминалы + станции погрузки).
func (s *HistorySearchService) Meta(ctx context.Context) (HistoryMetaDTO, error) {
	stations, err := s.repo.DistinctStationsNach(ctx)
	if err != nil {
		return HistoryMetaDTO{}, err
	}
	return HistoryMetaDTO{Targets: terminalTargets(s.dir), Stations: stations}, nil
}

// Excel — книга по ВСЕМУ отфильтрованному набору (page запроса игнорируется).
// Потолок HistoryExportMax проверяется ДО сборки (COUNT), строки читаются
// курсором — набор в память целиком не поднимается.
func (s *HistorySearchService) Excel(ctx context.Context, req HistorySearchRequest) ([]byte, string, error) {
	f, err := buildHistoryFilter(req.Filter)
	if err != nil {
		return nil, "", err
	}
	col, desc, err := historySortColumn(req.Sort)
	if err != nil {
		return nil, "", err
	}
	_, total, err := s.repo.SearchRows(ctx, f, col, desc, 1, 0)
	if err != nil {
		return nil, "", err
	}
	if total == 0 {
		return nil, "", fmt.Errorf("%w: под фильтр не попало ни одной записи", ErrHistorySearchInvalid)
	}
	if total > HistoryExportMax {
		return nil, "", fmt.Errorf("%w: найдено %d строк — сузьте фильтры (потолок экспорта %d)",
			ErrHistorySearchInvalid, total, HistoryExportMax)
	}
	data, err := report.HistoryXLSX(func(fn func(domain.VagonHistory) error) error {
		return s.repo.IterateSearch(ctx, f, col, desc, fn)
	})
	if err != nil {
		return nil, "", err
	}
	name := fmt.Sprintf("история_вагонов_%s.xlsx", clock.Now().Time().Format("02.01.2006"))
	return data, name, nil
}

// ── Валидация и маппинг ─────────────────────────────────────────────────────

// buildHistoryFilter — DTO → доменный фильтр: разбор дат, свап перепутанных
// границ (дружелюбие arrivalsRange), нормализация списков с потолком.
func buildHistoryFilter(dto HistorySearchFilterDTO) (domain.HistorySearchFilter, error) {
	var f domain.HistorySearchFilter
	var err error
	if f.DateNachDFrom, f.DateNachDTo, err = historyRange("дата погрузки", dto.DateNachDFrom, dto.DateNachDTo); err != nil {
		return f, err
	}
	if f.DatePribDFrom, f.DatePribDTo, err = historyRange("дата прибытия", dto.DatePribDFrom, dto.DatePribDTo); err != nil {
		return f, err
	}
	if f.DateVigrDFrom, f.DateVigrDTo, err = historyRange("дата выгрузки", dto.DateVigrDFrom, dto.DateVigrDTo); err != nil {
		return f, err
	}
	if f.GruzpolS, err = historyList("gruzpol_s", dto.GruzpolS); err != nil {
		return f, err
	}
	if f.Naznach, err = historyList("naznach", dto.Naznach); err != nil {
		return f, err
	}
	if f.PlaceVigr, err = historyList("place_vigr", dto.PlaceVigr); err != nil {
		return f, err
	}
	if f.Vagons, err = historyList("vagons", dto.Vagons); err != nil {
		return f, err
	}
	if f.Invoices, err = historyList("invoices", dto.Invoices); err != nil {
		return f, err
	}
	if f.StationNach, err = historyList("station_nach", dto.StationNach); err != nil {
		return f, err
	}
	f.NotUnloaded = dto.NotUnloaded
	if f.NotUnloaded && len(f.PlaceVigr) > 0 {
		return f, fmt.Errorf("%w: «не выгруж.» несовместим со списком мест выгрузки", ErrHistorySearchInvalid)
	}
	f.OnlyOverdue = dto.OnlyOverdue
	f.OnlyNotArrived = dto.OnlyNotArrived
	// «Не выгруж.» и «просрочка» исключают недоехавших по построению — вместе
	// с «недоехавшие» дали бы гарантированно пустую выдачу, это ошибка запроса.
	if f.OnlyNotArrived && (f.NotUnloaded || f.OnlyOverdue) {
		return f, fmt.Errorf("%w: «недоехавшие» несовместим с «не выгруж.» и «просрочка»", ErrHistorySearchInvalid)
	}
	return f, nil
}

// historyRange — пара границ yyyy-MM-dd; перепутанные местами — свап.
func historyRange(field, fromS, toS string) (*domain.LocalTime, *domain.LocalTime, error) {
	parse := func(s string) (*domain.LocalTime, error) {
		if s == "" {
			return nil, nil
		}
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			return nil, fmt.Errorf("%w: %s — некорректная дата %q (ожидается yyyy-MM-dd)",
				ErrHistorySearchInvalid, field, s)
		}
		lt := domain.LocalTime(t)
		return &lt, nil
	}
	from, err := parse(fromS)
	if err != nil {
		return nil, nil, err
	}
	to, err := parse(toS)
	if err != nil {
		return nil, nil, err
	}
	if from != nil && to != nil && to.Time().Before(from.Time()) {
		from, to = to, from
	}
	return from, to, nil
}

// historyList — нормализация спискового фильтра: трим, пустые вон, потолок.
func historyList(field string, in []string) ([]string, error) {
	if len(in) > historyListMax {
		return nil, fmt.Errorf("%w: список %s длиннее %d элементов", ErrHistorySearchInvalid, field, historyListMax)
	}
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out, nil
}

// historySortColumn — колонка из белого списка; пустой запрос = дефолт экрана
// gtport (дата погрузки, новые сверху).
func historySortColumn(s HistorySortDTO) (col string, desc bool, err error) {
	by := s.By
	if by == "" {
		by = "date_nach_d"
	}
	col, ok := historySortColumns[by]
	if !ok {
		return "", false, fmt.Errorf("%w: сортировка по %q не поддерживается", ErrHistorySearchInvalid, s.By)
	}
	switch s.Dir {
	case "", "desc":
		desc = true
	case "asc":
		desc = false
	default:
		return "", false, fmt.Errorf("%w: направление сортировки %q (ожидается asc|desc)", ErrHistorySearchInvalid, s.Dir)
	}
	return col, desc, nil
}

// historyPage — limit с клампом [1..historyLimitMax] (дефолт historyLimitDefault),
// offset ≥ 0.
func historyPage(p HistoryPageDTO) (limit, offset int, err error) {
	limit = p.Limit
	if limit == 0 {
		limit = historyLimitDefault
	}
	if limit < 1 || limit > historyLimitMax {
		return 0, 0, fmt.Errorf("%w: limit %d вне диапазона [1..%d]", ErrHistorySearchInvalid, p.Limit, historyLimitMax)
	}
	if p.Offset < 0 {
		return 0, 0, fmt.Errorf("%w: offset не может быть отрицательным", ErrHistorySearchInvalid)
	}
	return limit, p.Offset, nil
}

func toHistoryRowDTO(h *domain.VagonHistory) HistoryRowDTO {
	return HistoryRowDTO{
		ID: h.ID, Vagon: h.Vagon, InvoiceMain: h.InvoiceMain, Invoice: h.Invoice,
		IndexMain: h.IndexMain, IndexPp: h.IndexPp, DateNachD: h.DateNachD,
		StationNach: h.StationNach, Gruzotpr: h.Gruzotpr, GruzpolS: h.GruzpolS,
		Naznach: h.Naznach, CargoS: h.CargoS, Ves: h.Ves, Client: h.Client,
		Status: h.Status, DateDostav: h.DateDostav, DatePribD: h.DatePribD,
		PlanJd: h.PlanJd, Delay: h.Delay, DateVigr: h.DateVigr,
		DateVigrD: h.DateVigrD, PlaceVigr: h.PlaceVigr, Frost: h.Frost, Owner: h.Owner,
		Freight: h.FreightExactName, GtdNumber: h.GtdNumber,
		Shipments: h.Shipments, Peregruz: h.Peregruz, NotArrived: h.NotArrived,
	}
}
