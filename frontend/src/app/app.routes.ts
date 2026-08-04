import { Routes } from '@angular/router';
import { authGuard } from './core/auth/auth.guard';
import { ShellComponent } from './layout/shell/shell.component';
import { PlaceholderComponent } from './features/placeholder/placeholder.component';
import { ADMIN, OPER, DISPATCHER_NAV } from './layout/shell/nav.config';

// Разделы, перенесённые из заглушки на реальный экран — исключаем из
// автогенерации ниже и подключаем явно (см. routes).
const IMPLEMENTED_PATHS = new Set(['dislocation', 'missing', 'rearrangement', 'plan', 'reports', 'forecasts', 'admin', 'operator-tools']);

// Разделы диспетчера — генерируем из реестра навигации: каждый пункт (кроме
// external, напр. home, и уже перенесённых из IMPLEMENTED_PATHS) → маршрут на
// общую заглушку с title/icon и RBAC по ролям. При переносе раздела из GTport
// добавляем его path в IMPLEMENTED_PATHS и подключаем реальный компонент ниже.
const dispatcherRoutes: Routes = DISPATCHER_NAV
  .filter((i) => !i.external && !IMPLEMENTED_PATHS.has(i.path))
  .map((i) => ({
    path: i.path,
    component: PlaceholderComponent,
    canActivate: [authGuard],
    data: { title: i.label, icon: i.icon, roles: i.roles },
  }));

// Маршрута /login нет намеренно: вход — hosted-страница Keycloak
// (Auth Code + PKCE, см. core/auth/auth.service.ts). Своя форма входа была бы
// вторым способом собирать пароль и на стенде не заработала бы — Direct access
// grants в платформенной модели выключен.
export const routes: Routes = [
  {
    path: '',
    component: ShellComponent,
    canActivate: [authGuard],
    children: [
      { path: '', redirectTo: 'home', pathMatch: 'full' },
      {
        path: 'home',
        loadComponent: () =>
          import('./features/home/home.component').then((m) => m.HomeComponent),
      },
      {
        path: 'dislocation',
        loadComponent: () =>
          import('./features/dislocation/dislocation.component').then((m) => m.DislocationComponent),
        canActivate: [authGuard],
        data: { roles: OPER },
      },
      {
        path: 'missing',
        loadComponent: () =>
          import('./features/missing/missing.component').then((m) => m.MissingComponent),
        canActivate: [authGuard],
        data: { roles: OPER },
      },
      {
        path: 'rearrangement',
        loadComponent: () =>
          import('./features/rearrangement/rearrangement.component').then((m) => m.RearrangementComponent),
        canActivate: [authGuard],
        data: { roles: OPER },
      },
      {
        path: 'plan',
        loadComponent: () =>
          import('./features/plan/plan.component').then((m) => m.PlanComponent),
        canActivate: [authGuard],
        data: { roles: OPER },
      },
      {
        path: 'reports',
        loadComponent: () =>
          import('./features/reports/reports.component').then((m) => m.ReportsComponent),
        canActivate: [authGuard],
        data: { roles: OPER },
      },
      // Старый адрес страницы «Справки» — влита в «Справки и отчёты».
      { path: 'reference', redirectTo: 'reports' },
      {
        // «Прогнозы»: вкладки «Новый прогноз» (gtport PrognozNew) и
        // «Прогноз прибытия/выгрузки» (gtport страница GT).
        path: 'forecasts',
        loadComponent: () =>
          import('./features/forecasts/forecasts-page.component').then((m) => m.ForecastsPageComponent),
        canActivate: [authGuard],
        data: { roles: OPER },
      },
      {
        // «Инструменты оператора»: вкладки «Поезда в движении» (gtport
        // OperatorToolsDislocation) и «Работа с историческими данными»
        // (gtport OperatorToolsHistory).
        path: 'operator-tools',
        loadComponent: () =>
          import('./features/operator-tools/operator-tools.component').then((m) => m.OperatorToolsComponent),
        canActivate: [authGuard],
        data: { roles: OPER },
      },
      {
        path: 'admin',
        loadComponent: () =>
          import('./features/admin/admin.component').then((m) => m.AdminComponent),
        canActivate: [authGuard],
        data: { roles: ADMIN },
      },
      ...dispatcherRoutes,
      {
        path: 'forbidden',
        loadComponent: () =>
          import('./pages/forbidden/forbidden.component').then((m) => m.ForbiddenComponent),
      },
    ],
  },
  { path: '**', redirectTo: '' },
];
