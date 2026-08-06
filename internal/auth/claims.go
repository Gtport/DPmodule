package auth

import "context"

// Role — имя роли. Живёт в двух схемах, которые НЕЛЬЗЯ смешивать:
// client-роли своего клиента Keycloak (resource_access["iqport-dpport"].roles,
// короткие имена) и realm-роли старой схемы (realm_access.roles, суффикс
// _dpport). Одно и то же слово в разных схемах значит разное: client-роль
// operator — наш оператор, realm-роль operator — платформенное легаси чужих
// приложений. Поэтому списки ролей хранятся и проверяются раздельно (Access).
type Role string

// Client-роли клиента iqport-dpport (новая схема, переход 08.2026). Шкала
// платформы client → operator → senior-operator + наши client-dispatcher и admin.
//
// ⚠️ Роли НЕЗАВИСИМЫ (решение владельца 31.07.2026): иерархии с весами нет,
// старшая роль не включает права младших. Доступ = членство в наборе (Access).
const (
	ClientAdmin          Role = "admin"             // администратор: единственный с полной админкой
	ClientSeniorOperator Role = "senior-operator"   // оператор + правки за пределами смены + словари
	ClientOperator       Role = "operator"          // оператор ГТ (правит данные в пределах смены)
	ClientDispatcher     Role = "client-dispatcher" // диспетчер клиента (свои маршруты — след. итерация)
	ClientClient         Role = "client"            // клиент (просмотр)
)

// Realm-роли старой схемы (claim realm_access.roles). Суффикс _dpport разводит
// наш модуль от соседних в общем realm'е. Живут до выключения Full scope allowed
// платформой; аналога senior-operator в старой схеме нет и не заводится.
const (
	RoleAdmin            Role = "admin_dpport"             // администратор системы
	RoleOperator         Role = "operator_dpport"          // оператор ГТ (правит данные)
	RoleClientDispatcher Role = "client_dispatcher_dpport" // диспетчер клиента
	RoleClient           Role = "client_dpport"            // клиент (просмотр)
)

// Access — набор доступа: кто пускается по client-схеме И кто по realm-схеме.
// Два списка, а не один: сложить их в плоский массив — дыра (чужой realm-operator
// стал бы нашим оператором). Каждый список сверяется только со «своей» полкой токена.
type Access struct {
	Client []Role // client-роли своего клиента (resource_access)
	Realm  []Role // realm-роли старой схемы (realm_access)
}

// Наборы доступа — единственное место, где записано «кому что можно».
// Новая ручка закрывается автоматически: гейты висят на группах в server.go.
// Матрица (решение владельца 06.08.2026): operator правит в пределах смены
// (сегодня/вчера); senior-operator — оператор + правки за пределами смены +
// словари marka/stations/naznach_station/sf; admin — всё + полная админка.
var (
	// AccessWrite — «порог правок»: кто может менять данные (любая мутация).
	AccessWrite = Access{
		Client: []Role{ClientOperator, ClientSeniorOperator, ClientAdmin},
		Realm:  []Role{RoleOperator, RoleAdmin},
	}
	// AccessCrossShift — правки документов за пределами смены (окно сегодня/вчера
	// не действует): правки прибытий и «Грузовой работы» за старые даты.
	AccessCrossShift = Access{
		Client: []Role{ClientSeniorOperator, ClientAdmin},
		Realm:  []Role{RoleAdmin},
	}
	// AccessDicts — словари senior-operator'а (marka, stations, naznach_station,
	// sf; список таблиц — service.seniorEditableTables) и вход в админ-раздел.
	// Полный ли доступ внутри раздела — решает AccessAdmin.
	AccessDicts = Access{
		Client: []Role{ClientSeniorOperator, ClientAdmin},
		Realm:  []Role{RoleAdmin},
	}
	// AccessAdmin — полная админка: все таблицы реестра list_tables.
	AccessAdmin = Access{
		Client: []Role{ClientAdmin},
		Realm:  []Role{RoleAdmin},
	}
)

// legacyRoles — прежние имена realm-ролей → нынешние. Переходный период: пока
// Keycloak не переведён на имена с суффиксом, токены со старыми ролями работают.
// Здесь же имена времён gtport (administrator/dispatcher) — они встречаются и в
// данных (client_settings.reject_older_role_exempt), а не только в токенах.
//
// ⚠️ К ролям ИЗ ТОКЕНА эта таблица применяется только при keycloak.strict_roles
// = false (см. TokenRoles): в ОБЩЕМ realm'е стенда голые admin/operator/
// dispatcher — легаси-роли всего контура и могут принадлежать пользователям
// чужих приложений; превращать их в наши — дыра.
var legacyRoles = map[Role]Role{
	"admin":             RoleAdmin,
	"administrator":     RoleAdmin,
	"operator":          RoleOperator,
	"dispatcher":        RoleOperator,
	"client_dispatcher": RoleClientDispatcher,
	"client":            RoleClient,
}

// NormalizeRole приводит имя роли к каноническому (учитывая legacy-имена).
func NormalizeRole(r Role) Role {
	if canon, ok := legacyRoles[r]; ok {
		return canon
	}
	return r
}

// TokenRoles приводит роли из проверенного токена к внутреннему виду — это
// ЕДИНСТВЕННОЕ место, где легаси-имена из токена становятся нашими ролями.
// strict (общий realm, keycloak.strict_roles: true) — имена берутся дословно,
// без нормализации: чужой пользователь контура с легаси-ролью operator не
// должен получить права operator_dpport. Не-strict — свой realm переходного
// периода (боевой VPS, локальный Keycloak), старые токены работают.
func TokenRoles(raw []Role, strict bool) []Role {
	if strict {
		return raw
	}
	out := make([]Role, len(raw))
	for i, r := range raw {
		out[i] = NormalizeRole(r)
	}
	return out
}

// Claims holds the validated JWT payload after Keycloak verification.
// ClientRoles и Roles — разные схемы, хранятся раздельно от разбора токена до
// проверки (см. Access); складывать их в один список нельзя.
type Claims struct {
	Subject  string
	Email    string
	Username string
	Roles       []Role // realm_access.roles — старая схема (нормализованы TokenRoles)
	ClientRoles []Role // resource_access[<clientId>].roles — client-роли нашего модуля
}

type contextKey struct{}

// WithClaims stores validated claims in the context.
func WithClaims(ctx context.Context, c *Claims) context.Context {
	return context.WithValue(ctx, contextKey{}, c)
}

// ClaimsFromContext retrieves claims; returns nil if absent (unauthenticated path).
func ClaimsFromContext(ctx context.Context) *Claims {
	c, _ := ctx.Value(contextKey{}).(*Claims)
	return c
}

// HasRole — есть ли у пользователя хотя бы одна из перечисленных ролей.
// Нормализуется только сторона want: она приходит из кода и данных, где живут
// старые имена (значение "administrator" в reject_older_role_exempt). Роли
// пользователя (have) НЕ нормализуются — их уже привёл TokenRoles при разборе
// токена; нормализация здесь вернула бы дыру strict-режима (чужой operator
// матчился бы с operator_dpport). nil-claims → false (fail closed). Пустой
// список ролей → false: «разрешено никому», а не «всем».
func (c *Claims) HasRole(roles ...Role) bool {
	if c == nil {
		return false
	}
	for _, want := range roles {
		nw := NormalizeRole(want)
		for _, have := range c.Roles {
			if have == nw {
				return true
			}
		}
	}
	return false
}

// Allows — пускает ли набор доступа этого пользователя: есть нужная CLIENT-роль
// ИЛИ нужная REALM-роль, каждый список сверяется со своей полкой токена.
// Client-роли сравниваются дословно (они принадлежат только нашему клиенту,
// нормализовать нечего); realm-сторона идёт через HasRole (нормализация want —
// для старых имён из данных). nil-claims → false (fail closed).
func (c *Claims) Allows(a Access) bool {
	if c == nil {
		return false
	}
	for _, want := range a.Client {
		for _, have := range c.ClientRoles {
			if have == want {
				return true
			}
		}
	}
	return c.HasRole(a.Realm...)
}

// clientEquivalent — какой client-роли соответствует каноническая realm-роль.
// Нужна AccessFor: настройки в данных (reject_older_role_exempt) записаны
// старыми именами, а матчить должны пользователей обеих схем.
var clientEquivalent = map[Role]Role{
	RoleAdmin:            ClientAdmin,
	RoleOperator:         ClientOperator,
	RoleClientDispatcher: ClientDispatcher,
	RoleClient:           ClientClient,
}

// AccessFor — набор доступа из ОДНОГО имени роли, пришедшего из данных или
// настроек (например, reject_older_role_exempt = "administrator"): realm-сторона
// — каноническое realm-имя, client-сторона — его эквивалент в client-схеме.
// Имя, которого нет в realm-схеме (senior-operator), матчится как client-роль.
func AccessFor(r Role) Access {
	canon := NormalizeRole(r)
	if ce, ok := clientEquivalent[canon]; ok {
		return Access{Client: []Role{ce}, Realm: []Role{canon}}
	}
	return Access{Client: []Role{r}, Realm: []Role{canon}}
}
