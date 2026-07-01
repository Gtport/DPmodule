import { Component, inject, signal } from '@angular/core';
import { UpperCasePipe } from '@angular/common';
import { RouterOutlet, RouterLink, RouterLinkActive } from '@angular/router';
import { NzLayoutModule } from 'ng-zorro-antd/layout';
import { NzMenuModule } from 'ng-zorro-antd/menu';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzDropDownModule } from 'ng-zorro-antd/dropdown';
import { AuthService } from '../../core/auth/auth.service';
import { ModuleSwitcherComponent } from '../module-switcher/module-switcher.component';
import { environment } from '../../../environments/environment';

/** Shell-layout: navbar + collapsible sidebar + content + footer (IQPort §7.2). */
@Component({
  selector: 'app-shell',
  imports: [
    UpperCasePipe,
    RouterOutlet, RouterLink, RouterLinkActive,
    NzLayoutModule, NzMenuModule, NzIconModule, NzButtonModule, NzDropDownModule,
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
          <ul nz-menu nzMode="inline" nzTheme="light">
            <li nz-menu-item routerLink="/home" routerLinkActive="ant-menu-item-selected">
              <span nz-icon nzType="dashboard"></span>
              <span>Главная</span>
            </li>
            <!-- Пункты модуля добавляются здесь -->
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
    .user { display: inline-flex; align-items: center; gap: var(--space-xs); color: var(--color-text); }
    .username { margin-left: var(--space-xs); }
    .sidebar { background: var(--color-bg-surface); border-right: 1px solid var(--color-border-light); }
    .content { display: flex; flex-direction: column; overflow: auto; }
    main { flex: 1 1 auto; padding: var(--space-md); }
    .footer { color: var(--color-text-muted); font-size: var(--font-size-sm); }
  `],
})
export class ShellComponent {
  readonly auth = inject(AuthService);
  readonly collapsed = signal(false);
  readonly moduleId = environment.moduleId;
  // Ширины берём из токенов (§8: только переменные, не числа).
  readonly sidebarWidth = 'var(--layout-sidebar-width)';
  readonly collapsedWidth = 60;
}
