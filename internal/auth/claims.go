package auth

import "context"

// Role — каноническая роль приложения (realm-роль Keycloak после нормализации).
type Role string

// Иерархия ролей (модель gtport): старшая роль включает права младших.
// Порог правок — RoleOperator: чтение доступно любому залогиненному,
// мутации — от operator и выше (гейт в server.go).
const (
	RoleAdmin            Role = "admin"             // 100 — администратор системы
	RoleOperator         Role = "operator"          // 80  — оператор ГТ (правит данные)
	RoleClientDispatcher Role = "client_dispatcher" // 70  — диспетчер клиента
	RoleClient           Role = "client"            // 60  — клиент (просмотр)
)

// roleWeight — веса иерархии. Неизвестная роль весит 0 и не даёт ничего.
var roleWeight = map[Role]int{
	RoleAdmin:            100,
	RoleOperator:         80,
	RoleClientDispatcher: 70,
	RoleClient:           60,
}

// legacyRoles — старые имена realm-ролей → канонические. Переходный период:
// пока Keycloak (локальный и боевой) не переведён на новые имена, токены со
// старыми ролями продолжают работать (administrator ≡ admin, dispatcher ≡ operator).
var legacyRoles = map[Role]Role{
	"administrator": RoleAdmin,
	"dispatcher":    RoleOperator,
}

// NormalizeRole приводит имя роли к каноническому (учитывая legacy-имена).
func NormalizeRole(r Role) Role {
	if canon, ok := legacyRoles[r]; ok {
		return canon
	}
	return r
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

// HasRole — точное совпадение с любой из перечисленных ролей (без иерархии).
// Обе стороны нормализуются, поэтому сравнение с legacy-именем тоже работает
// (например, значение "administrator" из настройки в БД). nil-claims → false.
func (c *Claims) HasRole(roles ...Role) bool {
	if c == nil {
		return false
	}
	for _, want := range roles {
		for _, have := range c.Roles {
			if NormalizeRole(have) == NormalizeRole(want) {
				return true
			}
		}
	}
	return false
}

// HasMinRole — иерархическая проверка: есть ли роль с весом не ниже min.
// nil-claims → false (fail closed).
func (c *Claims) HasMinRole(min Role) bool {
	if c == nil {
		return false
	}
	need := roleWeight[NormalizeRole(min)]
	if need == 0 {
		return false // неизвестный порог — не разрешаем ничего
	}
	for _, have := range c.Roles {
		if roleWeight[NormalizeRole(have)] >= need {
			return true
		}
	}
	return false
}
