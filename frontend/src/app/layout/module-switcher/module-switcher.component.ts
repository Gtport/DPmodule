import { Component, computed, inject } from '@angular/core';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzDropDownModule } from 'ng-zorro-antd/dropdown';
import { NzMenuModule } from 'ng-zorro-antd/menu';
import { AuthService } from '../../core/auth/auth.service';
import { PLATFORM_MODULES, PlatformModule } from '../../core/config/modules.config';
import { environment } from '../../../environments/environment';

/**
 * Переход между модулями платформы (IQPort §1 — единый вход, SSO).
 * Показывает только те модули, которые развёрнуты (available) и к которым у
 * пользователя есть роль; текущий модуль прячет. Первым пунктом — возврат на
 * портал-лаунчер (роли не проверяем: портал открыт всем вошедшим, недоступные
 * плитки он гасит сам).
 *
 * Переход — обычная ссылка на другой origin: сессия одна на весь realm iqport,
 * повторный вход не потребуется, пока адрес Keycloak у модулей совпадает
 * (docs/PORTAL_INTEGRATION.md §3).
 */
@Component({
  selector: 'app-module-switcher',
  imports: [NzButtonModule, NzIconModule, NzDropDownModule, NzMenuModule],
  template: `
    <a nz-dropdown [nzDropdownMenu]="menu">
      <button nz-button nzType="text">
        <span nz-icon nzType="appstore"></span>
        <span class="label">Модули</span>
      </button>
    </a>
    <nz-dropdown-menu #menu="nzDropdownMenu">
      <ul nz-menu class="switcher">
        @if (portalUrl) {
          <li nz-menu-item>
            <a [href]="portalUrl">
              <span nz-icon nzType="global"></span>
              <span>Портал — все модули</span>
            </a>
          </li>
          <li nz-menu-divider></li>
        }
        @for (m of available(); track m.id) {
          <li nz-menu-item>
            <a [href]="m.url">
              <span nz-icon [nzType]="m.icon"></span>
              <span>{{ m.short }} — {{ m.name }}</span>
            </a>
          </li>
        }
      </ul>
    </nz-dropdown-menu>
  `,
  styles: [`
    .label { margin-left: var(--space-xs); }
    .switcher { box-shadow: var(--shadow-xl); border-radius: var(--radius-lg); }
    .switcher a { color: var(--color-text); }
    .switcher nz-icon { margin-right: var(--space-sm); }
  `],
})
export class ModuleSwitcherComponent {
  private readonly auth = inject(AuthService);

  readonly portalUrl = environment.portalUrl;

  readonly available = computed(() =>
    PLATFORM_MODULES.filter(
      (m) => m.available && m.id !== environment.moduleId && this.allowsModule(m),
    ),
  );

  /** Доступ к модулю каталога: его realm-роли ИЛИ client-роли его клиента
   *  (полка iqport-<id> в resource_access — конвенция портала moduleClientId).
   *  Схемы проверяются раздельно; оба списка пусты = доступ всем вошедшим. */
  private allowsModule(m: PlatformModule): boolean {
    const clientRoles = m.clientRoles ?? [];
    if (!m.roles.length && !clientRoles.length) return true;
    const mine = this.auth.clientRolesOf(`iqport-${m.id}`);
    return clientRoles.some((r) => mine.includes(r)) || this.auth.hasAnyRole(m.roles);
  }
}
