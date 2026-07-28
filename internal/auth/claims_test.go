package auth

import "testing"

// Иерархия и нормализация legacy-имён — фундамент всей авторизации,
// фиксируем поведение таблично.
func TestHasMinRole(t *testing.T) {
	cases := []struct {
		name string
		have []Role
		min  Role
		want bool
	}{
		{"admin проходит порог operator", []Role{RoleAdmin}, RoleOperator, true},
		{"operator проходит порог operator", []Role{RoleOperator}, RoleOperator, true},
		{"client_dispatcher НЕ проходит порог operator", []Role{RoleClientDispatcher}, RoleOperator, false},
		{"client НЕ проходит порог operator", []Role{RoleClient}, RoleOperator, false},
		{"client проходит порог client", []Role{RoleClient}, RoleClient, true},
		{"operator НЕ проходит порог admin", []Role{RoleOperator}, RoleAdmin, false},
		{"legacy administrator ≡ admin", []Role{"administrator"}, RoleAdmin, true},
		{"legacy dispatcher ≡ operator", []Role{"dispatcher"}, RoleOperator, true},
		{"legacy dispatcher НЕ проходит порог admin", []Role{"dispatcher"}, RoleAdmin, false},
		{"неизвестная роль ничего не даёт", []Role{"manager"}, RoleClient, false},
		{"без ролей — отказ", nil, RoleClient, false},
		{"несколько ролей — берётся максимальная", []Role{RoleClient, RoleOperator}, RoleOperator, true},
		{"неизвестный порог — отказ даже админу", []Role{RoleAdmin}, "boss", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Claims{Roles: tc.have}
			if got := c.HasMinRole(tc.min); got != tc.want {
				t.Fatalf("HasMinRole(%v, %q) = %v, want %v", tc.have, tc.min, got, tc.want)
			}
		})
	}
}

func TestHasMinRoleNilClaims(t *testing.T) {
	var c *Claims
	if c.HasMinRole(RoleClient) {
		t.Fatal("nil claims должны давать отказ (fail closed)")
	}
	if c.HasRole(RoleAdmin) {
		t.Fatal("nil claims должны давать отказ (fail closed)")
	}
}

// HasRole нормализует обе стороны: значение-настройка из БД со старым именем
// роли («administrator» в reject_older_role_exempt) продолжает работать.
func TestHasRoleNormalizesBothSides(t *testing.T) {
	c := &Claims{Roles: []Role{RoleAdmin}}
	if !c.HasRole("administrator") {
		t.Fatal("HasRole(administrator) должен матчить каноническую роль admin")
	}
	legacy := &Claims{Roles: []Role{"administrator"}}
	if !legacy.HasRole(RoleAdmin) {
		t.Fatal("HasRole(admin) должен матчить legacy-роль administrator")
	}
}
