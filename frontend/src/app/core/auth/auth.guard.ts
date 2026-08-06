import { inject } from '@angular/core';
import { CanActivateFn, Router } from '@angular/router';
import { AuthService } from './auth.service';
import { ANY, RoleSet } from './roles';

/**
 * Не залогинен → редирект на hosted-вход Keycloak (обычно не срабатывает: init
 * login-required уже требует вход при загрузке). Залогинен → RBAC по
 * route.data.roles (RoleSet — пара списков client/realm, см. roles.ts).
 * Использование: { path: 'admin', canActivate: [authGuard], data: { roles: ADMIN } }
 * Легаси-форма — плоский массив — трактуется как realm-список (НЕ как client:
 * складывать схемы в один массив нельзя, см. «Главную ловушку»).
 */
export const authGuard: CanActivateFn = (route, state) => {
  const auth = inject(AuthService);
  const router = inject(Router);

  if (!auth.authenticated()) {
    void auth.login(window.location.origin + state.url);
    return false;
  }

  const data = route.data?.['roles'] as RoleSet | string[] | undefined;
  const set: RoleSet = !data ? ANY : Array.isArray(data) ? { client: [], realm: data } : data;
  if (!auth.allows(set)) {
    return router.createUrlTree(['/forbidden']);
  }
  return true;
};
