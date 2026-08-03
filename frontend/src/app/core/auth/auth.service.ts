import { Injectable, computed, effect, inject, signal } from '@angular/core';
import Keycloak from 'keycloak-js';
import { KEYCLOAK_EVENT_SIGNAL } from 'keycloak-angular';
import { OPER, normalizeRole } from './roles';

/**
 * Фасад над keycloak-js (инициализируется через provideKeycloak в app.config).
 * Вход — Authorization Code + PKCE на hosted-странице Keycloak: SPA НЕ видит
 * пароль и не хранит refresh-токен.
 *
 * ⚠️ Так работает вся платформа IQPort (шаблон iqport-frontend-template), и это
 * не косметика. Прежний вариант — своя форма и grant_type=password (ROPC):
 * пароль проходил через наш код, refresh-токен лежал в localStorage, SSO между
 * модулями не было (в каждый логинились отдельно), а на клиенте требовался
 * включённый Direct access grants — в платформенной модели он не нужен и
 * обычно выключен. Заодно объяснилась находка тимлида: сломанный redirect_uri
 * никто не замечал, потому что путь с password grant его не задевает
 * (docs/KEYCLOAK_HANDOVER.md §1).
 *
 * Публичный API сохранён (authenticated / username / roles / canEdit /
 * hasAnyRole / logout / accountManagement), поэтому shell, guard и экраны
 * не менялись.
 */
interface JwtPayload {
  exp: number;
  preferred_username?: string;
  name?: string;
  realm_access?: { roles?: string[] };
}

@Injectable({ providedIn: 'root' })
export class AuthService {
  private readonly kc = inject(Keycloak);
  // Сигнал событий keycloak-angular — дёргается на Ready/AuthSuccess/Refresh/Logout/…
  private readonly kcEvent = inject(KEYCLOAK_EVENT_SIGNAL);
  private readonly payload = signal<JwtPayload | null>(this.currentPayload());

  /** Реактивные геттеры для навбара/гвардов/страниц. */
  readonly authenticated = computed(() => this.payload() !== null);
  readonly username = computed(() => {
    const p = this.payload();
    return p?.name || p?.preferred_username || 'user';
  });
  /** Роли из токена, нормализованные к нынешним именам *_dpport (см. roles.ts). */
  readonly roles = computed(() =>
    (this.payload()?.realm_access?.roles ?? []).map(normalizeRole),
  );
  /** Порог правок: набор OPER (operator_dpport или admin_dpport — иерархии нет).
   *  Клиентские роли видят экраны без кнопок действий. */
  readonly canEdit = computed(() => this.hasAnyRole(OPER));

  constructor() {
    // На любое событие Keycloak пересобираем payload из актуального токена.
    effect(() => {
      this.kcEvent();
      this.payload.set(this.currentPayload());
    });
  }

  private currentPayload(): JwtPayload | null {
    return this.kc.authenticated ? ((this.kc.tokenParsed as JwtPayload) ?? null) : null;
  }

  /**
   * Есть ли хотя бы одна из требуемых ролей. Нормализуются ОБЕ стороны (как
   * auth.Claims.HasRole на бэкенде): тогда старое имя, оставшееся в route data
   * или в конфиге (modules.config.ts), продолжает совпадать с нынешней ролью
   * из токена. Пустой список требуемых ролей = «доступно любому залогиненному».
   */
  hasAnyRole(roles: string[]): boolean {
    if (!roles.length) return true;
    const mine = this.roles();
    return roles.some((r) => mine.includes(normalizeRole(r)));
  }

  /** Редирект на hosted-вход Keycloak (Auth Code + PKCE). */
  login(redirectUri: string = window.location.href): Promise<void> {
    return this.kc.login({ redirectUri });
  }

  /** Валидный access-токен (тихо обновляется). Для кода вне HTTP-интерсептора. */
  async getValidToken(): Promise<string | null> {
    try {
      await this.kc.updateToken(30);
    } catch {
      return null;
    }
    return this.kc.token ?? null;
  }

  logout(): Promise<void> {
    return this.kc.logout({ redirectUri: window.location.origin });
  }

  /** Консоль управления учётной записью Keycloak. */
  accountManagement(): void {
    void this.kc.accountManagement();
  }
}
