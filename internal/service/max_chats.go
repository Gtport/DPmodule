package service

import (
	"context"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
)

// MaxChatService — справочник чатов MAX и разрешение маршрутов рассылки форм.
// Отделяет «куда слать» от транспорта (adapter/max): рассылка (следующая ветка)
// спрашивает у сервиса список чатов по форме и терминалу, а сам HTTP не трогает.
type MaxChatService struct {
	repo port.MaxChatRepository
}

func NewMaxChatService(repo port.MaxChatRepository) *MaxChatService {
	return &MaxChatService{repo: repo}
}

// MaxChatDTO — чат для фронта/админ-списка. chat_id не отдаём: клиенту он не
// нужен (рассылку по форме+терминалу разрешает сервер), незачем светить id.
type MaxChatDTO struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	IsActive    bool   `json:"is_active"`
}

// Chats — все чаты (для GET /max/chats).
func (s *MaxChatService) Chats(ctx context.Context) ([]MaxChatDTO, error) {
	chats, err := s.repo.Chats(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]MaxChatDTO, 0, len(chats))
	for _, c := range chats {
		out = append(out, MaxChatDTO{Name: c.Name, Description: c.Description, IsActive: c.IsActive})
	}
	return out, nil
}

// ResolveChats — активные чаты для формы report и терминала terminal (пусто —
// сводная форма) в порядке маршрутов. Неактивные и несуществующие чаты
// пропускаются (маршрут может ссылаться на выключенный чат). Дедуп по коду:
// один чат в маршрутах формы не должен получить сообщение дважды.
//
// Возвращает доменные чаты С chat_id — они уходят в транспорт рассылки.
func (s *MaxChatService) ResolveChats(ctx context.Context, report, terminal string) ([]domain.MaxChat, error) {
	routes, err := s.repo.Routes(ctx, report, terminal)
	if err != nil {
		return nil, err
	}
	if len(routes) == 0 {
		return nil, nil
	}

	chats, err := s.repo.Chats(ctx)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]domain.MaxChat, len(chats))
	for _, c := range chats {
		byName[c.Name] = c
	}

	var out []domain.MaxChat
	seen := map[string]struct{}{}
	for _, r := range routes {
		if _, dup := seen[r.ChatName]; dup {
			continue
		}
		c, ok := byName[r.ChatName]
		if !ok || !c.IsActive || c.ChatID == "" {
			continue
		}
		seen[r.ChatName] = struct{}{}
		out = append(out, c)
	}
	return out, nil
}
