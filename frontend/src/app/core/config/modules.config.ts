/**
 * Каталог модулей платформы IQPort. Один источник правды для:
 *  - module-switcher в навбаре (переход между модулями),
 *  - портала-лаунчера.
 *
 * ⚠️ Зеркало каталога ПОРТАЛА (репозиторий iqport/portal,
 * src/app/core/config/modules.config.ts). Владелец списка — портал; здесь копия,
 * чтобы свитчер в навбаре показывал ровно те же модули, что плитки лаунчера.
 * Разъезжаются списки — правится и там, и тут (docs/PORTAL_INTEGRATION.md).
 *
 * url — поддомен модуля (отдельные SPA + SSO, один realm iqport).
 * roles — realm-роли, дающие доступ к модулю: достаточно ЛЮБОЙ из списка.
 *   Обычно это view_<id>/edit_<id>, но шкала у модуля может быть своя — у dpport
 *   четыре уровня (admin/operator/client_dispatcher/client), и перечислены все,
 *   потому что доступ к модулю даёт каждый из них.
 *   ВАЖНО: пустой список = доступ ВСЕМ вошедшим.
 * available — модуль реально развёрнут. false => в свитчере пункта нет вовсе
 *   (в портале — серая плитка «Недоступно»), независимо от ролей.
 */
export interface PlatformModule {
  id: string;
  name: string;
  short: string;
  description: string;
  url: string;
  icon: string;
  roles: string[];
  available: boolean;
}

export const PLATFORM_MODULES: PlatformModule[] = [
  {
    id: 'mpport',
    name: 'Месячное планирование порта',
    short: 'MPPort',
    description: 'Стратегическое планирование шахта → судно',
    // В ветке тимлида здесь остался http://localhost:4202 (локальная отладка) —
    // в сборку такое не берём: боевой адрес по конвенции платформы.
    url: 'https://mpport.iqport.ru',
    icon: 'calendar',
    roles: ['view_mpport', 'edit_mpport'],
    available: true,
  },
  {
    id: 'dpport',
    name: 'Суточный план движения вагонов',
    short: 'DPPort',
    description: 'Ведёт вагон от оформления до подачи',
    url: 'https://dpport.iqport.ru',
    icon: 'schedule',
    // Наша ролевая модель (roles.ts, решение владельца 31.07.2026): четыре
    // независимых уровня с суффиксом модуля, доступ в модуль даёт любой из них.
    roles: ['admin_dpport', 'operator_dpport', 'client_dispatcher_dpport', 'client_dpport'],
    available: true,
  },
  {
    id: 'rtport',
    name: 'Операции терминала (ЭКС)',
    short: 'RTPort',
    description: 'Статус вагонов, схема путей, маневры в реальном времени',
    url: 'https://rtport.iqport.ru',
    icon: 'deployment-unit',
    roles: ['view_rtport', 'edit_rtport'],
    available: true,
  },
  {
    id: 'sspr',
    name: 'Сменно-суточный план подач/уборок',
    short: 'ССПР',
    description: 'Обязательный документ, основание для компенсаций',
    url: 'https://sspr.iqport.ru',
    icon: 'file-done',
    roles: ['view_sspr', 'edit_sspr'],
    available: true,
  },
  {
    id: 'rtgeo',
    name: 'Карта дислокации поездов',
    short: 'RTGeo',
    description: 'Реальное время, перестановки, фильтры',
    url: 'https://rtgeo.iqport.ru',
    icon: 'environment',
    roles: ['view_rtgeo', 'edit_rtgeo'],
    available: false,
  },
  {
    id: 'spport',
    name: 'Планирование запасов склада',
    short: 'SPPort',
    description: 'Прогноз формирования и расхода угля по маркам',
    url: 'https://spport.iqport.ru',
    icon: 'database',
    roles: ['view_spport', 'edit_spport'],
    available: false,
  },
  {
    id: 'fpport',
    name: 'Погрузка на суда',
    short: 'FPPort',
    description: 'Назначение угля, списание со склада, сверка отгрузок',
    url: 'https://fpport.iqport.ru',
    icon: 'container',
    roles: ['view_fpport', 'edit_fpport'],
    available: false,
  },
];
