import { Injectable, computed, effect, inject, signal } from '@angular/core';
import Keycloak from 'keycloak-js';
import { KEYCLOAK_EVENT_SIGNAL } from 'keycloak-angular';
import { environment } from '../../../environments/environment';
import { OPER, RoleSet, normalizeRole } from './roles';

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
  resource_access?: Record<string, { roles?: string[] }>;
}

@Injectable({ providedIn: 'root' })
export class AuthService {
  private readonly kc = inject(Keycloak);
  // Сигнал событий keycloak-angular — дёргается на Ready/AuthSuccess/Refresh/Logout/…
  private readonly kcEvent = inject(KEYCLOAK_EVENT_SIGNAL);
  private readonly payload = signal<JwtPayload | null>(this.currentPayload());
  // Наш клиент Keycloak — тот же, которым логинимся (provideKeycloak).
  private readonly clientId = environment.keycloak.clientId;

  /** Реактивные геттеры для навбара/гвардов/страниц. */
  readonly authenticated = computed(() => this.payload() !== null);
  readonly username = computed(() => {
    const p = this.payload();
    return p?.name || p?.preferred_username || 'user';
  });
  /** REALM-роли из токена (старая схема), нормализованные к именам *_dpport. */
  readonly roles = computed(() =>
    (this.payload()?.realm_access?.roles ?? []).map(normalizeRole),
  );
  /** CLIENT-роли нашего клиента (новая схема), дословно. Два списка, а не один
   *  — см. «Главную ловушку» в roles.ts. */
  readonly clientRoles = computed(() => this.clientRolesOf(this.clientId));
  /** Порог правок: набор OPER. Клиентские роли видят экраны без кнопок действий. */
  readonly canEdit = computed(() => this.allows(OPER));

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
   * Пускает ли набор доступа (пара списков — см. roles.ts): есть нужная
   * CLIENT-роль ИЛИ нужная REALM-роль, каждый список сверяется со своей полкой
   * токена — зеркало auth.Claims.Allows на бэкенде. Пустой набор (оба списка
   * пусты) = «доступно любому залогиненному».
   */
  allows(set: RoleSet): boolean {
    if (!set.client.length && !set.realm.length) return true;
    const mineClient = this.clientRoles();
    return set.client.some((r) => mineClient.includes(r)) || this.hasAnyRole(set.realm);
  }

  /**
   * REALM-сторона проверки: есть ли хотя бы одна из требуемых realm-ролей.
   * Нормализуются ОБЕ стороны (как auth.Claims.HasRole на бэкенде): старое имя
   * из конфига (modules.config.ts) продолжает совпадать с нынешней ролью из
   * токена. ⚠️ Пустой список здесь = отказ («разрешено никому»), правило
   * «пусто = всем» живёт в allows.
   */
  hasAnyRole(roles: string[]): boolean {
    const mine = this.roles();
    return roles.some((r) => mine.includes(normalizeRole(r)));
  }

  /** CLIENT-роли произвольного клиента каталога (для module-switcher: чужие
   *  полки resource_access тоже приходят в токене, пока Full scope включён). */
  clientRolesOf(clientId: string): string[] {
    return this.payload()?.resource_access?.[clientId]?.roles ?? [];
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
