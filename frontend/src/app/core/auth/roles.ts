/**
 * Ролевая модель (решение владельца 31.07.2026, справка — docs/ROLES.md).
 * Realm-роли Keycloak с суффиксом модуля: admin_dpport, operator_dpport,
 * client_dispatcher_dpport, client_dpport. Суффикс разводит наш модуль от
 * соседних в общем realm'е (в токене рядом видны их роли — view_sspr, view_rtport).
 *
 * ⚠️ Роли НЕЗАВИСИМЫ: иерархии с весами нет, admin_dpport не включает права
 * operator_dpport автоматически — он входит в OPER потому, что явно перечислен.
 * Кому нужны обе области, тому в Keycloak назначают две роли.
 *
 * Зеркало серверных наборов auth.Writers / auth.Admins (internal/auth/claims.go).
 * Чтение бэкенд разрешает любому залогиненному, мутации — набору Writers; здесь
 * тот же порог управляет кнопками действий и доступом к разделам.
 *
 * Это единственный источник правды для фронта: nav.config.ts реэкспортирует
 * списки, auth.service.ts берёт нормализацию отсюда же.
 *
 * ⚠️ Роль на фронте — только UI (спрятать кнопку/раздел). Источник истины —
 * гейты бэкенда: на общем realm'е он в strict-режиме и чужую легаси-роль
 * (operator без суффикса) не пустит, даже если UI покажет кнопку.
 */

/**
 * Прежние имена realm-ролей → нынешние (переходный период Keycloak).
 * Нужны, пока realm не переведён: токен со старой ролью продолжает работать,
 * и порядок раскатки «сначала фронт или сначала Keycloak» не важен.
 */
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
