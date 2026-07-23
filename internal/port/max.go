package port

import (
	"context"

	"github.com/Gtport/DPmodule/internal/domain"
)

// MaxChatRepository — чтение справочника чатов MAX и маршрутов рассылки форм
// (таблицы max_chat/max_route). Данные мелкие и меняются редко, кэш не нужен —
// читаем из БД по запросу (рассылка идёт по кнопке, не горячий путь).
type MaxChatRepository interface {
	// Chats — все чаты (для админ-списка/фронта); порядок по имени.
	Chats(ctx context.Context) ([]domain.MaxChat, error)
	// Routes — маршруты для формы report и терминала terminal (пусто — сводные),
	// только включённые, по sort_order. Возврат — коды чатов (max_chat.name).
	Routes(ctx context.Context, report, terminal string) ([]domain.MaxRoute, error)
}

// MessengerSender — исходящий канал рассылки в мессенджер MAX (перенос gtport
// max.Client). Домен и сервис рассылки знают только этот интерфейс, а не HTTP:
// реализация — internal/adapter/max.
//
// chatID — числовой идентификатор чата MAX (из справочника max_chat). Картинку
// и файл MAX принимает трёхшаговой загрузкой (получить URL → залить → отправить
// вложение по токену) — это скрыто в адаптере, наружу видны простые методы.
type MessengerSender interface {
	// Ping проверяет доступность API и валидность токена (GET /me). Нужен для
	// health-ручки: «проверка токена боем» без отправки сообщения.
	Ping(ctx context.Context) error
	// SendText отправляет текстовое сообщение в чат.
	SendText(ctx context.Context, chatID, text string) error
	// SendImage отправляет изображение (PNG формы) с подписью caption.
	SendImage(ctx context.Context, chatID string, image []byte, filename, caption string) error
	// SendFile отправляет произвольный файл (xlsx/pdf) с подписью caption.
	SendFile(ctx context.Context, chatID string, file []byte, filename, caption string) error
}
