import { Component, computed, inject } from '@angular/core';
import { UpperCasePipe } from '@angular/common';
import { RouterOutlet, RouterLink, RouterLinkActive } from '@angular/router';
import { NzLayoutModule } from 'ng-zorro-antd/layout';
import { NzMenuModule } from 'ng-zorro-antd/menu';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzDropDownModule } from 'ng-zorro-antd/dropdown';
import { AuthService } from '../../core/auth/auth.service';
import { ModuleSwitcherComponent } from '../module-switcher/module-switcher.component';
import { DISPATCHER_NAV } from './nav.config';
import { environment } from '../../../environments/environment';

/** Человекочитаемые названия ролей для бейджа/тултипа (буква + подпись). */
const ROLE_LABELS: Record<string, string> = {
  administrator: 'Администратор',
  dispatcher: 'Диспетчер',
  manager: 'Менеджер',
  operator: 'Оператор',
};

/**
 * Shell-layout в стиле GTport: слева — компактный сайдбар 60px (иконки +
 * всплывающие подписи-тултипы справа), справа — топбар и контент.
 */
@Component({
  selector: 'app-shell',
  imports: [
    UpperCasePipe,
    RouterOutlet, RouterLink, RouterLinkActive,
    NzLayoutModule, NzMenuModule, NzIconModule, NzTooltipModule, NzDropDownModule,
    ModuleSwitcherComponent,
  ],
  template: `
    <div class="shell">
      <aside class="sidebar">
        <div class="sidebar-top">
          <a class="logo" routerLink="/home"
             nz-tooltip nzTooltipTitle="IQPort" nzTooltipPlacement="right">IQ</a>
        </div>

        <nav class="sidebar-nav">
          <ul>
            @for (item of navItems(); track item.path) {
              <li>
                <a class="nav-link" [routerLink]="'/' + item.path"
                   routerLinkActive="active"
                   nz-tooltip [nzTooltipTitle]="item.label" nzTooltipPlacement="right">
                  <span nz-icon [nzType]="item.icon" [nzTheme]="item.theme" class="nav-icon"></span>
                </a>
              </li>
            }
          </ul>
        </nav>

        <div class="sidebar-footer">
          <div class="role-badge"
               nz-tooltip [nzTooltipTitle]="roleTooltip()" nzTooltipPlacement="right">
            {{ roleLetter() }}
          </div>
        </div>
      </aside>

      <nz-layout class="main">
        <nz-header class="topbar">
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

        <nz-content class="content">
          <main><router-outlet /></main>
          <nz-footer class="footer">IQPort · {{ moduleId | uppercase }} · © 2026</nz-footer>
        </nz-content>
      </nz-layout>
    </div>
  `,
  styles: [`
    :host { display: block; height: 100vh; }
    .shell { display: flex; height: 100%; }

    /* ---- Компактный сайдбар (стиль GTport) ---- */
    .sidebar {
      flex: 0 0 var(--layout-sidebar-collapsed-width);
      width: var(--layout-sidebar-collapsed-width);
      display: flex; flex-direction: column;
      background: var(--color-sidebar-bg);
      border-right: 1px solid var(--color-sidebar-border);
    }
    .sidebar-top {
      padding: var(--space-md) 0;
      display: flex; justify-content: center;
      border-bottom: 1px solid var(--color-sidebar-border);
    }
    .logo {
      width: 40px; height: 40px; border-radius: var(--radius-lg);
      display: flex; align-items: center; justify-content: center;
      background: var(--color-sidebar-accent-bg);
      color: rgba(255, 255, 255, 1); font-weight: 700;
      font-size: var(--font-size-subtitle); letter-spacing: .5px;
      text-decoration: none; transition: all .3s ease;
    }
    .logo:hover { background: var(--color-sidebar-item-hover-bg); transform: scale(1.05); }
    .logo:active { transform: scale(.95); }

    .sidebar-nav { flex: 1 1 auto; overflow-y: auto; padding: var(--space-md) 0; }
    .sidebar-nav ul { list-style: none; margin: 0; padding: 0; }
    .sidebar-nav li { margin: var(--space-xs) 0; }

    .nav-link {
      position: relative;
      display: flex; align-items: center; justify-content: center;
      width: var(--layout-sidebar-collapsed-width); height: 44px;
      text-decoration: none;
      transition: all .2s ease;
    }
    .nav-link:hover { background: var(--color-sidebar-item-hover-bg); }
    .nav-link:hover .nav-icon { animation: nav-pulse .3s ease; }
    .nav-link.active { background: var(--color-sidebar-active-bg); }
    .nav-link.active::before {
      content: ''; position: absolute; left: 0; top: 50%; transform: translateY(-50%);
      width: 3px; height: 20px; background: rgba(255, 255, 255, 1); border-radius: 0 2px 2px 0;
    }
    /* Иконки всегда сплошные и белые (как в GTport), крупнее для «веса». */
    .nav-icon { font-size: 20px; display: flex; color: rgba(255, 255, 255, 1); transition: all .2s ease; }
    .nav-link.active .nav-icon { transform: scale(1.1); }
    @keyframes nav-pulse { 0% { transform: scale(1); } 50% { transform: scale(1.1); } 100% { transform: scale(1); } }

    .sidebar-footer {
      padding: var(--space-sm) 0; display: flex; justify-content: center;
      border-top: 1px solid var(--color-sidebar-border);
    }
    .role-badge {
      width: 40px; height: 40px; border-radius: var(--radius-lg);
      display: flex; align-items: center; justify-content: center;
      background: var(--color-sidebar-accent-bg);
      color: rgba(255, 255, 255, 1); font-weight: 700; font-size: var(--font-size-card-title);
    }
    /* тонкий скроллбар сайдбара */
    .sidebar-nav::-webkit-scrollbar { width: 3px; }
    .sidebar-nav::-webkit-scrollbar-thumb { background: var(--color-sidebar-item-hover-bg); border-radius: 3px; }

    /* ---- Правая колонка ---- */
    .main { flex: 1 1 auto; min-width: 0; }
    .topbar {
      display: flex; align-items: center; gap: var(--space-sm);
      height: var(--layout-navbar-height); padding: 0 var(--space-md);
      background: var(--color-bg-surface);
      box-shadow: var(--shadow-sm);
      position: sticky; top: 0; z-index: var(--z-sticky);
    }
    .brand { font-weight: 600; color: var(--color-text); }
    .spacer { flex: 1 1 auto; }
    .user { display: inline-flex; align-items: center; gap: var(--space-xs); color: var(--color-text); cursor: pointer; }
    .username { margin-left: var(--space-xs); }
    .content { display: flex; flex-direction: column; overflow: auto; height: calc(100vh - var(--layout-navbar-height)); }
    main { flex: 1 1 auto; padding: var(--space-md); }
    .footer { color: var(--color-text-muted); font-size: var(--font-size-sm); }
  `],
})
export class ShellComponent {
  readonly auth = inject(AuthService);
  readonly moduleId = environment.moduleId;

  // Пункты меню из реестра, отфильтрованные по ролям текущего пользователя
  // (реактивно — auth.roles() внутри hasAnyRole является сигналом).
  readonly navItems = computed(() =>
    DISPATCHER_NAV.filter((i) => this.auth.hasAnyRole(i.roles)),
  );

  // Бейдж роли внизу сайдбара (как индикатор роли в GTport).
  private readonly primaryRole = computed(() =>
    this.auth.roles().find((r) => ROLE_LABELS[r]),
  );
  readonly roleLetter = computed(() => {
    const label = ROLE_LABELS[this.primaryRole() ?? ''];
    return label ? label[0] : '?';
  });
  readonly roleTooltip = computed(() => {
    const role = this.primaryRole();
    return 'Роль: ' + (role ? ROLE_LABELS[role] : '—');
  });
}
