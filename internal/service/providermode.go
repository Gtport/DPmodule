package service

// ProviderModeService — режим источника данных провайдера rwgate: каким шлюзом
// он добывает дислокацию и историю ПРЯМО СЕЙЧАС. Провайдер отдаёт его ручкой
// GET /wagons/history/source (сделана им именно для внешних серверов — чтобы не
// дублировать его логику фолбэка АСУ↔ЛК), значения:
//
//	asu    — штатный режим, данные из АСУ-шлюза («АСУ-АСУ» на панели);
//	lk     — АСУ отказал, провайдер работает через робота ЛК РЖД («АСУ-ЛК»);
//	paused — оба источника лежат: АСУ отказал подряд, и последняя попытка ЛК
//	         тоже не удалась.
//
// Зачем нам это знать (решение владельца 02.09.2026): в режиме lk каждый запрос
// истории (601) гонит робота провайдера в кабинет РЖД — медленно, дорого и
// рискует блокировкой учётки кабинета, поэтому АВТОМАТИЧЕСКИЕ запросы истории
// (очередь vagon_op_request) при не-asu приостанавливаются, а ручной запрос
// диспетчера из интерфейса остаётся. Плюс режим показывается на панели
// «Статус системы».
//
// Ответ кэшируется на providerModeTTL: режим читают и статус-панель (раз в
// минуту с каждого открытого экрана), и воркер очереди (каждые 15 секунд) —
// без кэша провайдер получал бы шквал одинаковых запросов.

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
	"github.com/Gtport/DPmodule/pkg/logger"
)

// Значения режима. unknown — наше: провайдер не ответил или ответ не разобран.
// Для блокировки автоматики unknown равносилен «не asu»: неизвестный режим — не
// разрешение жечь запросы.
const (
	ProviderSourceASU     = "asu"
	ProviderSourceLK      = "lk"
	ProviderSourcePaused  = "paused"
	ProviderSourceUnknown = "unknown"
)

const (
	// providerModeTTL — срок годности кэша. Минута: столько же живёт тик
	// статус-панели, а смена режима провайдера — событие редкое (часы).
	providerModeTTL = time.Minute
	// providerModeTimeout — потолок одного похода. Короче общего таймаута
	// адаптера (30s): режим читается на пути ручки /dislocation/status, и
	// лежащий провайдер не должен подвешивать статус-панель надолго.
	providerModeTimeout = 5 * time.Second
)

// ProviderMode — текущий режим глазами потребителей.
type ProviderMode struct {
	Source    string           // asu | lk | paused | unknown
	OK        bool             // ответ получен и разобран
	CheckedAt domain.LocalTime // момент последнего похода к провайдеру (МСК)
}

// IsASU — можно ли автоматике ходить за историей.
func (m ProviderMode) IsASU() bool { return m.Source == ProviderSourceASU }

type ProviderModeService struct {
	client port.ProviderModeClient
	log    *zap.Logger
	ttl    time.Duration

	mu      sync.Mutex
	cur     ProviderMode
	fetched domain.LocalTime // когда кэш наполнен; нулевое — ещё ни разу
}

func NewProviderModeService(client port.ProviderModeClient, log *zap.Logger) *ProviderModeService {
	if log == nil {
		log = zap.NewNop()
	}
	return &ProviderModeService{client: client, log: log, ttl: providerModeTTL}
}

// Mode — текущий режим источника провайдера (из кэша, при протухании — свежий
// поход). Ошибка похода не отдаётся наверх, а сворачивается в Source=unknown:
// потребителям нужен режим, а не стек ошибки, и «не знаем» — тоже ответ.
func (s *ProviderModeService) Mode(ctx context.Context) ProviderMode {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := clock.Now()
	if !s.fetched.Time().IsZero() && now.Time().Sub(s.fetched.Time()) < s.ttl {
		return s.cur
	}

	ctx, cancel := context.WithTimeout(ctx, providerModeTimeout)
	defer cancel()

	next := ProviderMode{Source: ProviderSourceUnknown, CheckedAt: now}
	raw, err := s.client.PullProviderMode(ctx)
	if err == nil {
		if src, perr := parseProviderMode(raw); perr == nil {
			next.Source, next.OK = src, true
		} else {
			// Сам поход удался — сломан контракт. Транспорт такой отказ не
			// запишет (HTTP 200), поэтому говорим здесь.
			s.log.Warn("режим источника провайдера не разобран", logger.Comp(logger.CompDislocation),
				zap.Error(perr))
		}
	}

	if prev := s.cur.Source; prev != next.Source {
		// Смена режима — событие (часы работы в фолбэке), а не шум: пишем только
		// переходы, сами опросы идут в Debug транспортом.
		if next.Source == ProviderSourceASU {
			s.log.Info("провайдер вернулся в штатный режим АСУ-АСУ", logger.Comp(logger.CompDislocation),
				zap.String("was", prev))
		} else {
			s.log.Warn("провайдер сменил режим источника", logger.Comp(logger.CompDislocation),
				zap.String("was", prev), zap.String("now", next.Source),
				zap.String("подробности", "автоматические запросы истории приостановлены, ручные из интерфейса работают"))
		}
	}

	s.cur, s.fetched = next, now
	return s.cur
}

// parseProviderMode — разбор ответа /wagons/history/source. Штатно провайдер
// отдаёт JSON {"source":"asu"}, но принимаем и голый текст: форма ответа —
// его внутренняя кухня, и её смена не должна ронять наш индикатор.
// Неизвестное значение — ошибка, а не asu: непонятный ответ не разрешает
// автоматике работать.
func parseProviderMode(raw []byte) (string, error) {
	s := strings.TrimSpace(string(raw))
	if strings.HasPrefix(s, "{") {
		var payload struct {
			Source string `json:"source"`
		}
		if err := json.Unmarshal([]byte(s), &payload); err != nil {
			return "", err
		}
		s = payload.Source
	}
	switch strings.ToLower(strings.Trim(strings.TrimSpace(s), `"`)) {
	case ProviderSourceASU:
		return ProviderSourceASU, nil
	case ProviderSourceLK:
		return ProviderSourceLK, nil
	case ProviderSourcePaused:
		return ProviderSourcePaused, nil
	default:
		return "", &providerModeParseError{got: s}
	}
}

type providerModeParseError struct{ got string }

func (e *providerModeParseError) Error() string {
	return "неизвестный режим источника провайдера: " + e.got
}
