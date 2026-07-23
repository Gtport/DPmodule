/**
 * Реестр навигации модуля (data-driven, а не хардкод в шаблоне).
 * Из него строятся И пункты сайдбара (shell), И дочерние маршруты (app.routes).
 * Универсальный принцип: новые разделы добавляются сюда одной строкой.
 *
 * Соответствие оригиналу GTport — меню роли «диспетчер» (в GTport — operator),
 * см. CompactSidebar.tsx (operatorItems).
 */
export interface NavItem {
  /** Путь маршрута без ведущего слэша (совпадает с path в Routes). */
  path: string;
  /** Подпись пункта (как видит пользователь). */
  label: string;
  /** Тип иконки ng-zorro (nzType); определение (Fill/Outline/кастом) — в app.config.ts. */
  icon: string;
  /** Тема иконки: 'fill' (сплошная, как в GTport) или 'outline'. */
  theme: 'fill' | 'outline';
  /** Роли, которым виден пункт. Пусто = виден всем авторизованным. */
  roles: string[];
  /** true — реальная страница вне реестра-плейсхолдера (маршрут не генерим). */
  external?: boolean;
}

/** Кому доступны рабочие разделы диспетчера (админ видит всё). */
export const DISP = ['dispatcher', 'administrator'];

/** Только администратор (раздел «Админ»: редактор справочников). */
export const ADMIN = ['administrator'];

// Иконки подобраны 1:1 с оригиналом GTport (CompactSidebar.tsx): сплошные (fill).
// train / ship / warehouse — кастомные (см. core/config/custom-icons.ts), т.к. в
// наборе ant-design их нет; swap/global/line-chart/bar-chart — только outline.
export const DISPATCHER_NAV: NavItem[] = [
  { path: 'home',           label: 'Главная',               icon: 'home',         theme: 'fill',    roles: [],   external: true },
  // «Дислокация» убрана из меню (решение владельца): статус системы, «Обновить
  // из АСУ» и «Приём ЛК» переехали на главную, в колонку «Оперативка». Маршрут
  // /dislocation жив (прямой ссылкой) — страница осталась как запасной путь.
  // «Пропавшие вагоны» и «Грузовая работа» убраны из меню (решение владельца):
  // оба открываются модалками из карточки «Информация» на главной. Маршрут
  // /missing жив (прямой ссылкой, см. app.routes), у «Грузовой работы»
  // отдельной страницы нет вовсе — только модалка.
  { path: 'rearrangement',  label: 'Перестановки',          icon: 'swap',         theme: 'outline', roles: DISP },
  { path: 'plan',           label: 'План подвода',          icon: 'train',        theme: 'fill',    roles: DISP },
  { path: 'broadcast',      label: 'Рассылка',              icon: 'send',         theme: 'outline', roles: DISP },
  { path: 'reference',      label: 'Справки',               icon: 'edit',         theme: 'fill',    roles: DISP },
  { path: 'warehouse',      label: 'Склад',                 icon: 'warehouse',    theme: 'fill',    roles: DISP },
  { path: 'shipments',      label: 'Судовые партии',        icon: 'ship',         theme: 'fill',    roles: DISP },
  { path: 'daily-work',     label: 'Работа за сутки',       icon: 'clock-circle', theme: 'fill',    roles: DISP },
  { path: 'operator-tools', label: 'Инструменты оператора', icon: 'tool',         theme: 'fill',    roles: DISP },
  { path: 'maps',           label: 'Карты',                 icon: 'global',       theme: 'outline', roles: DISP },
  { path: 'forecasts',      label: 'Прогнозы',              icon: 'line-chart',   theme: 'outline', roles: DISP },
  { path: 'reports',        label: 'Справки и отчёты',      icon: 'bar-chart',    theme: 'outline', roles: DISP },
  { path: 'admin',          label: 'Админ',                 icon: 'setting',      theme: 'fill',    roles: ADMIN },
];
