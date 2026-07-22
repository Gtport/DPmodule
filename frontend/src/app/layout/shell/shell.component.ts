import { Component, computed, inject, signal } from '@angular/core';
import { UpperCasePipe } from '@angular/common';
import { RouterOutlet, RouterLink, RouterLinkActive } from '@angular/router';
import { NzLayoutModule } from 'ng-zorro-antd/layout';
import { NzMenuModule } from 'ng-zorro-antd/menu';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzDropDownModule } from 'ng-zorro-antd/dropdown';
import { AuthService } from '../../core/auth/auth.service';
import { ModuleSwitcherComponent } from '../module-switcher/module-switcher.component';
import { DISPATCHER_NAV } from './nav.config';
import { environment } from '../../../environments/environment';

/**
 * Shell-layout в стиле шаблона IQPort (§7.2): topbar + сворачиваемый nz-sider с
 * inline-меню + контент + footer. Пункты — из реестра навигации (RBAC по ролям).
 * По умолчанию сайдбар компактный (только иконки); при наведении в свёрнутом
 * виде ng-zorro сам показывает подсказку с названием. Кнопка-триггер в топбаре
 * разворачивает меню с подписями.
 */
@Component({
  selector: 'app-shell',
  imports: [
    UpperCasePipe,
    RouterOutlet, RouterLink, RouterLinkActive,
    NzLayoutModule, NzMenuModule, NzIconModule, NzButtonModule, NzTooltipModule, NzDropDownModule,
    ModuleSwitcherComponent,
  ],
  template: `
    <nz-layout class="root">
      <nz-header class="topbar">
        <button nz-button nzType="text" class="trigger" (click)="collapsed.set(!collapsed())">
          <span nz-icon nzType="menu"></span>
        </button>
        <span class="brand">IQPort · {{ moduleId | uppercase }}</span>
        <span class="spacer"></span>

        <app-module-switcher />

        <a nz-dropdown [nzDropdownMenu]="userMenu" class="user">
          <span nz-icon nzType="user"></span>
          <span class="username">{{ auth.username() }}</span>
        </a>
        <nz-dropdown-menu #userMenu="nzDropdownMenu">
          <ul nz-menu>
            <li nz-menu-item (click)="auth.accountManagement()">
              <span nz-icon nzType="setting"></span> Профиль
            </li>
            <li nz-menu-item (click)="auth.logout()">
              <span nz-icon nzType="logout"></span> Выйти
            </li>
          </ul>
        </nz-dropdown-menu>
      </nz-header>

      <nz-layout>
        <nz-sider class="sidebar" nzCollapsible [nzCollapsed]="collapsed()" [nzTrigger]="null"
                  [nzWidth]="sidebarWidth" [nzCollapsedWidth]="collapsedWidth">
          <ul nz-menu nzMode="inline" nzTheme="light" [nzInlineCollapsed]="collapsed()">
            @for (item of navItems(); track item.path) {
              <li nz-menu-item [routerLink]="'/' + item.path"
                  routerLinkActive="ant-menu-item-selected" [nzMatchRouter]="true"
                  [nzMatchRouterExact]="false"
                  nz-tooltip [nzTooltipTitle]="collapsed() ? item.label : ''"
                  nzTooltipPlacement="right">
                <span nz-icon [nzType]="item.icon" [nzTheme]="item.theme" class="nav-icon"></span>
                <span>{{ item.label }}</span>
              </li>
            }
          </ul>
        </nz-sider>

        <nz-content class="content">
          <main><router-outlet /></main>
          <nz-footer class="footer">IQPort · {{ moduleId | uppercase }} · © 2026</nz-footer>
        </nz-content>
      </nz-layout>
    </nz-layout>
  `,
  styles: [`
    :host { display: block; height: 100vh; }
    .root { height: 100%; }
    .topbar {
      display: flex; align-items: center; gap: var(--space-sm);
      height: var(--layout-navbar-height);
      padding: 0 var(--space-md);
      background: rgba(255, 255, 255, 0.95);
      backdrop-filter: blur(5px);
      box-shadow: var(--shadow-sm);
      position: sticky; top: 0; z-index: var(--z-sticky);
    }
    .trigger { font-size: var(--font-size-card-title); }
    .brand { font-weight: 600; color: var(--color-text); }
    .spacer { flex: 1 1 auto; }
    .user { display: inline-flex; align-items: center; gap: var(--space-xs); color: var(--color-text); cursor: pointer; }
    .username { margin-left: var(--space-xs); }
    .sidebar { background: var(--color-bg-surface); border-right: 1px solid var(--color-border-light); }
    /* Иконки меню — нейтрально-серые (§2.2), синие на активном пункте.
       Размер на 20% больше базового (--layout-icon-size 16px → 19.2px, решение
       владельца); !important — перебиваем размер из темы ant-menu. */
    .nav-icon { color: var(--color-icon-default); transition: color .2s ease;
                font-size: calc(var(--layout-icon-size) * 1.2) !important; }
    .ant-menu-item-selected .nav-icon { color: var(--color-primary); }
    .content { display: flex; flex-direction: column; overflow: auto; }
    main { flex: 1 1 auto; padding: var(--space-md); }
    .footer { color: var(--color-text-muted); font-size: var(--font-size-sm); }
  `],
})
export class ShellComponent {
  readonly auth = inject(AuthService);
  readonly collapsed = signal(true); // по умолчанию компактный
  readonly moduleId = environment.moduleId;
  // Ширины берём из токенов (§8: только переменные, не числа).
  readonly sidebarWidth = 'var(--layout-sidebar-width)';
  readonly collapsedWidth = 60;

  // Пункты меню из реестра, отфильтрованные по ролям текущего пользователя
  // (реактивно — auth.roles() внутри hasAnyRole является сигналом).
  readonly navItems = computed(() =>
    DISPATCHER_NAV.filter((i) => this.auth.hasAnyRole(i.roles)),
  );
}
