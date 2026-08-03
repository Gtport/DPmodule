package auth

import "testing"

// Роли НЕЗАВИСИМЫ: старшая не включает права младших. Фиксируем это таблично —
// раньше здесь была иерархия с весами, и возврат к ней должен ронять тесты.
func TestHasRole(t *testing.T) {
	cases := []struct {
		name  string
		have  []Role
		allow []Role
		want  bool
	}{
		{"operator среди Writers", []Role{RoleOperator}, Writers, true},
		{"admin среди Writers", []Role{RoleAdmin}, Writers, true},
		{"client_dispatcher НЕ среди Writers", []Role{RoleClientDispatcher}, Writers, false},
		{"client НЕ среди Writers", []Role{RoleClient}, Writers, false},
		{"admin среди Admins", []Role{RoleAdmin}, Admins, true},
		{"operator НЕ среди Admins — иерархии нет", []Role{RoleOperator}, Admins, false},
		{"две роли — хватает одной подходящей", []Role{RoleClient, RoleOperator}, Writers, true},
		{"неизвестная роль ничего не даёт", []Role{"manager_dpport"}, Writers, false},
		{"роль соседнего модуля не подходит", []Role{"admin_other"}, Admins, false},
		{"без ролей — отказ", nil, Writers, false},
		{"пустой набор разрешённых — отказ даже админу", []Role{RoleAdmin}, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Claims{Roles: tc.have}
			if got := c.HasRole(tc.allow...); got != tc.want {
				t.Fatalf("HasRole(have=%v, allow=%v) = %v, want %v", tc.have, tc.allow, got, tc.want)
			}
		})
	}
}

func TestHasRoleNilClaims(t *testing.T) {
	var c *Claims
	if c.HasRole(RoleClient) {
		t.Fatal("nil claims должны давать отказ (fail closed)")
	}
	if c.HasRole(Admins...) {
		t.Fatal("nil claims должны давать отказ (fail closed)")
	}
}

// Переходный период СВОЕГО realm'а (strict=false): боевой Keycloak ещё на
// старых именах, и токены с ними обязаны работать до перевода realm'а.
func TestTokenRolesLegacyNormalized(t *testing.T) {
	cases := []struct {
		legacy Role
		canon  Role
	}{
		{"admin", RoleAdmin},
		{"administrator", RoleAdmin},
		{"operator", RoleOperator},
		{"dispatcher", RoleOperator},
		{"client_dispatcher", RoleClientDispatcher},
		{"client", RoleClient},
	}
	for _, tc := range cases {
		t.Run(string(tc.legacy), func(t *testing.T) {
			if got := NormalizeRole(tc.legacy); got != tc.canon {
				t.Fatalf("NormalizeRole(%q) = %q, want %q", tc.legacy, got, tc.canon)
			}
			c := &Claims{Roles: TokenRoles([]Role{tc.legacy}, false)}
			if !c.HasRole(tc.canon) {
				t.Fatalf("токен с legacy-ролью %q должен проходить проверку на %q", tc.legacy, tc.canon)
			}
		})
	}
}

// strict_roles (общий realm стенда): легаси-роли контура — возможно ЧУЖИЕ
// (пользователь другого приложения с ролью operator) и наших прав не дают.
// Дыра, которую сторожит тест: нормализация have-стороны в HasRole или ролей
// токена в strict-режиме снова превратила бы чужой operator в operator_dpport.
func TestTokenRolesStrictNoLegacy(t *testing.T) {
	foreign := &Claims{Roles: TokenRoles([]Role{"operator", "admin", "dispatcher", "view_sspr"}, true)}
	if foreign.HasRole(Writers...) || foreign.HasRole(Admins...) {
		t.Fatal("strict: легаси-роли общего realm'а не должны давать доступ DPPort")
	}
	ours := &Claims{Roles: TokenRoles([]Role{RoleOperator, "view_rtport"}, true)}
	if !ours.HasRole(Writers...) {
		t.Fatal("strict: точное имя operator_dpport обязано работать")
	}
}

// Want-сторона нормализуется: значение-настройка из БД со старым именем роли
// («administrator» в reject_older_role_exempt) продолжает матчить админа.
// Have-сторона — НЕТ: роли пользователя приводит только TokenRoles при разборе.
func TestHasRoleNormalizesWantOnly(t *testing.T) {
	c := &Claims{Roles: []Role{RoleAdmin}}
	if !c.HasRole("administrator") {
		t.Fatal("HasRole(administrator) должен матчить нынешнюю роль admin_dpport")
	}
	raw := &Claims{Roles: []Role{"administrator"}}
	if raw.HasRole(RoleAdmin) {
		t.Fatal("ненормализованная have-роль не должна матчиться: приведение — забота TokenRoles")
	}
}

// Наборы — единственное место, где записано «кому что можно». Тест сторожит
// смысл: правки — оператору и админу, админ-раздел — только админу.
func TestAccessSets(t *testing.T) {
	if len(Writers) != 2 || !(&Claims{Roles: []Role{RoleOperator}}).HasRole(Writers...) ||
		!(&Claims{Roles: []Role{RoleAdmin}}).HasRole(Writers...) {
		t.Errorf("Writers = %v, ждали operator_dpport + admin_dpport", Writers)
	}
	if len(Admins) != 1 || Admins[0] != RoleAdmin {
		t.Errorf("Admins = %v, ждали только admin_dpport", Admins)
	}
}
