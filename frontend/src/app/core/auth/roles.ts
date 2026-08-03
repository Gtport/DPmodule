/**
 * Ролевая модель (решение владельца 31.07.2026, справка — docs/ROLES.md).
 * Realm-роли Keycloak с суффиксом модуля: admin_dpport, operator_dpport,
 * client_dispatcher_dpport, client_dpport. Роли НЕЗАВИСИМЫ — иерархии с
 * весами нет, доступ = членство в наборе (OPER / ADMIN ниже).
 * Здесь — единственный источник правды для фронта: списки ролей для
 * guard'ов/меню и нормализация прежних имён из старых токенов.
 *
 * ⚠️ Роль на фронте — только UI (спрятать кнопку/раздел). Источник истины —
 * гейты бэкенда: на общем realm'е стенда он в strict-режиме и чужую
 * легаси-роль (operator без суффикса) не пустит, даже если UI покажет кнопку.
 */

/** Прежние имена realm-ролей → нынешние (переходный период Keycloak). */
export const ROLE_ALIASES: Record<string, string> = {
  admin: 'admin_dpport',
  administrator: 'admin_dpport',
  operator: 'operator_dpport',
  dispatcher: 'operator_dpport',
  client_dispatcher: 'client_dispatcher_dpport',
  client: 'client_dpport',
};

export function normalizeRole(role: string): string {
  return ROLE_ALIASES[role] ?? role;
}

/** Порог правок: кто видит кнопки действий и рабочие разделы оператора. */
export const OPER = ['operator_dpport', 'admin_dpport'];

/** Только администратор (раздел «Админ»: редактор справочников). */
export const ADMIN = ['admin_dpport'];
