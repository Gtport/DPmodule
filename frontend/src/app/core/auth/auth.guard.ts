import { inject } from '@angular/core';
import { CanActivateFn, Router } from '@angular/router';
import { AuthService } from './auth.service';

/**
 * Не залогинен → редирект на hosted-вход Keycloak (обычно не срабатывает: init
 * login-required уже требует вход при загрузке). Залогинен → RBAC по route.data.roles.
 * Использование: { path: 'admin', canActivate: [authGuard], data: { roles: ['admin'] } }
 */
export const authGuard: CanActivateFn = (route, state) => {
  const auth = inject(AuthService);
  const router = inject(Router);

  if (!auth.authenticated()) {
    void auth.login(window.location.origin + state.url);
    return false;
  }

  const roles = (route.data?.['roles'] as string[] | undefined) ?? [];
  if (!auth.hasAnyRole(roles)) {
    return router.createUrlTree(['/forbidden']);
  }
  return true;
};
