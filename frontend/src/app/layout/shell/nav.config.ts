/**
 * Реестр навигации модуля (data-driven, а не хардкод в шаблоне).
 * Из него строятся И пункты сайдбара (shell), И дочерние маршруты (app.routes).
 * Универсальный принцип: новые разделы добавляются сюда одной строкой.
 *
 * Соответствие оригиналу GTport — меню роли «диспетчер» (в GTport — operator),
 * см. CompactSidebar.tsx (operatorItems).
 */
import { ANY, DICTS, OPER, RoleSet } from '../../core/auth/roles';

export interface NavItem {
  /** Путь маршрута без ведущего слэша (совпадает с path в Routes). */
  path: string;
  /** Подпись пункта (как видит пользователь). */
  label: string;
  /** Тип иконки ng-zorro (nzType); определение (Fill/Outline/кастом) — в app.config.ts. */
  icon: string;
  /** Тема иконки: 'fill' (сплошная, как в GTport) или 'outline'. */
  theme: 'fill' | 'outline';
  /** Набор доступа (пара client/realm, см. roles.ts). ANY = всем авторизованным. */
  roles: RoleSet;
  /** true — реальная страница вне реестра-плейсхолдера (маршрут не генерим). */
  external?: boolean;
}

// Наборы ролей — из единого источника core/auth/roles.ts. Рабочие разделы
// оператора — от operator и выше; клиентские роли видят только «Главную»
// (просмотр, кнопки действий скрыты через auth.canEdit()). «Админ» — DICTS:
// senior-operator входит и видит свои 4 словаря (список отдаёт сервер),
// полный реестр — у admin.
export { DICTS, OPER };

// Иконки подобраны 1:1 с оригиналом GTport (CompactSidebar.tsx): сплошные (fill).
// train / ship / warehouse — кастомные (см. core/config/custom-icons.ts), т.к. в
// наборе ant-design их нет; swap/global/line-chart/bar-chart — только outline.
export const DISPATCHER_NAV: NavItem[] = [
  { path: 'home',           label: 'Главная',               icon: 'home',         theme: 'fill',    roles: ANY,  external: true },
  // «Дислокация» убрана из меню (решение владельца): статус системы, «Обновить
  // из АСУ» и «Приём ЛК» переехали на главную, в колонку «Оперативка». Маршрут
  // /dislocation жив (прямой ссылкой) — страница осталась как запасной путь.
  // «Пропавшие вагоны» страницей больше не существуют (решение владельца
  // 10.08.2026): пропавшие живут в станционных карточках «Кандидаты на
  // прибытие» на главной. «Грузовая работа» — модалка из карточки
  // «Информация», отдельной страницы нет.
  // «Справки» убраны из меню (решение владельца 29.07.2026): в gtport вкладка
  // была байт-в-байт поднабором «Справок и отчётов» — СМС-формы влиты туда
  // (features/reports), маршрут /reference редиректит на /reports.
  { path: 'rearrangement',  label: 'Перестановки',          icon: 'swap',         theme: 'outline', roles: OPER },
  { path: 'plan',           label: 'План подвода',          icon: 'train',        theme: 'fill',    roles: OPER },
  { path: 'warehouse',      label: 'Склад',                 icon: 'warehouse',    theme: 'fill',    roles: OPER },
  { path: 'shipments',      label: 'Судовые партии',        icon: 'ship',         theme: 'fill',    roles: OPER },
  { path: 'daily-work',     label: 'Работа за сутки',       icon: 'clock-circle', theme: 'fill',    roles: OPER },
  { path: 'operator-tools', label: 'Инструменты оператора', icon: 'tool',         theme: 'fill',    roles: OPER },
  { path: 'maps',           label: 'Карты',                 icon: 'global',       theme: 'outline', roles: OPER },
  { path: 'forecasts',      label: 'Прогнозы',              icon: 'line-chart',   theme: 'outline', roles: OPER },
  { path: 'reports',        label: 'Справки и отчёты',      icon: 'bar-chart',    theme: 'outline', roles: OPER },
  { path: 'admin',          label: 'Админ',                 icon: 'setting',      theme: 'fill',    roles: DICTS },
];
