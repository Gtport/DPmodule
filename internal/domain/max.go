package domain

// Справочник чатов мессенджера MAX и маршруты рассылки форм. Транспорт (адаптер
// port.MessengerSender) знает лишь chat_id; КУДА слать конкретную форму —
// определяют эти сущности. Уносит порт-хардкод фронта gtport (MAX_CHATS_CONFIG)
// в данные (таблицы max_chat/max_route, правятся в Админе).

// Типы форм рассылки (значение max_route.report). Не enum БД: новую форму
// добавляют строкой маршрута, без миграции.
const (
	MaxReportSpravki = "spravki" // справки терминала (полный план подвода)
	MaxReportOper    = "oper"    // оперативка терминала (список поездов)
	MaxReportPlan    = "plan"    // сводная форма (в общий чат, без привязки к терминалу)
)

// MaxChat — чат MAX: код (ключ маршрутов) → числовой id чата у провайдера.
type MaxChat struct {
	Name        string // код чата (at/gut/oper/...)
	ChatID      string // id чата в MAX (число строкой: длиннее int64, с минусом)
	Description string
	IsActive    bool
}

// MaxRoute — правило «форма × терминал → чат». Terminal пуст для сводных форм.
type MaxRoute struct {
	ID        int64
	Report    string // MaxReport*
	Terminal  string // ports.name_s; '' — не по одному терминалу
	ChatName  string // MaxChat.Name
	SortOrder int
	Enabled   bool
}
