/**
 * Ролевая модель (решения владельца 31.07 и 06.08.2026, справка — docs/ROLES.md).
 * Схем ДВЕ, и они живут раздельно (RoleSet — пара списков):
 *
 *  - client-роли нашего клиента iqport-dpport (токен:
 *    resource_access['iqport-dpport'].roles) — новая схема, короткие имена:
 *    admin / senior-operator / operator / client-dispatcher / client;
 *  - realm-роли (токен: realm_access.roles) — старая схема с суффиксом
 *    *_dpport, живёт до выключения Full scope allowed платформой.
 *
 * ⚠️ ГЛАВНАЯ ЛОВУШКА: одно слово в двух схемах значит разное. Client-роль
 * operator — наш оператор; realm-роль operator — платформенное легаси чужих
 * приложений (висит на demo). Поэтому списки НЕЛЬЗЯ складывать в один и искать
 * по плоскому массиву: каждый список сверяется только со «своей» полкой токена
 * (auth.service.allows), ровно как auth.Claims.Allows на бэкенде.
 *
 * ⚠️ Роли НЕЗАВИСИМЫ: иерархии с весами нет, admin не включает права operator
 * автоматически — он входит в OPER потому, что явно перечислен. Кому нужны обе
 * области, тому в Keycloak назначают две роли.
 *
 * Это единственный источник правды для фронта — зеркало серверных наборов
 * auth.AccessWrite / AccessCrossShift / AccessDicts / AccessAdmin
 * (internal/auth/claims.go). Роль на фронте — только UI (спрятать кнопку/
 * раздел); источник истины — гейты бэкенда.
 */

/** Пара списков ролей: каждый сверяется со своей полкой токена. */
export interface RoleSet {
  /** Client-роли нашего клиента (resource_access, дословно). */
  client: string[];
  /** Realm-роли старой схемы (realm_access, с нормализацией прежних имён). */
  realm: string[];
}

/**
 * Прежние имена realm-ролей → нынешние (переходный период Keycloak).
 * Нужны, пока боевой realm не переведён: токен со старой ролью работает,
 * и порядок раскатки «сначала фронт или сначала Keycloak» не важен.
 * К client-ролям НЕ применяется — там имена дословные.
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

/** Виден всем авторизованным (оба списка пусты). */
export const ANY: RoleSet = { client: [], realm: [] };

/** Порог правок: кто видит кнопки действий и рабочие разделы оператора. */
export const OPER: RoleSet = {
  client: ['operator', 'senior-operator', 'admin'],
  realm: ['operator_dpport', 'admin_dpport'],
};

/**
 * Словари senior-operator'а и вход в раздел «Админ»: senior видит там только
 * свои 4 словаря (marka, stations, станции перестановок, sf) — список таблиц
 * отдаёт сервер уже отфильтрованным. Полный реестр — ADMIN.
 */
export const DICTS: RoleSet = {
  client: ['senior-operator', 'admin'],
  realm: ['admin_dpport'],
};

/** Только администратор (полный реестр справочников; аналога senior в старой схеме нет). */
export const ADMIN: RoleSet = {
  client: ['admin'],
  realm: ['admin_dpport'],
};
