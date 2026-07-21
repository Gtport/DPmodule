package service

// VagonOpService — история продвижения вагона (запрос 601 к провайдеру АСУ).
//
// Автоматика (решение владельца): интервал всегда date_nach−1 день … сегодня
// (МСК); запрос при прибытии (→10), пропаже незавершённого рейса (запись-8) и
// выбытии прибывшего (исчез на станции назначения); каждая последующая история
// затирает предыдущую (ReplaceForTrip). Плюс ручной запрос из интерфейса.
//
// Групповые смены статусов (~200 вагонов за пересборку) не бьют по провайдеру:
// конвейер лишь складывает заявки в таблицу-очередь vagon_op_request (upsert по
// trip_key — дедуп), а фоновый воркер разгребает пачками с паузой между
// HTTP-запросами. Очередь в БД переживает рестарты/деплои.

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/parser"
	"github.com/Gtport/DPmodule/internal/port"
)

// Причины заявки (vagon_op_request.reason).
const (
	VagonOpReasonArrival  = "arrival"  // прибытие: переход в статус 10
	VagonOpReasonMissing  = "missing"  // пропажа незавершённого рейса (запись-8)
	VagonOpReasonDeparted = "departed" // выбытие прибывшего: статус-10 исчез из батча
	VagonOpReasonManual   = "manual"   // запрос оператора из интерфейса
)

// Пороги подтверждены владельцем: пачка 50 заявок за тик, пауза 500 мс между
// запросами (~2 мин на 200 вагонов), 5 неудач — заявка снимается.
const (
	defaultVagonOpBatch       = 50
	defaultVagonOpPause       = 500 * time.Millisecond
	defaultVagonOpMaxAttempts = 5
)

var ErrVagonNotFound = fmt.Errorf("вагон не найден в текущем снимке")

type VagonOpService struct {
	repo   port.VagonOperationRepository
	client port.WagonHistoryClient
	dir    *DirectoryCache
	actual *ActualCache
	log    *zap.Logger

	batch       int
	pause       time.Duration
	maxAttempts int

	mu sync.Mutex // один проход воркера за раз (тик + ручной не пересекаются)
}

func NewVagonOpService(repo port.VagonOperationRepository, client port.WagonHistoryClient, dir *DirectoryCache, actual *ActualCache, log *zap.Logger) *VagonOpService {
	if log == nil {
		log = zap.NewNop()
	}
	return &VagonOpService{
		repo: repo, client: client, dir: dir, actual: actual, log: log,
		batch: defaultVagonOpBatch, pause: defaultVagonOpPause, maxAttempts: defaultVagonOpMaxAttempts,
	}
}

// SetLimits — пороги воркера из конфига (0 → дефолт).
func (s *VagonOpService) SetLimits(batch int, pause time.Duration, maxAttempts int) {
	if batch > 0 {
		s.batch = batch
	}
	if pause > 0 {
		s.pause = pause
	}
	if maxAttempts > 0 {
		s.maxAttempts = maxAttempts
	}
}

// EnqueueTransitions — вызывается конвейером ДО подмены снимка (actual — прежний):
// собирает заявки по трём переходам и одним заходом кладёт в очередь. Возвращает
// число заявок; ошибки очереди не валят пересборку (решает вызывающий).
func (s *VagonOpService) EnqueueTransitions(ctx context.Context, kept []domain.Dislocation, actual *ActualCache) (int, error) {
	now := clock.Now()
	var reqs []domain.VagonOpRequest

	seen := make(map[string]struct{}, len(kept))
	for i := range kept {
		r := &kept[i]
		if r.Vagon == "" {
			continue
		}
		seen[r.Vagon] = struct{}{}
		if r.Status == nil || *r.Status != 10 {
			continue
		}
		prev, ok := actual.FindVagonInActual(r.Vagon)
		if !ok || prev.Status == nil || *prev.Status != 10 {
			if q, ok := s.requestFor(r, VagonOpReasonArrival, 0, now); ok {
				reqs = append(reqs, q)
			}
		}
	}

	for _, v := range actual.All() {
		if v.Vagon == "" {
			continue
		}
		if _, present := seen[v.Vagon]; present {
			continue
		}
		st := 0
		if v.Status != nil {
			st = *v.Status
		}
		switch {
		case st == 10: // прибыл и исчез на станции назначения
			if q, ok := s.requestFor(&v, VagonOpReasonDeparted, 0, now); ok {
				reqs = append(reqs, q)
			}
		case st == 6 || st == 12: // штатное выбытие завершённого — истории не надо
		default: // 0,1,2,4,5,9 — пропажа незавершённого рейса (путь записи-8)
			if q, ok := s.requestFor(&v, VagonOpReasonMissing, 0, now); ok {
				reqs = append(reqs, q)
			}
		}
	}

	if len(reqs) == 0 {
		return 0, nil
	}
	if err := s.repo.Enqueue(ctx, reqs); err != nil {
		// Трейл — вторичные данные: пересборку снимка отказ очереди не валит.
		s.log.Warn("601: постановка заявок не удалась", zap.Int("count", len(reqs)), zap.Error(err))
		return 0, err
	}
	return len(reqs), nil
}

// requestFor строит заявку по записи снимка: trip_key из вагона+даты начала
// рейса, клиент провайдера — из реестра ports по ОКПО грузополучателя (вагон в
// базе РЖД строго за грузополучателем; naznach переставляют — он не годится).
func (s *VagonOpService) requestFor(r *domain.Dislocation, reason string, priority int, now domain.LocalTime) (domain.VagonOpRequest, bool) {
	key, ok := domain.TripKeyOf(r.Vagon, r.DateNach)
	if !ok {
		return domain.VagonOpRequest{}, false
	}
	client := s.clientForOkpo(r.GruzpolOkpo)
	if client == "" {
		s.log.Debug("601: клиент провайдера не определён — заявка пропущена",
			zap.String("vagon", r.Vagon), zap.String("okpo", r.GruzpolOkpo))
		return domain.VagonOpRequest{}, false
	}
	t := r.DateNach.Time()
	dateOnly := domain.LocalTime(time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC))
	return domain.VagonOpRequest{
		TripKey: key, Vagon: r.Vagon, DateNachD: dateOnly,
		Client: client, Reason: reason, Priority: priority,
		CreatedAt: now, UpdatedAt: now,
	}, true
}

func (s *VagonOpService) clientForOkpo(okpo string) string {
	n, err := strconv.ParseInt(okpo, 10, 64)
	if err != nil {
		return ""
	}
	ports, ok := s.dir.PortsByOkpo(n)
	if !ok {
		return ""
	}
	for _, p := range ports {
		if p.ProviderClient != "" {
			return p.ProviderClient
		}
	}
	return ""
}

// DrainQueue — тик фонового воркера: пачка заявок, последовательные запросы с
// паузой. Ошибка одной заявки не останавливает остальные (attempts++/снятие).
func (s *VagonOpService) DrainQueue(ctx context.Context) error {
	if !s.mu.TryLock() {
		return nil // предыдущий проход ещё идёт
	}
	defer s.mu.Unlock()

	reqs, err := s.repo.NextBatch(ctx, s.batch)
	if err != nil {
		return fmt.Errorf("очередь 601: %w", err)
	}
	if len(reqs) == 0 {
		return nil
	}
	done, failed := 0, 0
	for i, q := range reqs {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(s.pause):
			}
		}
		if err := s.fetchStore(ctx, q); err != nil {
			failed++
			s.log.Warn("601: запрос истории не удался",
				zap.String("vagon", q.Vagon), zap.Int("attempts", q.Attempts+1), zap.Error(err))
			if ferr := s.repo.Fail(ctx, q.TripKey, err.Error(), s.maxAttempts, clock.Now()); ferr != nil {
				s.log.Warn("601: фиксация неудачи", zap.Error(ferr))
			}
			continue
		}
		done++
		if cerr := s.repo.Complete(ctx, q.TripKey); cerr != nil {
			s.log.Warn("601: снятие заявки", zap.Error(cerr))
		}
	}
	left, _ := s.repo.QueueSize(ctx)
	s.log.Info("601: проход очереди",
		zap.Int("done", done), zap.Int("failed", failed), zap.Int("left", left))
	return nil
}

// fetchStore — один запрос 601: интервал date_nach−1 день … сегодня (МСК)
// включительно, разбор и полная перезапись трейла рейса.
func (s *VagonOpService) fetchStore(ctx context.Context, q domain.VagonOpRequest) error {
	from := q.DateNachD.Time().AddDate(0, 0, -1).Format("2006-01-02")
	to := clock.Now().Time().Format("2006-01-02")
	raw, err := s.client.PullWagonHistory(ctx, q.Client, q.Vagon, from, to)
	if err != nil {
		return err
	}
	ops, err := parser.Parse601(raw)
	if err != nil {
		return err
	}
	return s.repo.ReplaceForTrip(ctx, q.TripKey, ops)
}

// RequestNow — ручной запрос оператора из интерфейса: синхронно (оператор сразу
// видит результат), мимо очереди. Вагон ищется в текущем снимке.
func (s *VagonOpService) RequestNow(ctx context.Context, vagon string) ([]domain.VagonOperation, error) {
	r, ok := s.actual.FindVagonInActual(vagon)
	if !ok {
		return nil, ErrVagonNotFound
	}
	q, ok := s.requestFor(&r, VagonOpReasonManual, 10, clock.Now())
	if !ok {
		return nil, fmt.Errorf("вагон %s: не определить рейс или клиента провайдера", vagon)
	}
	if err := s.fetchStore(ctx, q); err != nil {
		return nil, err
	}
	return s.repo.OperationsByTrip(ctx, q.TripKey)
}

// Operations — сохранённый трейл текущего рейса вагона (без запроса к провайдеру).
func (s *VagonOpService) Operations(ctx context.Context, vagon string) ([]domain.VagonOperation, error) {
	r, ok := s.actual.FindVagonInActual(vagon)
	if !ok {
		return nil, ErrVagonNotFound
	}
	key, ok := domain.TripKeyOf(r.Vagon, r.DateNach)
	if !ok {
		return nil, fmt.Errorf("вагон %s: не определить рейс", vagon)
	}
	return s.repo.OperationsByTrip(ctx, key)
}
