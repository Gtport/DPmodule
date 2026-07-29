package service

// Отчёт «Погрузка» страницы «Справки и отчёты» (перенос gtport LoadingReport +
// LoadingReportPeriod — обе формы сидели на одном SQL по vagon_history, здесь
// одна ручка и одна форма с двумя видами). Бэкенд отдаёт дневные агрегаты,
// раскладку (сводка по терминалам / пивот «дата × отправитель») строит фронт —
// как в gtport, где пивот тоже собирал клиент.
//
// Отходы от gtport (решения владельца 29.07.2026):
//   - перегрузы исключены из погрузки (peregruz непустой — TARGET.md §3.17);
//   - разбивка «уголь/металл» — по cargo_group из словаря грузов, а не по
//     клиенту «ЕВРАЗ ТК», зашитому в SQL (HARDCODE_INVENTORY §6.2); заодно
//     уходит gtport-баг сравнения с «ЕВРАЗ» (итог всегда 0).

import (
	"context"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
)

// LoadingReportService — данные отчёта «Погрузка» из бизнес-истории.
type LoadingReportService struct {
	history port.HistoryRepository
}

func NewLoadingReportService(history port.HistoryRepository) *LoadingReportService {
	return &LoadingReportService{history: history}
}

// LoadingReportDTO — дневные агрегаты погрузки за период [from; to].
type LoadingReportDTO struct {
	From string                   `json:"from"` // yyyy-MM-dd
	To   string                   `json:"to"`
	Rows []domain.LoadingDailyRow `json:"rows"`
}

// Report — агрегаты погрузки за период (даты включительно).
func (s *LoadingReportService) Report(ctx context.Context, from, to domain.LocalTime) (LoadingReportDTO, error) {
	rows, err := s.history.LoadingDaily(ctx, from, to)
	if err != nil {
		return LoadingReportDTO{}, err
	}
	if rows == nil {
		rows = []domain.LoadingDailyRow{}
	}
	return LoadingReportDTO{
		From: from.String()[:10],
		To:   to.String()[:10],
		Rows: rows,
	}, nil
}
