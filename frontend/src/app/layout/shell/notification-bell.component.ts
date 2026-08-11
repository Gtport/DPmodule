import { Component, OnDestroy, OnInit, inject, signal } from '@angular/core';
import { Router } from '@angular/router';
import { NzBadgeModule } from 'ng-zorro-antd/badge';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzDropDownModule } from 'ng-zorro-antd/dropdown';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { AuthService } from '../../core/auth/auth.service';
import { AppNotification, NotificationsApiService } from '../../core/notifications/notifications-api.service';

/**
 * Колокольчик уведомлений в топбаре (перенос gtport Navbar): бейдж непрочитанных
 * (тихий опрос раз в 60 с — конвенция автообновления главной), дропдаун со
 * списком по клику (список тянется при каждом открытии). Клик по уведомлению
 * отмечает его прочитанным и ведёт по deep-link'у (брошенные/«Без атрибуции» —
 * модалки главной через ?open=, справочник — Админ с выбранной таблицей,
 * прибытия — главная: «Прибывшие» там первые на экране).
 *
 * Кнопки отметки прячутся при !auth.canEdit(): PUT-ручки закрыты глобальным
 * RequireForWrites, клиентские роли видят пустой колокольчик (см. handler).
 */
@Component({
  selector: 'app-notification-bell',
  imports: [NzBadgeModule, NzButtonModule, NzDropDownModule, NzIconModule, NzTooltipModule],
  template: `
    <a nz-dropdown [nzDropdownMenu]="menu" nzTrigger="click" [nzOverlayStyle]="{ 'margin-top': '8px' }"
       class="bell" (nzVisibleChange)="onOpenChange($event)"
       nz-tooltip nzTooltipTitle="Уведомления">
      <nz-badge [nzCount]="unread()" [nzOverflowCount]="99" nzSize="small">
        <span nz-icon nzType="bell" class="bell-icon"></span>
      </nz-badge>
    </a>
    <nz-dropdown-menu #menu="nzDropdownMenu">
      <div class="panel">
        <div class="head">
          <span class="ttl">Уведомления</span>
          @if (canMark && items().length > 0 && unread() > 0) {
            <button nz-button nzType="link" nzSize="small" (click)="markAll($event)">Прочитать все</button>
          }
        </div>
        <div class="body">
          @if (loading()) {
            <p class="mut">Загрузка…</p>
          } @else if (items().length === 0) {
            <p class="mut">Уведомлений нет</p>
          } @else {
            @for (n of items(); track n.id) {
              <button type="button" class="item" [class.unread]="!n.is_read" (click)="open(n)">
                <span nz-icon [nzType]="icon(n.type)" class="t-icon" [class]="'t-' + n.type"></span>
                <span class="txt">
                  <span class="t">{{ n.title }}</span>
                  <span class="m">{{ n.message }}</span>
                  <span class="when">{{ when(n.created_at) }}</span>
                </span>
                @if (!n.is_read) { <span class="dot"></span> }
              </button>
            }
          }
        </div>
      </div>
    </nz-dropdown-menu>
  `,
  styles: [`
    .bell { display: inline-flex; align-items: center; color: var(--color-text); cursor: pointer;
            padding: 0 var(--space-xs); }
    .bell-icon { font-size: calc(var(--layout-icon-size) * 1.2); }
    .panel { width: 380px; max-width: 90vw; background: var(--color-bg-surface);
             border-radius: var(--radius-card); box-shadow: var(--shadow-lg); overflow: hidden; }
    .head { display: flex; align-items: center; justify-content: space-between;
            padding: var(--space-xs) var(--space-sm); border-bottom: 1px solid var(--color-border-light); }
    .ttl { font-weight: 600; }
    .body { max-height: 60vh; overflow-y: auto; }
    .mut { color: var(--color-text-secondary); text-align: center; padding: var(--space-md); margin: 0; }
    .item { display: flex; gap: var(--space-sm); align-items: flex-start; width: 100%;
            padding: var(--space-xs) var(--space-sm); border: 0; border-bottom: 1px solid var(--color-border-light);
            background: transparent; text-align: left; cursor: pointer; }
    .item:hover { background: var(--color-bg-hover, rgba(0,0,0,.04)); }
    .item.unread { background: var(--color-bg-selected, rgba(24,144,255,.06)); }
    .t-icon { margin-top: 3px; }
    .t-warning { color: var(--color-warning, #faad14); }
    .t-error { color: var(--color-danger, #ff4d4f); }
    .t-info { color: var(--color-primary); }
    .t-service { color: var(--color-text-secondary); }
    .txt { display: flex; flex-direction: column; gap: 2px; min-width: 0; flex: 1 1 auto; }
    .t { font-weight: 600; }
    .m { color: var(--color-text-secondary); font-size: var(--font-size-sm); white-space: normal; }
    .when { color: var(--color-text-muted); font-size: var(--font-size-sm); }
    .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--color-primary);
           flex: 0 0 auto; margin-top: 6px; }
  `],
})
export class NotificationBellComponent implements OnInit, OnDestroy {
  private readonly api = inject(NotificationsApiService);
  private readonly auth = inject(AuthService);
  private readonly router = inject(Router);

  readonly unread = signal(0);
  readonly items = signal<AppNotification[]>([]);
  readonly loading = signal(false);
  private timer: ReturnType<typeof setInterval> | undefined;

  /** Отметки прочтения — только ролям с правом записи (PUT под RequireForWrites). */
  get canMark(): boolean {
    return this.auth.canEdit();
  }

  ngOnInit(): void {
    void this.refreshCount();
    // 60 с — конвенция тихого обновления главной; ошибки молчат (бейдж не критичен).
    this.timer = setInterval(() => void this.refreshCount(), 60_000);
  }

  ngOnDestroy(): void {
    if (this.timer) clearInterval(this.timer);
  }

  private async refreshCount(): Promise<void> {
    try {
      this.unread.set(await this.api.unreadCount());
    } catch { /* тихо: сеть/бэк вернутся — бейдж обновится следующим тиком */ }
  }

  /** Открытие дропдауна тянет свежий список (как gtport). */
  onOpenChange(open: boolean): void {
    if (!open) return;
    this.loading.set(true);
    this.api.list(false, 50)
      .then((list) => this.items.set(list))
      .catch(() => this.items.set([]))
      .finally(() => this.loading.set(false));
  }

  /** Клик: отметить прочитанным и перейти по deep-link'у уведомления. */
  open(n: AppNotification): void {
    if (this.canMark && !n.is_read) {
      n.is_read = true;
      this.unread.set(Math.max(0, this.unread() - 1));
      void this.api.markRead(n.id).catch(() => { /* best-effort */ });
    }
    const target = this.route(n);
    if (target) void this.router.navigate(target.path, { queryParams: target.query });
  }

  markAll(ev: Event): void {
    ev.stopPropagation();
    void this.api.markAllRead()
      .then(() => {
        this.unread.set(0);
        this.items.update((list) => list.map((n) => ({ ...n, is_read: true })));
      })
      .catch(() => { /* best-effort */ });
  }

  /** Маршрут deep-link'а: модалки главной — через ?open= (читает HomeComponent). */
  private route(n: AppNotification): { path: string[]; query?: Record<string, string> } | null {
    switch (n.action_component) {
      case 'bros': return { path: ['/home'], query: { open: 'bros' } };
      case 'unmatched': return { path: ['/home'], query: { open: 'unmatched' } };
      case 'arrivals': return { path: ['/home'] }; // «Прибывшие» — первые на главной
      case 'admin_dict': {
        const table = typeof n.action_params?.['table'] === 'string'
          ? (n.action_params['table'] as string) : 'stations';
        return { path: ['/admin'], query: { table } };
      }
      default: return null;
    }
  }

  icon(t: string): string {
    switch (t) {
      case 'warning': return 'warning';
      case 'error': return 'close-circle';
      case 'service': return 'tool';
      default: return 'info-circle';
    }
  }

  /** «10.08 09:15» — короткий штамп (время МСК naive, как отдаёт бэк). */
  when(iso: string): string {
    if (!iso || iso.length < 16) return iso ?? '';
    return `${iso.slice(8, 10)}.${iso.slice(5, 7)} ${iso.slice(11, 16)}`;
  }
}
