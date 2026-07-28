/**
 * Ролевая модель (перенос gtport, решение владельца 28.07.2026).
 * Иерархия: admin (100) > operator (80) > client_dispatcher (70) > client (60).
 * Чтение — любому залогиненному, мутации — от operator (гейт на бэкенде,
 * server.go). Здесь — единственный источник правды для фронта: списки ролей
 * для guard'ов/меню и нормализация legacy-имён из старых токенов.
 */

/** Старые имена realm-ролей → канонические (переходный период Keycloak). */
export const ROLE_ALIASES: Record<string, string> = {
  administrator: 'admin',
  dispatcher: 'operator',
};

export function normalizeRole(role: string): string {
  return ROLE_ALIASES[role] ?? role;
}

/** Порог правок: кто видит кнопки действий и рабочие разделы оператора. */
export const OPER = ['operator', 'admin'];

/** Только администратор (раздел «Админ»: редактор справочников). */
export const ADMIN = ['admin'];
