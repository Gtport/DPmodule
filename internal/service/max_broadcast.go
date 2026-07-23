package service

import (
	"context"
	"time"

	"github.com/Gtport/DPmodule/internal/port"
)

// MaxBroadcastService — рассылка готовых форм в чаты MAX. «Куда слать» берёт из
// маршрутов (MaxChatService.ResolveChats), «как слать» — из транспорта
// (port.MessengerSender). Сам текст/картинку НЕ строит: как в gtport, форму
// собирает фронт (текст — formatTextForCopy, картинку — html-to-image), сюда
// приходит готовое. Это тонкий релей с разрешением адресатов и паузой между
// чатами (лимит API 30 rps).
type MaxBroadcastService struct {
	chats  *MaxChatService
	sender port.MessengerSender
	delay  time.Duration // пауза между отправками в разные чаты
}

func NewMaxBroadcastService(chats *MaxChatService, sender port.MessengerSender, delay time.Duration) *MaxBroadcastService {
	return &MaxBroadcastService{chats: chats, sender: sender, delay: delay}
}

// BroadcastResult — исход рассылки по чатам маршрута. Sent/Failed — по коду чата
// (max_chat.name): диспетчеру видно, куда ушло, а куда нет и почему.
type BroadcastResult struct {
	Chats  int               `json:"chats"`  // сколько чатов разрешил маршрут
	Sent   []string          `json:"sent"`   // коды чатов с успешной отправкой
	Failed map[string]string `json:"failed"` // код чата → текст ошибки
}

// AllFailed — рассылка была (чаты нашлись), но ни одна отправка не прошла.
func (r BroadcastResult) AllFailed() bool {
	return r.Chats > 0 && len(r.Sent) == 0
}

// SendText рассылает текст формы report для терминала terminal (пусто — сводная
// форма). Чаты берутся из маршрутов; при пустом маршруте возвращается результат
// с Chats=0 (не ошибка — это ненастроенный маршрут, видно в ответе). Отправки
// идут по очереди с паузой; ошибка одного чата не прерывает остальные.
func (s *MaxBroadcastService) SendText(ctx context.Context, report, terminal, text string) (BroadcastResult, error) {
	chats, err := s.chats.ResolveChats(ctx, report, terminal)
	if err != nil {
		return BroadcastResult{}, err
	}
	res := BroadcastResult{Chats: len(chats), Failed: map[string]string{}}
	for i, c := range chats {
		if i > 0 && s.delay > 0 {
			select {
			case <-ctx.Done():
				return res, ctx.Err()
			case <-time.After(s.delay):
			}
		}
		if err := s.sender.SendText(ctx, c.ChatID, text); err != nil {
			res.Failed[c.Name] = err.Error()
		} else {
			res.Sent = append(res.Sent, c.Name)
		}
	}
	return res, nil
}
