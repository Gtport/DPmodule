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
		{"operator среди правок", []Role{RoleOperator}, AccessWrite.Realm, true},
		{"admin среди правок", []Role{RoleAdmin}, AccessWrite.Realm, true},
		{"client_dispatcher НЕ среди правок", []Role{RoleClientDispatcher}, AccessWrite.Realm, false},
		{"client НЕ среди правок", []Role{RoleClient}, AccessWrite.Realm, false},
		{"admin среди админки", []Role{RoleAdmin}, AccessAdmin.Realm, true},
		{"operator НЕ среди админки — иерархии нет", []Role{RoleOperator}, AccessAdmin.Realm, false},
		{"две роли — хватает одной подходящей", []Role{RoleClient, RoleOperator}, AccessWrite.Realm, true},
		{"неизвестная роль ничего не даёт", []Role{"manager_dpport"}, AccessWrite.Realm, false},
		{"роль соседнего модуля не подходит", []Role{"admin_other"}, AccessAdmin.Realm, false},
		{"без ролей — отказ", nil, AccessWrite.Realm, false},
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
	if c.Allows(AccessAdmin) {
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
	if foreign.Allows(AccessWrite) || foreign.Allows(AccessAdmin) {
		t.Fatal("strict: легаси-роли общего realm'а не должны давать доступ DPPort")
	}
	ours := &Claims{Roles: TokenRoles([]Role{RoleOperator, "view_rtport"}, true)}
	if !ours.Allows(AccessWrite) {
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

// Наборы — единственное место, где записано «кому что можно». Матрица владельца
// 06.08.2026 таблично: operator правит в пределах смены; senior-operator — ещё
// за пределами смены и словари; admin — всё + полная админка; клиентские роли —
// только просмотр. Проверяется через Allows по ОБЕИМ схемам.
func TestAccessMatrix(t *testing.T) {
	cases := []struct {
		name                            string
		c                               *Claims
		write, crossShift, dicts, admin bool
	}{
		{"client-роль operator", &Claims{ClientRoles: []Role{ClientOperator}}, true, false, false, false},
		{"client-роль senior-operator", &Claims{ClientRoles: []Role{ClientSeniorOperator}}, true, true, true, false},
		{"client-роль admin", &Claims{ClientRoles: []Role{ClientAdmin}}, true, true, true, true},
		{"client-роль client", &Claims{ClientRoles: []Role{ClientClient}}, false, false, false, false},
		{"client-роль client-dispatcher", &Claims{ClientRoles: []Role{ClientDispatcher}}, false, false, false, false},
		{"realm operator_dpport (старая схема)", &Claims{Roles: []Role{RoleOperator}}, true, false, false, false},
		{"realm admin_dpport (старая схема)", &Claims{Roles: []Role{RoleAdmin}}, true, true, true, true},
		{"без ролей", &Claims{}, false, false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := [4]bool{tc.c.Allows(AccessWrite), tc.c.Allows(AccessCrossShift),
				tc.c.Allows(AccessDicts), tc.c.Allows(AccessAdmin)}
			want := [4]bool{tc.write, tc.crossShift, tc.dicts, tc.admin}
			if got != want {
				t.Fatalf("write/crossShift/dicts/admin = %v, want %v", got, want)
			}
		})
	}
}

// ГЛАВНАЯ ЛОВУШКА перехода на client-роли: одно слово в двух схемах значит
// разное. Client-роль operator — наш оператор; realm-роль operator — чужое
// платформенное легаси (висит на demo). Списки нельзя складывать в один:
// тест сторожит, что realm-имя не проходит по client-списку и наоборот.
func TestAllowsSchemasNotMixed(t *testing.T) {
	// demo: в strict-режиме realm-роль operator остаётся дословной — правок нет.
	demo := &Claims{Roles: TokenRoles([]Role{"operator"}, true)}
	if demo.Allows(AccessWrite) {
		t.Fatal("realm-роль operator (чужое легаси) не должна давать правки")
	}
	// Имя client-схемы, случайно оказавшееся в realm-полке, не работает.
	fakeRealm := &Claims{Roles: []Role{"senior-operator"}}
	if fakeRealm.Allows(AccessCrossShift) {
		t.Fatal("client-имя в realm-полке не должно матчиться")
	}
	// Имя realm-схемы в client-полке — тоже нет.
	fakeClient := &Claims{ClientRoles: []Role{RoleOperator}}
	if fakeClient.Allows(AccessWrite) {
		t.Fatal("realm-имя operator_dpport в client-полке не должно матчиться")
	}
}

// AccessFor: настройка из данных с одним именем роли (reject_older_role_exempt
// = "administrator") матчит пользователей ОБЕИХ схем.
func TestAccessFor(t *testing.T) {
	a := AccessFor("administrator")
	if !(&Claims{Roles: []Role{RoleAdmin}}).Allows(a) {
		t.Fatal("administrator должен матчить realm admin_dpport")
	}
	if !(&Claims{ClientRoles: []Role{ClientAdmin}}).Allows(a) {
		t.Fatal("administrator должен матчить client-роль admin")
	}
	if (&Claims{ClientRoles: []Role{ClientOperator}}).Allows(a) {
		t.Fatal("client-роль operator не должна матчить administrator")
	}
	// Имя, существующее только в client-схеме, работает как client-роль.
	s := AccessFor("senior-operator")
	if !(&Claims{ClientRoles: []Role{ClientSeniorOperator}}).Allows(s) {
		t.Fatal("senior-operator должен матчить client-роль senior-operator")
	}
}
