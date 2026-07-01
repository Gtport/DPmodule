import { HttpInterceptorFn } from '@angular/common/http';
import { inject } from '@angular/core';
import { from, switchMap } from 'rxjs';
import { AuthService } from './auth.service';
import { environment } from '../../../environments/environment';

/**
 * Bearer вешаем ТОЛЬКО на бэкенд модуля (environment.apiBaseUrl), который сидит на
 * Keycloak JWT. Сторонние API (если появятся) сюда не попадают. Перед запросом
 * токен при необходимости тихо обновляется (updateToken), чтобы не ловить 401.
 */
export const authInterceptor: HttpInterceptorFn = (req, next) => {
  const isApiCall =
    req.url.startsWith(environment.apiBaseUrl) || req.url.startsWith('/api');
  if (!isApiCall) {
    return next(req);
  }

  const auth = inject(AuthService);
  return from(auth.getValidToken()).pipe(
    switchMap((token) => {
      const authedReq = token
        ? req.clone({ setHeaders: { Authorization: `Bearer ${token}` } })
        : req;
      return next(authedReq);
    }),
  );
};
