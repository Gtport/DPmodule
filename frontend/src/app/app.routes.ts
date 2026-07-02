import { Routes } from '@angular/router';
import { authGuard } from './core/auth/auth.guard';
import { ShellComponent } from './layout/shell/shell.component';
import { PlaceholderComponent } from './features/placeholder/placeholder.component';
import { DISPATCHER_NAV } from './layout/shell/nav.config';

// Разделы диспетчера — генерируем из реестра навигации: каждый пункт (кроме
// external, напр. home) → маршрут на общую заглушку с title/icon и RBAC по ролям.
// При переносе раздела из GTport заменяем PlaceholderComponent на реальный.
const dispatcherRoutes: Routes = DISPATCHER_NAV
  .filter((i) => !i.external)
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
