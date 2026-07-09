import { Routes } from '@angular/router';
import { authGuard } from './core/auth/auth.guard';
import { ShellComponent } from './layout/shell/shell.component';
import { PlaceholderComponent } from './features/placeholder/placeholder.component';
import { DISP, DISPATCHER_NAV } from './layout/shell/nav.config';

// Разделы, перенесённые из заглушки на реальный экран — исключаем из
// автогенерации ниже и подключаем явно (см. routes).
const IMPLEMENTED_PATHS = new Set(['dislocation', 'plan']);

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

export const routes: Routes = [
  {
    path: 'login',
    loadComponent: () =>
      import('./pages/login/login.component').then((m) => m.LoginComponent),
  },
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
        data: { roles: DISP },
      },
      {
        path: 'plan',
        loadComponent: () =>
          import('./features/plan/plan.component').then((m) => m.PlanComponent),
        canActivate: [authGuard],
        data: { roles: DISP },
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
