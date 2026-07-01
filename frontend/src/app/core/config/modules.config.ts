/**
 * Каталог модулей платформы IQPort. Один источник правды для:
 *  - module-switcher в навбаре (переход между модулями),
 *  - портала-лаунчера.
 * url — поддомен модуля (отдельные SPA + SSO). roles — кто видит плитку/пункт.
 * Пустой roles => виден всем аутентифицированным.
 */
export interface PlatformModule {
  id: string;
  name: string;
  short: string;
  description: string;
  url: string;
  icon: string;
  roles: string[];
}

export const PLATFORM_MODULES: PlatformModule[] = [
  {
    id: 'mpport',
    name: 'Месячное планирование порта',
    short: 'MPPort',
    description: 'Стратегическое планирование шахта → судно',
    url: 'https://mpport.iqport.ru',
    icon: 'calendar',
    roles: ['manager', 'admin'],
  },
  {
    id: 'dpport',
    name: 'Суточный план движения вагонов',
    short: 'DPPort',
    description: 'Ведёт вагон от оформления до подачи',
    url: 'https://dpport.iqport.ru',
    icon: 'schedule',
    roles: [],
  },
  {
    id: 'rtport',
    name: 'Операции терминала (ЭКС)',
    short: 'RTPort',
    description: 'Статус вагонов, схема путей, маневры в реальном времени',
    url: 'https://rtport.iqport.ru',
    icon: 'deployment-unit',
    roles: [],
  },
  {
    id: 'sspr',
    name: 'Сменно-суточный план подач/уборок',
    short: 'ССПР',
    description: 'Обязательный документ, основание для компенсаций',
    url: 'https://sspr.iqport.ru',
    icon: 'file-done',
    roles: ['dispatcher', 'manager', 'admin'],
  },
  {
    id: 'rtgeo',
    name: 'Карта дислокации поездов',
    short: 'RTGeo',
    description: 'Реальное время, перестановки, фильтры',
    url: 'https://rtgeo.iqport.ru',
    icon: 'environment',
    roles: [],
  },
  {
    id: 'spport',
    name: 'Планирование запасов склада',
    short: 'SPPort',
    description: 'Прогноз формирования и расхода угля по маркам',
    url: 'https://spport.iqport.ru',
    icon: 'database',
    roles: ['dispatcher', 'manager', 'admin'],
  },
  {
    id: 'fpport',
    name: 'Погрузка на суда',
    short: 'FPPort',
    description: 'Назначение угля, списание со склада, сверка отгрузок',
    url: 'https://fpport.iqport.ru',
    icon: 'container',
    roles: [],
  },
];
