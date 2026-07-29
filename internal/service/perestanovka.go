package service

// Отчёты «Перестановки» страницы «Справки и отчёты» (перенос gtport
// TransferExportButton + RearrangementFact):
//   - Excel — текущие перестановки на терминал книгой .xlsx из RAM-снимка
//     (книгу собирает internal/report, терминал — из реестра ports);
//   - Fact — факт перестановок за период из vagon_history: строки с
//     gruzpol_s ≠ naznach за период по дате прибытия либо выгрузки,
//     опциональный срез по терминалу-цели. Excel факта собирает фронт
//     (как в «Истории движения вагона») — таблица и так уезжает в браузер.

import (
	"context"
	"fmt"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
	"github.com/Gtport/DPmodule/internal/report"
)

type PerestanovkaService struct {
	actual *ActualCache
	dir    *DirectoryCache
	hist   port.HistoryRepository
}

func NewPerestanovkaService(actual *ActualCache, dir *DirectoryCache, hist port.HistoryRepository) *PerestanovkaService {
	return &PerestanovkaService{actual: actual, dir: dir, hist: hist}
}

// Excel — книга «Перестановка на {терминал}» из текущего снимка.
func (s *PerestanovkaService) Excel(terminal string) ([]byte, string, error) {
	if _, ok := s.dir.PortByNameS(terminal); !ok {
		return nil, "", fmt.Errorf("неизвестный терминал: %s", terminal)
	}
	return report.PerestanovkaXLSX(s.actual.All(), terminal)
}

// PerestanovkaFactRow — строка отчёта «Факт перестановок» (раскладка gtport
// RearrangementFact; «Собственник» = owner, «Марка»/«ГТД» — как в повагонке).
// «Соответствие» (naznach = place_vigr) вычисляет фронт.
type PerestanovkaFactRow struct {
	Vagon       string            `json:"vagon"`
	InvoiceMain string            `json:"invoice_main,omitempty"`
	Invoice     string            `json:"invoice,omitempty"`
	IndexMain   string            `json:"index_main,omitempty"`
	IndexPp     string            `json:"index_pp,omitempty"`
	DateNachD   *domain.LocalTime `json:"date_nach_d,omitempty"`
	StationNach string            `json:"station_nach,omitempty"`
	Gruzotpr    string            `json:"gruzotpr,omitempty"`
	GruzpolS    string            `json:"gruzpol_s,omitempty"`
	Naznach     string            `json:"naznach,omitempty"`
	CargoS      string            `json:"cargo_s,omitempty"`
	Ves         *float64          `json:"ves,omitempty"`
	Client      string            `json:"client,omitempty"`
	DateDostav  *domain.LocalTime `json:"date_dostav,omitempty"`
	DatePrib    *domain.LocalTime `json:"date_prib,omitempty"`
	PlanJd      *domain.LocalTime `json:"plan_jd,omitempty"`
	Delay       *int              `json:"delay,omitempty"`
	DateVigr    *domain.LocalTime `json:"date_vigr,omitempty"`
	PlaceVigr   string            `json:"place_vigr,omitempty"`
	Frost       *int              `json:"frost,omitempty"`
	Owner       string            `json:"owner,omitempty"`
	Marka       string            `json:"marka,omitempty"` // freight_exact_name
	Gtd         string            `json:"gtd,omitempty"`   // gtd_number
	Shipments   string            `json:"shipments,omitempty"`
}

type PerestanovkaFactDTO struct {
	From  string                `json:"from"`
	To    string                `json:"to"`
	Rows  []PerestanovkaFactRow `json:"rows"`
	Total int                   `json:"total"`
}

// Fact — факт перестановок за период [from; to] (yyyy-MM-dd, дефолты как в
// «Истории прибывших»: вчера—сегодня). byVigr — период по дате выгрузки, иначе
// по дате прибытия. terminal — срез по терминалу-цели (naznach); пусто — все.
func (s *PerestanovkaService) Fact(ctx context.Context, fromS, toS string, byVigr bool, terminal string) (PerestanovkaFactDTO, error) {
	if terminal != "" {
		if _, ok := s.dir.PortByNameS(terminal); !ok {
			return PerestanovkaFactDTO{}, fmt.Errorf("неизвестный терминал: %s", terminal)
		}
	}
	from, to, err := arrivalsRange(fromS, toS)
	if err != nil {
		return PerestanovkaFactDTO{}, err
	}
	rows, err := s.hist.PerestanovkaRows(ctx, from, to, byVigr)
	if err != nil {
		return PerestanovkaFactDTO{}, err
	}
	out := make([]PerestanovkaFactRow, 0, len(rows))
	for i := range rows {
		h := &rows[i]
		if terminal != "" && h.Naznach != terminal {
			continue
		}
		out = append(out, PerestanovkaFactRow{
			Vagon: h.Vagon, InvoiceMain: h.InvoiceMain, Invoice: h.Invoice,
			IndexMain: h.IndexMain, IndexPp: h.IndexPp, DateNachD: h.DateNachD,
			StationNach: h.StationNach, Gruzotpr: h.Gruzotpr,
			GruzpolS: h.GruzpolS, Naznach: h.Naznach,
			CargoS: h.CargoS, Ves: h.Ves, Client: h.Client,
			DateDostav: h.DateDostav, DatePrib: h.DatePrib, PlanJd: h.PlanJd,
			Delay: h.Delay, DateVigr: h.DateVigr, PlaceVigr: h.PlaceVigr,
			Frost: h.Frost, Owner: h.Owner,
			Marka: h.FreightExactName, Gtd: h.GtdNumber, Shipments: h.Shipments,
		})
	}
	return PerestanovkaFactDTO{
		From: from.String()[:10], To: to.String()[:10],
		Rows: out, Total: len(out),
	}, nil
}
