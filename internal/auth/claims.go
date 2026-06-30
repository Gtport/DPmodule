package auth

import "context"

// Role is a Keycloak realm role.
type Role string

const (
	RoleOperator      Role = "operator"
	RoleDispatcher    Role = "dispatcher"
	RoleManager       Role = "manager"
	RoleAdministrator Role = "administrator"
)

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

// HasRole returns true if claims contain any of the specified roles.
func (c *Claims) HasRole(roles ...Role) bool {
	for _, want := range roles {
		for _, have := range c.Roles {
			if have == want {
				return true
			}
		}
	}
	return false
}
