import { HttpClient, HttpParams } from '@angular/common/http';
import { Injectable, computed, inject, signal } from '@angular/core';
import { Router } from '@angular/router';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../../environments/environment';

/**
 * Авторизация формой ВНУТРИ Angular (своя страница /login), без редиректа на
 * хостед-форму Keycloak. Идентити-провайдер остаётся Keycloak (realm iqport):
 * логин/пароль постятся на token endpoint потоком Resource Owner Password
 * Credentials (grant_type=password, Direct Access Grants на клиенте).
 *
 * ВАЖНО: на клиенте `iqport-<module>` в Keycloak должен быть включён
 * "Direct access grants" (directAccessGrantsEnabled=true) — иначе token endpoint
 * вернёт unauthorized_client.
 *
 * Минусы относительно Auth Code + PKCE: нет SSO между модулями (в каждый логинишься
 * отдельно) и пароль проходит через JS. Это осознанный выбор шаблона.
 */
interface TokenResponse {
  access_token: string;
  refresh_token: string;
  expires_in: number;
}

interface JwtPayload {
  exp: number;
  preferred_username?: string;
  name?: string;
  realm_access?: { roles?: string[] };
}

const REFRESH_KEY = 'iqport.refresh_token';

@Injectable({ providedIn: 'root' })
export class AuthService {
  private readonly http = inject(HttpClient);
  private readonly router = inject(Router);

  private readonly base =
    `${environment.keycloak.url}/realms/${environment.keycloak.realm}/protocol/openid-connect`;

  private accessToken: string | null = null;
  private refreshToken: string | null = localStorage.getItem(REFRESH_KEY);

  private readonly payload = signal<JwtPayload | null>(null);

  /** Реактивные геттеры для навбара/гвардов/страниц. */
  readonly authenticated = computed(() => this.payload() !== null);
  readonly username = computed(() => {
    const p = this.payload();
    return p?.name || p?.preferred_username || 'user';
  });
  readonly roles = computed(() => this.payload()?.realm_access?.roles ?? []);

  /**
   * Вызывается в provideAppInitializer. Если в localStorage остался refresh-токен
   * (прошлая сессия) — молча восстанавливает её, чтобы не логиниться после F5.
   * Любая ошибка = просто разлогинен, без редиректа (этим займётся guard).
   */
  async restoreSession(): Promise<void> {
    if (!this.refreshToken) return;
    try {
      await this.refresh();
    } catch {
      this.clearSession();
    }
  }

  /** Логин формой: grant_type=password на token endpoint Keycloak. */
  async login(username: string, password: string): Promise<void> {
    const body = new HttpParams()
      .set('grant_type', 'password')
      .set('client_id', environment.keycloak.clientId)
      .set('scope', 'openid')
      .set('username', username)
      .set('password', password);
    const res = await firstValueFrom(this.post(body));
    this.setSession(res);
  }

  /** Возвращает валидный access-токен для интерсептора (тихо обновляет при истечении). */
  async getValidToken(): Promise<string | null> {
    if (!this.accessToken) return null;
    if (this.isExpiringSoon()) {
      try {
        await this.refresh();
      } catch {
        await this.logout();
        return null;
      }
    }
    return this.accessToken;
  }

  hasAnyRole(roles: string[]): boolean {
    if (!roles.length) return true;
    const mine = this.roles();
    return roles.some((r) => mine.includes(r));
  }

  /** Отзывает сессию в Keycloak, чистит локально и уводит на /login. */
  async logout(): Promise<void> {
    if (this.refreshToken) {
      const body = new HttpParams()
        .set('client_id', environment.keycloak.clientId)
        .set('refresh_token', this.refreshToken);
      await firstValueFrom(
        this.http.post(`${this.base}/logout`, body.toString(), { headers: this.formHeaders }),
      ).catch(() => undefined);
    }
    this.clearSession();
    this.router.navigate(['/login']);
  }

  /** Консоль управления учётной записью Keycloak (Keycloak остаётся IdP). */
  accountManagement(): void {
    window.open(`${environment.keycloak.url}/realms/${environment.keycloak.realm}/account`, '_blank');
  }

  private async refresh(): Promise<void> {
    if (!this.refreshToken) throw new Error('no refresh token');
    const body = new HttpParams()
      .set('grant_type', 'refresh_token')
      .set('client_id', environment.keycloak.clientId)
      .set('refresh_token', this.refreshToken);
    const res = await firstValueFrom(this.post(body));
    this.setSession(res);
  }

  private post(body: HttpParams) {
    return this.http.post<TokenResponse>(`${this.base}/token`, body.toString(), {
      headers: this.formHeaders,
    });
  }

  private get formHeaders() {
    return { 'Content-Type': 'application/x-www-form-urlencoded' };
  }

  private setSession(res: TokenResponse): void {
    this.accessToken = res.access_token;
    this.refreshToken = res.refresh_token;
    localStorage.setItem(REFRESH_KEY, res.refresh_token);
    this.payload.set(this.decode(res.access_token));
  }

  private clearSession(): void {
    this.accessToken = null;
    this.refreshToken = null;
    localStorage.removeItem(REFRESH_KEY);
    this.payload.set(null);
  }

  private isExpiringSoon(minValiditySec = 30): boolean {
    const exp = this.payload()?.exp;
    if (!exp) return true;
    return Date.now() >= exp * 1000 - minValiditySec * 1000;
  }

  private decode(token: string): JwtPayload | null {
    try {
      const json = atob(token.split('.')[1].replace(/-/g, '+').replace(/_/g, '/'));
      return JSON.parse(decodeURIComponent(escape(json))) as JwtPayload;
    } catch {
      return null;
    }
  }
}
