package logger

import "go.uber.org/zap"

// Словарь предметных областей. Список ЗАКРЫТЫЙ: пока он закрыт, `grep
// 'dislocation'` собирает всю работу конвейера независимо от того, в каком
// файле стоит вызов. Прежде область писалась префиксом в тексте сообщения и
// каждый раз по-своему — «АСУ:», «601:», «reference:», «ЛК РЖД:», «конвейер:»,
// «JWT:» — и грепом не собиралась.
//
// ГЛАВНОЕ ПРАВИЛО: component — это ЧТО МЫ ДЕЛАЕМ, а не КУДА ХОДИМ.
// Дислокация приходит двумя путями (ЛК и АСУ) — компонент у них один,
// CompDislocation, а источник живёт в колонке цели. Иначе одна операция
// размазывается по двум «компонентам» и цепочка не собирается.
//
// Новую область заводить здесь же и одновременно вписывать в docs/LOGGING.md:
// словарь — часть интерфейса лога для того, кто его читает.
const (
	CompStartup     = "startup"     // подъём и остановка приложения
	CompHTTP        = "http"        // входящие запросы
	CompDB          = "db"          // обращения к базе (мост GORM → zap)
	CompAuth        = "auth"        // разбор токена, роли, сервис-аккаунт Keycloak
	CompDislocation = "dislocation" // конвейер: забор ЛК/АСУ, гарды, пересборка снимка
	CompPamyatka    = "pamyatka"    // памятки ГУ-45 на подачу/уборку
	CompGU2B        = "gu2b"        // уведомления ГУ-2б о завершении грузовой операции (факт выгрузки)
	CompVagonops    = "vagonops"    // история продвижения вагона (запрос 601)
	CompPlan        = "plan"        // план подвода
	CompNotify      = "notify"      // уведомления (колокольчик)
	CompBroadcast   = "broadcast"   // отправка форм и отчётов в MAX
	CompJournal     = "journal"     // журнал событий event_journal
	CompWorker      = "worker"      // кроны и фоновые воркеры
	CompTiles       = "tiles"       // кэш тайлов карты
)

// components — тот же список для проверок; порядок не важен.
var components = []string{
	CompStartup, CompHTTP, CompDB, CompAuth, CompDislocation, CompPamyatka,
	CompGU2B, CompVagonops, CompPlan, CompNotify, CompBroadcast, CompJournal,
	CompWorker, CompTiles,
}

// IsComponent сообщает, входит ли значение в словарь. Нужен тестам, которые
// сторожат закрытость списка.
func IsComponent(s string) bool { return contains(components, s) }

// Comp — поле предметной области для событий без сети (пересборка снимка,
// старт, запись в журнал).
func Comp(component string) zap.Field { return zap.String(FieldComponent, component) }

// Out — поля исходящего вызова: что делаем и куда пошли.
// target — «имя host:port»: имя чтобы читать, адрес чтобы понимать, в какой
// контур ушёл запрос (у нас бой, стенд и машина разработчика ходят к разным).
func Out(component, target string) []zap.Field {
	return []zap.Field{
		zap.String(FieldComponent, component),
		zap.String(FieldDir, DirOut),
		zap.String(FieldTarget, target),
	}
}

// In — поля входящего запроса: кто пришёл.
func In(component, target string) []zap.Field {
	return []zap.Field{
		zap.String(FieldComponent, component),
		zap.String(FieldDir, DirIn),
		zap.String(FieldTarget, target),
	}
}

// With приклеивает component к логгеру раз и навсегда — для подсистем, у
// которых свой долгоживущий логгер (воркер, сервис памяток).
func With(l *zap.Logger, component string) *zap.Logger {
	return l.With(Comp(component))
}
