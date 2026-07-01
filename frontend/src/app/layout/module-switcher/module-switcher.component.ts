import { Component, computed, inject } from '@angular/core';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzDropDownModule } from 'ng-zorro-antd/dropdown';
import { NzMenuModule } from 'ng-zorro-antd/menu';
import { AuthService } from '../../core/auth/auth.service';
import { PLATFORM_MODULES } from '../../core/config/modules.config';
import { environment } from '../../../environments/environment';

/**
 * Переход между модулями платформы (IQPort §1 — единый вход, SSO).
 * Показывает только те модули, к которым у пользователя есть роль, и прячет текущий.
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

  readonly available = computed(() =>
    PLATFORM_MODULES.filter(
      (m) => m.id !== environment.moduleId && this.auth.hasAnyRole(m.roles),
    ),
  );
}
