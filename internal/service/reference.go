package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/parser"
	"github.com/Gtport/DPmodule/internal/port"
	"github.com/Gtport/DPmodule/internal/report"
)

// lastUpdateLayout — формат курсора провайдера: "YYYY-MM-DD HH:MM:SS.sss".
const lastUpdateLayout = "2006-01-02 15:04:05.000"

// ReferenceService — памятки ГУ-45 на подачу/уборку: крон-инкремент у внешнего
// провайдера и разнос вех подачи/уборки по рейсам в vagon_history.
//
// В gtport эти вехи вбивали руками — Excel-файлом «Журнал движения». Здесь их
// ведёт крон: раз в pull_interval по каждому клиенту забираем изменения с
// сохранённого курсора, разбираем и раскладываем движком domain.ApplyPamyatki
// (правила выбора рейса и перезаписи — там же, с тестами).
//
// Сами памятки не храним: нужны не документы, а вехи рейса. Единственное
// служебное состояние — курсор last_update по клиентам (таблица pamyatka_cursor).
type ReferenceService struct {
	cl       port.ReferenceClient
	history  port.HistoryRepository
	cursors  port.PamyatkaCursorRepository
	journal  port.JournalRepository
	clients  []string
	interval time.Duration
	log      *zap.Logger
}

func NewReferenceService(
	cl port.ReferenceClient,
	history port.HistoryRepository,
	cursors port.PamyatkaCursorRepository,
	journal port.JournalRepository,
	clients []string,
	interval time.Duration,
	log *zap.Logger,
) *ReferenceService {
	return &ReferenceService{
		cl: cl, history: history, cursors: cursors, journal: journal,
		clients: clients, interval: interval, log: log,
	}
}

// PamyatkaPullResult — итог одного прохода по клиенту (для ручки и журнала).
type PamyatkaPullResult struct {
	Client   string         `json:"client"`
	Pamyatki int            `json:"pamyatki"` // разобрано памяток
	Vagons   int            `json:"vagons"`   // строк вагонов в них
	Applied  int            `json:"applied"`  // рейсов истории обновлено
	Skipped  int            `json:"skipped"`  // строк не легло ни на один рейс
	Reasons  map[string]int `json:"reasons"`  // причины пропуска: код → сколько
}

// FetchByNumber — ручной забор памятки по номеру у конкретного клиента. Пустой
// client → первый из настроенных (reference.clients). Номер уникален в пределах
// клиента: у чужого клиента тот же номер даёт другой документ.
//
// Отдаёт сырой ответ провайдера: это иной контракт, чем у инкремента (обёртка
// ГУ-45 с полем _decoded_doc), в vagon_history он не раскладывается. Ручка
// диагностическая — посмотреть исходный документ целиком.
func (s *ReferenceService) FetchByNumber(ctx context.Context, client, number string) ([]byte, error) {
	client, body, err := s.fetchRawByNumber(ctx, client, number)
	if err != nil {
		return nil, err
	}
	s.log.Info("reference: памятка по номеру получена",
		zap.String("client", client), zap.String("number", number), zap.Int("bytes", len(body)))
	return body, nil
}

// FetchExcelByNumber — та же памятка по номеру, но бланком ГУ-45 в Excel:
// сырой ответ разбирается [parser.ParseReferenceDocByNumber] (только искомый
// документ, попутчики по пачке не трогаются) и раскладывается по печатной
// форме. Возвращает содержимое книги и имя файла для выгрузки.
func (s *ReferenceService) FetchExcelByNumber(ctx context.Context, client, number string) ([]byte, string, error) {
	client, body, err := s.fetchRawByNumber(ctx, client, number)
	if err != nil {
		return nil, "", err
	}
	doc, err := parser.ParseReferenceDocByNumber(body, client, number)
	if err != nil {
		return nil, "", err
	}
	xlsx, err := report.PamyatkaGU45XLSX(doc)
	if err != nil {
		return nil, "", err
	}
	s.log.Info("reference: памятка по номеру выгружена в Excel",
		zap.String("client", client), zap.String("number", number),
		zap.Int("vagons", len(doc.Vagons)), zap.Int("bytes", len(xlsx)))
	return xlsx, report.FileName(client, number), nil
}

// fetchRawByNumber — общий забор по номеру: подстановка клиента по умолчанию
// и запрос к провайдеру. Возвращает выбранного клиента (он нужен для разбора:
// номер уникален лишь в его пределах) и тело ответа.
func (s *ReferenceService) fetchRawByNumber(ctx context.Context, client, number string) (string, []byte, error) {
	if client == "" {
		if len(s.clients) == 0 {
			return "", nil, errors.New("reference: клиент не задан и список reference.clients пуст")
		}
		client = s.clients[0]
	}
	body, err := s.cl.ByNumber(ctx, client, number)
	if err != nil {
		return "", nil, err
	}
	return client, body, nil
}

// PullUpdates — крон-инкремент по всем клиентам. Клиенты независимы: ошибка
// одного не прерывает остальных (warn + продолжаем), сводная ошибка возвращается
// после полного прохода.
func (s *ReferenceService) PullUpdates(ctx context.Context) error {
	_, err := s.PullUpdatesDetailed(ctx)
	return err
}

// PullUpdatesDetailed — то же, но с разбором по клиентам (для ручного триггера).
func (s *ReferenceService) PullUpdatesDetailed(ctx context.Context) ([]PamyatkaPullResult, error) {
	var (
		results []PamyatkaPullResult
		failed  []string
	)
	for _, cl := range s.clients {
		res, err := s.pullClient(ctx, cl)
		if err != nil {
			s.log.Warn("reference: клиент пропущен из-за ошибки",
				zap.String("client", cl), zap.Error(err))
			failed = append(failed, cl)
			continue
		}
		results = append(results, res)
	}
	if len(failed) > 0 {
		return results, fmt.Errorf("reference update: ошибки по клиентам: %s", strings.Join(failed, ", "))
	}
	return results, nil
}

// pullClient — один клиент: забрать с курсора, разобрать, разнести по рейсам,
// сдвинуть курсор.
//
// Курсор двигаем ТОЛЬКО после успешной записи в БД и только на непустое
// LAST_UPDATE: на пустой пачке провайдер отдаёт пустую строку, и запись её в
// курсор потеряла бы позицию.
func (s *ReferenceService) pullClient(ctx context.Context, client string) (PamyatkaPullResult, error) {
	res := PamyatkaPullResult{Client: client, Reasons: map[string]int{}}

	cursor, err := s.cursors.Get(ctx, client)
	if err != nil {
		return res, fmt.Errorf("reference (%s): чтение курсора: %w", client, err)
	}
	if cursor == "" {
		// Первый заход: курсора нет — берём окно в один интервал назад.
		cursor = clock.Now().Time().Add(-s.interval).Format(lastUpdateLayout)
	}

	body, err := s.cl.Update(ctx, client, cursor)
	if err != nil {
		return res, err
	}
	upd, err := parser.ParseReferenceUpdate(body, client)
	if err != nil {
		return res, err
	}
	res.Pamyatki = len(upd.Pamyatki)
	for _, p := range upd.Pamyatki {
		res.Vagons += len(p.Vagons)
	}

	if len(upd.Pamyatki) > 0 {
		if err := s.applyToHistory(ctx, client, upd.Pamyatki, &res); err != nil {
			return res, err
		}
	}

	if upd.LastUpdate != "" {
		if err := s.cursors.Set(ctx, client, upd.LastUpdate); err != nil {
			return res, fmt.Errorf("reference (%s): сохранение курсора: %w", client, err)
		}
	}

	s.log.Info("reference: инкремент памяток обработан",
		zap.String("client", client), zap.String("cursor", cursor),
		zap.String("next_cursor", upd.LastUpdate), zap.Int("pamyatki", res.Pamyatki),
		zap.Int("vagons", res.Vagons), zap.Int("applied", res.Applied),
		zap.Int("skipped", res.Skipped), zap.Any("reasons", res.Reasons))

	s.appendJournal(ctx, res)
	return res, nil
}

// applyToHistory — сердце разноса: собрать вагоны пачки, поднять их рейсы,
// решить движком и записать одной транзакцией.
func (s *ReferenceService) applyToHistory(ctx context.Context, client string, pamyatki []domain.Pamyatka, res *PamyatkaPullResult) error {
	vagons := uniqueVagons(pamyatki)
	trips, err := s.history.TripsForPamyatki(ctx, vagons)
	if err != nil {
		return fmt.Errorf("reference (%s): чтение рейсов: %w", client, err)
	}

	applies, skips := domain.ApplyPamyatki(pamyatki, trips)

	updates := make(map[string]map[string]any, len(applies))
	for _, a := range applies {
		updates[a.TripID] = a.Fields
	}
	if err := s.history.UpdateFieldsBatch(ctx, updates); err != nil {
		return fmt.Errorf("reference (%s): запись вех в историю: %w", client, err)
	}

	res.Applied = len(applies)
	res.Skipped = len(skips)
	for _, sk := range skips {
		res.Reasons[sk.Reason]++
	}
	return nil
}

// appendJournal — след в едином журнале событий. Пустые проходы не пишем, иначе
// журнал забьётся тиками крона (памятки приходят далеко не каждый час).
func (s *ReferenceService) appendJournal(ctx context.Context, res PamyatkaPullResult) {
	if s.journal == nil || res.Pamyatki == 0 {
		return
	}
	detail, err := json.Marshal(map[string]any{
		"pamyatki": res.Pamyatki, "vagons": res.Vagons,
		"applied": res.Applied, "skipped": res.Skipped, "reasons": res.Reasons,
	})
	if err != nil {
		s.log.Warn("reference: сборка detail журнала не удалась", zap.Error(err))
		return
	}
	ev := domain.JournalEvent{
		EventType: domain.EventPamyatkaPull,
		Source:    res.Client,
		Trigger:   domain.TriggerScheduled,
		Actor:     "cron",
		Detail:    detail,
	}
	if err := s.journal.Append(ctx, ev); err != nil {
		// Журнал — не критичный путь: вехи уже записаны, ронять проход из-за
		// него нельзя.
		s.log.Warn("reference: запись в журнал событий не удалась", zap.Error(err))
	}
}

// uniqueVagons — номера вагонов пачки без повторов, отсортированы для
// предсказуемого запроса (вагон легко попадает в несколько памяток).
func uniqueVagons(pamyatki []domain.Pamyatka) []string {
	seen := map[string]struct{}{}
	for _, p := range pamyatki {
		for _, v := range p.Vagons {
			if v.Vagon != "" {
				seen[v.Vagon] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
