package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/port"
)

// lastUpdateLayout — формат курсора провайдера: "YYYY-MM-DD HH:MM:SS.sss".
const lastUpdateLayout = "2006-01-02 15:04:05.000"

// ReferenceService — забор памяток на подачу/уборку у внешнего провайдера.
//
// ЭТАП-ЗАГЛУШКА: данные принимаются и логируются, но НИКУДА НЕ СОХРАНЯЮТСЯ (ни в БД,
// ни в кэш) и не разбираются. Разбор JSON и хранение подключим позже; тогда же
// last_update станет настоящим курсором из поля LAST_UPDATE последнего ответа.
type ReferenceService struct {
	cl       port.ReferenceClient
	clients  []string
	interval time.Duration
	log      *zap.Logger
}

func NewReferenceService(cl port.ReferenceClient, clients []string, interval time.Duration, log *zap.Logger) *ReferenceService {
	return &ReferenceService{cl: cl, clients: clients, interval: interval, log: log}
}

// FetchByNumber — ручной забор памятки по номеру. Пока: получить, залогировать
// размер, вернуть его. Не сохраняем.
func (s *ReferenceService) FetchByNumber(ctx context.Context, number string) (int, error) {
	body, err := s.cl.ByNumber(ctx, number)
	if err != nil {
		return 0, err
	}
	s.log.Info("reference: памятка по номеру получена (не сохраняем)",
		zap.String("number", number), zap.Int("bytes", len(body)))
	return len(body), nil
}

// PullUpdates — крон-инкремент: по каждому клиенту забрать изменения с last_update
// и залогировать. last_update = «сейчас − interval» (курсор не храним на этом этапе).
// Не сохраняем. Клиенты независимы: ошибка одного не прерывает остальных (warn +
// продолжаем); если упал хоть один — вернём сводную ошибку после полного прохода.
func (s *ReferenceService) PullUpdates(ctx context.Context) error {
	lastUpdate := clock.Now().Time().Add(-s.interval).Format(lastUpdateLayout)
	var failed []string
	for _, cl := range s.clients {
		body, err := s.cl.Update(ctx, cl, lastUpdate)
		if err != nil {
			s.log.Warn("reference: клиент пропущен из-за ошибки забора",
				zap.String("client", cl), zap.String("last_update", lastUpdate), zap.Error(err))
			failed = append(failed, cl)
			continue
		}
		s.log.Info("reference: обновления памяток получены (не сохраняем)",
			zap.String("client", cl), zap.String("last_update", lastUpdate), zap.Int("bytes", len(body)))
	}
	if len(failed) > 0 {
		return fmt.Errorf("reference update: ошибки по клиентам: %s", strings.Join(failed, ", "))
	}
	return nil
}
