import { Component, computed, inject, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { DragDropModule } from '@angular/cdk/drag-drop';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzDropDownModule, NzContextMenuService, NzDropdownMenuComponent } from 'ng-zorro-antd/dropdown';
import { VagonTrailModalComponent } from './vagon-trail-modal.component';

/**
 * Строка списка вагонов «сбоку от снимка» — общая форма для пропавших (статус 8)
 * и доноров перегруза (статус 6): последняя известная позиция + давность.
 * `id` обязателен: по нему открывается «История движения вагона».
 */
export interface VagonListRow {
  id: string;
  vagon: string;
  index: string;
  station_oper: string;
  doroga_oper: string;
  oper_s: string;
  time_op: string | null;
  naznach: string;
  cargo_s: string;
  ves: number | null;
  since: string;
  days: number;
}

/**
 * Перемещаемая модалка со списком вагонов: используется и для «Пропавших»
 * (статус 8), и для «Перегруза» (статус 6) — таблицы одинаковые, различаются
 * заголовком и подписью колонки давности.
 *
 * ПКМ по строке — «История движения вагона» (та же модалка, что в истории
 * прибывших): рейс адресуется id строки, поэтому работает и для вагона,
 * которого уже нет в снимке.
 */
@Component({
  selector: 'app-vagon-list-modal',
  imports: [
    FormsModule, DragDropModule, NzButtonModule, NzIconModule, NzInputModule,
    NzModalModule, NzTagModule, NzTooltipModule, NzDropDownModule, VagonTrailModalComponent,
  ],
  template: `
    <nz-modal [nzVisible]="true" [nzTitle]="ttl" [nzFooter]="null" nzWidth="1000px"
              [nzMask]="false" (nzOnCancel)="closed.emit()">
      <ng-template #ttl>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          {{ title() }} ({{ rows().length }})
        </div>
      </ng-template>
      <ng-container *nzModalContent>
        <div class="bar">
          <span class="mut">{{ hint() }}</span>
          <span class="spacer"></span>
          <input nz-input nzSize="small" class="search" placeholder="№ вагона"
                 [ngModel]="search()" (ngModelChange)="search.set($event)" />
          <button nz-button nzType="text" nzSize="small" nz-tooltip nzTooltipTitle="Обновить"
                  (click)="reload.emit()">
            <span nz-icon nzType="reload"></span>
          </button>
        </div>

        <div class="tbl-wrap">
          <table class="tbl">
            <thead>
              <tr>
                <th class="c-vag">Вагон</th>
                <th class="c-idx">Индекс</th>
                <th>Станция операции</th>
                <th class="c-op">Операция</th>
                <th class="c-dt">Время оп.</th>
                <th class="c-term">Терминал</th>
                <th class="c-cargo">Груз</th>
                <th class="c-ves">Вес</th>
                <th class="c-dt">{{ sinceLabel() }}</th>
                <th class="c-days">Дней</th>
              </tr>
            </thead>
            <tbody>
              @for (r of filtered(); track r.id) {
                <tr [class.stale]="r.days >= 3" (contextmenu)="openMenu($event, r, menu)">
                  <td class="num">{{ r.vagon }}</td>
                  <td class="num idx" [title]="r.index">{{ r.index || '—' }}</td>
                  <td class="ell" [title]="station(r)">{{ station(r) }}</td>
                  <td class="ell" [title]="r.oper_s">{{ r.oper_s || '—' }}</td>
                  <td class="c">{{ fmt(r.time_op) }}</td>
                  <td class="c">
                    @if (r.naznach) { <nz-tag class="chip">{{ r.naznach }}</nz-tag> } @else { — }
                  </td>
                  <td class="ell" [title]="r.cargo_s">{{ r.cargo_s || 'порожний' }}</td>
                  <td class="c">{{ r.ves ? r.ves.toFixed(1) : '—' }}</td>
                  <td class="c">{{ fmt(r.since) }}</td>
                  <td class="c days">{{ r.days }}</td>
                </tr>
              } @empty {
                <tr><td colspan="10" class="empty">Список пуст</td></tr>
              }
            </tbody>
          </table>
        </div>

        <p class="hint">ПКМ по строке — история движения вагона. Времена — московские.</p>
      </ng-container>
    </nz-modal>

    <nz-dropdown-menu #menu="nzDropdownMenu">
      <ul nz-menu>
        @if (ctxRow(); as r) {
          <li nz-menu-item (click)="trailRow.set(r)">История движения вагона {{ r.vagon }}…</li>
        }
      </ul>
    </nz-dropdown-menu>

    @if (trailRow(); as r) {
      <app-vagon-trail-modal [vagonId]="r.id" [vagon]="r.vagon" (closed)="trailRow.set(null)" />
    }
  `,
  styles: [`
    .ttl { cursor: move; user-select: none; }
    .bar { display: flex; align-items: center; gap: var(--space-sm); margin-bottom: var(--space-sm);
           font-size: var(--font-size-sm); }
    .spacer { flex: 1 1 auto; }
    .search { width: 140px; }
    .mut { color: var(--color-text-muted); }
    .tbl-wrap { max-height: 62vh; overflow: auto; }
    .tbl { width: 100%; border-collapse: collapse; font-size: var(--font-size-sm); table-layout: fixed; }
    .tbl th { position: sticky; top: 0; background: var(--color-bg-subtle); font-weight: 600;
              padding: 4px 8px; border: 1px solid var(--color-border-light); text-align: center; z-index: 1; }
    .tbl td { padding: 3px 8px; border: 1px solid var(--color-border-light); }
    .c-vag { width: 90px; } .c-idx { width: 120px; } .c-op { width: 130px; }
    .c-dt { width: 100px; } .c-term { width: 80px; } .c-cargo { width: 130px; }
    .c-ves { width: 60px; } .c-days { width: 56px; }
    .num { font-variant-numeric: tabular-nums; }
    .idx, .ell { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .c { text-align: center; font-variant-numeric: tabular-nums; }
    .days { font-weight: 600; }
    .chip { margin: 0; }
    /* Давно в списке (3+ суток) — жёлтая подсветка строки (как на экране «Пропавшие»). */
    tr.stale > td { background: var(--color-warning-bg, #fffbe6); }
    .empty { text-align: center; color: var(--color-text-secondary); padding: var(--space-md); }
    .hint { margin: var(--space-xs) 0 0; color: var(--color-text-muted); font-size: var(--font-size-sm); }
  `],
})
export class VagonListModalComponent {
  private readonly ctxMenu = inject(NzContextMenuService);

  readonly title = input.required<string>();
  readonly rows = input.required<VagonListRow[]>();
  /** Подпись колонки давности: «Пропал» / «Донор с». */
  readonly sinceLabel = input('Пропал');
  readonly hint = input('');
  readonly closed = output<void>();
  readonly reload = output<void>();

  readonly search = signal('');
  readonly ctxRow = signal<VagonListRow | null>(null);
  readonly trailRow = signal<VagonListRow | null>(null);

  readonly filtered = computed(() => {
    const q = this.search().trim();
    return q ? this.rows().filter((r) => r.vagon.includes(q)) : this.rows();
  });

  openMenu(ev: MouseEvent, r: VagonListRow, menu: NzDropdownMenuComponent): void {
    ev.preventDefault();
    this.ctxRow.set(r);
    this.ctxMenu.create(ev, menu);
  }

  /** «Станция (дорога)» из последней известной позиции. */
  station(r: VagonListRow): string {
    if (!r.station_oper) return '—';
    return r.doroga_oper ? `${r.station_oper} (${r.doroga_oper})` : r.station_oper;
  }

  /** «2026-07-15T08:49:00» → «15.07 08:49»; пусто → «—». */
  fmt(ts: string | null): string {
    if (!ts || ts.length < 16) return '—';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)} ${ts.slice(11, 16)}`;
  }
}
