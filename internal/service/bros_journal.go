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
)

// Особые коды бросания (бизнес-правило РЖД, перенос gtport). Не порт-хардкод —
// общероссийский классификатор; вынесены в именованные константы, чтобы правило
// «какой код что требует» лежало в одном месте.
const (
	brosCodeTempPlacement = "05" // платное размещение → нужны реквизиты заявки
	brosCodeNonAcceptance = "01" // неприём грузополучателем → нужен is_agreed
)

// BrosJournalCreate — запрос на создание записи журнала (даты строками YYYY-MM-DD,
// парсятся сервисом). CreatedBy подставляет хендлер из JWT.
type BrosJournalCreate struct {
	BrosID       string
	Reason       string
	Comment      string
	ZayavkaNomer string
	ZayavkaDate  string
	DatePod      string
	ReasonText   string
	IsAgreed     *bool
	PlanPod      string
	CreatedBy    string
}

// BrosBulkResult — итог массового сохранения журнала за сегодня.
type BrosBulkResult struct {
	Total  int `json:"total"`
	Saved  int `json:"saved"`
	Failed int `json:"failed"`
}

// BrosJournalService — журнал бросков: фиксация состояния оператором, массовое
// сохранение перед рассылкой. Пишет запись журнала и синхронизирует код/дату
// подъёма в снимок bros.
type BrosJournalService struct {
	journal port.BrosJournalRepository
	bros    port.BrosRepository
	codes   port.BrosReasonCodesRepository
}

func NewBrosJournalService(journal port.BrosJournalRepository, bros port.BrosRepository, codes port.BrosReasonCodesRepository) *BrosJournalService {
	return &BrosJournalService{journal: journal, bros: bros, codes: codes}
}

// Create создаёт (или перезаписывает за сегодня) запись журнала.
//
// Валидация: reason обязателен; код 05 — реквизиты заявки + причина; код 01 —
// is_agreed, а согласованный дополнительно реквизиты письма. Побочно обновляет
// bros.reason (и date_pod для кода 05) — для быстрого отображения в таблице.
func (s *BrosJournalService) Create(ctx context.Context, req BrosJournalCreate) (*domain.BrosJournalEntry, error) {
	if req.BrosID == "" {
		return nil, errors.New("bros_id обязателен")
	}
	code := normalizeBrosCode(req.Reason)
	if code == "" {
		return nil, errors.New("reason (код бросания) обязателен")
	}

	if code == brosCodeTempPlacement {
		if req.ZayavkaNomer == "" || req.ZayavkaDate == "" || req.DatePod == "" || req.ReasonText == "" {
			return nil, errors.New("для кода 05 обязательны номер заявки, дата заявки, дата подъёма и причина")
		}
	}
	if code == brosCodeNonAcceptance {
		if req.IsAgreed == nil {
			return nil, errors.New("для кода 01 обязательно указать: согласованное или несогласованное (is_agreed)")
		}
		if *req.IsAgreed && (req.ZayavkaNomer == "" || req.ZayavkaDate == "" || req.DatePod == "") {
			return nil, errors.New("для согласованного кода 01 обязательны номер и дата гарантийного письма и согласованная дата подъёма")
		}
	}

	b, err := s.bros.GetByID(ctx, req.BrosID)
	if err != nil {
		return nil, fmt.Errorf("проверка броска: %w", err)
	}
	if b == nil {
		return nil, fmt.Errorf("бросок %s не найден", req.BrosID)
	}

	now := clock.Now()
	entry := domain.BrosJournalEntry{
		BrosID:    req.BrosID,
		Date:      dateOnly(&now),
		Reason:    code,
		Comment:   req.Comment,
		CreatedAt: &now,
		CreatedBy: req.CreatedBy,
	}
	if req.ZayavkaNomer != "" {
		zn := req.ZayavkaNomer
		entry.ZayavkaNomer = &zn
	}
	if code == brosCodeNonAcceptance {
		entry.IsAgreed = req.IsAgreed
	}

	// reason_text: код 05 — из запроса (оператор вводит); иначе авто из справочника.
	if req.ReasonText != "" {
		rt := req.ReasonText
		entry.ReasonText = &rt
	} else if code != brosCodeTempPlacement {
		if desc := s.codeDescription(ctx, code); desc != "" {
			entry.ReasonText = &desc
		}
	}

	if req.ZayavkaDate != "" {
		d, err := parseBrosJournalDate(req.ZayavkaDate)
		if err != nil {
			return nil, fmt.Errorf("неверный формат zayavka_date (YYYY-MM-DD): %w", err)
		}
		entry.ZayavkaDate = d
	}
	var datePod *domain.LocalTime
	if req.DatePod != "" {
		d, err := parseBrosJournalDate(req.DatePod)
		if err != nil {
			return nil, fmt.Errorf("неверный формат date_pod (YYYY-MM-DD): %w", err)
		}
		entry.DatePod = d
		datePod = d
	}
	if req.PlanPod != "" {
		if d, err := parseBrosJournalDate(req.PlanPod); err == nil {
			entry.PlanPod = d // план не критичен — ошибку игнорируем
		}
	}

	id, err := s.journal.Upsert(ctx, entry)
	if err != nil {
		return nil, fmt.Errorf("сохранение записи журнала: %w", err)
	}
	entry.ID = id

	// Синхронизируем код (и дату подъёма для 05) в снимок bros. Не валим запись,
	// если апдейт снимка не удался — журнал уже сохранён.
	fields := map[string]any{"reason": code, "updated_at": now}
	if code == brosCodeTempPlacement && datePod != nil {
		fields["date_pod"] = datePod
	}
	_ = s.bros.Update(ctx, req.BrosID, fields)

	return &entry, nil
}

// History — вся история записей журнала для броска (новые первыми).
func (s *BrosJournalService) History(ctx context.Context, brosID string) ([]domain.BrosJournalEntry, error) {
	return s.journal.ByBrosID(ctx, brosID)
}

// BulkSave фиксирует состояние всех активных бросков в журнале на сегодня.
// Вызывается перед рассылкой отчёта в MAX. Для поезда с историей — копирует
// последнюю запись на сегодня (plan_pod освежается из bros.plan); без истории —
// создаёт первую из полей bros (created_by=system).
func (s *BrosJournalService) BulkSave(ctx context.Context) (BrosBulkResult, error) {
	active, err := s.bros.Active(ctx)
	if err != nil {
		return BrosBulkResult{}, fmt.Errorf("активные броски: %w", err)
	}
	res := BrosBulkResult{Total: len(active)}
	now := clock.Now()
	today := dateOnly(&now)

	for _, b := range active {
		latest, err := s.journal.Latest(ctx, b.ID)
		if err != nil {
			res.Failed++
			continue
		}
		e := domain.BrosJournalEntry{
			BrosID: b.ID, Date: today, CreatedAt: &now, PlanPod: b.Plan,
		}
		if latest == nil {
			e.Reason = b.Reason
			e.DatePod = b.DatePod
			e.CreatedBy = "system"
		} else {
			e.Reason = latest.Reason
			e.Comment = latest.Comment
			e.ZayavkaNomer = latest.ZayavkaNomer
			e.ZayavkaDate = latest.ZayavkaDate
			e.DatePod = latest.DatePod
			e.ReasonText = latest.ReasonText
			e.IsAgreed = latest.IsAgreed
			if latest.CreatedBy != "" {
				e.CreatedBy = latest.CreatedBy
			} else {
				e.CreatedBy = "system"
			}
		}
		if _, err := s.journal.Upsert(ctx, e); err != nil {
			res.Failed++
		} else {
			res.Saved++
		}
	}
	return res, nil
}

// codeDescription — расшифровка кода из справочника ("" если не найдена/ошибка).
func (s *BrosJournalService) codeDescription(ctx context.Context, code string) string {
	codes, err := s.codes.ReasonCodes(ctx)
	if err != nil {
		return ""
	}
	for _, c := range codes {
		if c.Code == code {
			return c.Description
		}
	}
	return ""
}

// normalizeBrosCode приводит код к двузначному виду: "5"→"05", "1"→"01", "22"→"22".
func normalizeBrosCode(s string) string {
	s = strings.TrimSpace(s)
	if len(s) == 1 {
		return "0" + s
	}
	return s
}

// parseBrosJournalDate — YYYY-MM-DD (или ISO с временем) → LocalTime (Московское naive).
func parseBrosJournalDate(s string) (*domain.LocalTime, error) {
	if len(s) > 10 {
		s = s[:10]
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil, err
	}
	lt := domain.LocalTime(t)
	return &lt, nil
}
