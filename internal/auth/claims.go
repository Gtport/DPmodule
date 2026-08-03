package auth

import "context"

// Role — каноническая роль приложения (realm-роль Keycloak после нормализации).
type Role string

// Роли приложения — realm-роли Keycloak (claim realm_access.roles). Суффикс
// _dpport разводит наш модуль от соседних в общем realm'е.
//
// ⚠️ Роли НЕЗАВИСИМЫ (решение владельца 31.07.2026): иерархии с весами больше
// нет, старшая роль не включает права младших. Доступ = членство в наборе
// разрешённых ролей, наборы ниже.
const (
	RoleAdmin            Role = "admin_dpport"             // администратор системы
	RoleOperator         Role = "operator_dpport"          // оператор ГТ (правит данные)
	RoleClientDispatcher Role = "client_dispatcher_dpport" // диспетчер клиента
	RoleClient           Role = "client_dpport"            // клиент (просмотр)
)

// Наборы доступа — единственное место, где записано «кому что можно».
// Новая ручка закрывается автоматически: гейты висят на группах в server.go.
var (
	// Writers — «порог правок»: кто может менять данные (любая мутация).
	Writers = []Role{RoleOperator, RoleAdmin}
	// Admins — админ-раздел справочников (/admin/tables*), включая чтение.
	Admins = []Role{RoleAdmin}
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
type Claims struct {
	Subject  string
	Email    string
	Username string
	Roles    []Role
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
