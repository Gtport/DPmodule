import { inject } from '@angular/core';
import { CanActivateFn, Router } from '@angular/router';
import { AuthService } from './auth.service';

/**
 * Не залогинен → уводим на свою форму /login (с returnUrl, чтобы вернуть после входа).
 * Залогинен → проверяем RBAC: роль из route.data.roles.
 * Использование: { path: 'admin', canActivate: [authGuard], data: { roles: ['admin'] } }
 */
export const authGuard: CanActivateFn = (route, state) => {
  const auth = inject(AuthService);
  const router = inject(Router);

  if (!auth.authenticated()) {
    return router.createUrlTree(['/login'], { queryParams: { returnUrl: state.url } });
  }

  const roles = (route.data?.['roles'] as string[] | undefined) ?? [];
  if (!auth.hasAnyRole(roles)) {
    return router.createUrlTree(['/forbidden']);
  }
  return true;
};
