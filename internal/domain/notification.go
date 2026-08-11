package domain

import "encoding/json"

// Внутренние уведомления (перенос колокольчика gtport). Уведомление хранит
// АУДИТОРИЮ, а не список получателей: пользователей в БД нет (Keycloak),
// видимость вычисляется на чтении по ролям из claims. «Прочитано» — ленивая
// строка в notification_read на имя пользователя. Дедуп повторных событий —
// персистентный DedupKey (частичный уникальный индекс), а не RAM-мапы gtport.

// Типы уведомлений (notifications.ntype) — цвет/иконка в UI.
const (
	NotifyTypeInfo    = "info"    // обычное событие (подъём, прибытие)
	NotifyTypeWarning = "warning" // требует внимания («Брошен поезд!»)
	NotifyTypeError   = "error"   // системный сбой (отклонённый забор АСУ)
	NotifyTypeService = "service" // служебное (дыры справочников)
)

// Аудитории (notifications.audience) — соответствие наборам auth.Access*:
// oper → AccessWrite, dicts → AccessDicts, admin → AccessAdmin.
const (
	AudienceAll   = "all"   // все аутентифицированные (зарезервировано)
	AudienceOper  = "oper"  // операторский контур: брошенные, прибытия
	AudienceDicts = "dicts" // кто правит словари: дыры справочников
	AudienceAdmin = "admin" // администратор: системные сбои
)

// Deep-link фронта (notifications.action_component) — какую модалку/экран
// открыть по клику; параметры — в ActionParams.
const (
	NotifyActionBros      = "bros"       // модалка брошенных с главной
	NotifyActionArrivals  = "arrivals"   // модалка «История прибывших»
	NotifyActionUnmatched = "unmatched"  // модалка «Без атрибуции»
	NotifyActionAdminDict = "admin_dict" // админ-редактор справочника (params.table)
)

// Notification — одно уведомление (строка notifications).
type Notification struct {
	ID              int64
	Type            string // NotifyType*
	Title           string
	Message         string
	Audience        string // Audience*; при пустом TargetUsername определяет видимость
	TargetUsername  string // адресно одному пользователю; пусто = по аудитории
	ActionComponent string // NotifyAction*; пусто = без deep-link
	ActionParams    json.RawMessage
	DedupKey        string // пусто = без дедупа
	CreatedAt       LocalTime
}

// UserNotification — уведомление глазами пользователя (+ состояние прочтения).
type UserNotification struct {
	Notification
	IsRead bool
	ReadAt *LocalTime
}
