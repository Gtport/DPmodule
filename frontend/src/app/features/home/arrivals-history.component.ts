import { Component, OnInit, computed, inject, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { DragDropModule } from '@angular/cdk/drag-drop';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { ArrivalGroup, ArrivalSubgroup, ArrivalsApiService, TerminalTarget } from './arrivals-api.service';

/**
 * Модалка «История прибывших — <станция>» (перенос gtport HistoryTable, только
 * просмотр): таблица Дата / Индекс / План / Факт / Откл + колонка на каждый
 * терминал станции (подгруппы по naznach, клик — разворот вагонов). Фильтры:
 * поиск вагона (локальный), период С/По, «Сегодня/Вчера», обновление, свернуть
 * всё. Окно перемещается по экрану за заголовок (cdkDrag).
 */
@Component({
  selector: 'app-arrivals-history',
  imports: [
    FormsModule, DragDropModule, NzButtonModule, NzIconModule, NzInputModule,
    NzModalModule, NzSpinModule, NzTooltipModule,
  ],
  template: `
    <nz-modal [nzVisible]="true" [nzTitle]="title" [nzFooter]="null" nzWidth="1250px"
              [nzMask]="false" nzWrapClassName="arrivals-wrap" (nzOnCancel)="closed.emit()">
      <ng-template #title>
        <!-- Перетаскивание всего окна за заголовок; маска отключена, чтобы окно
             можно было отодвинуть и смотреть страницу под ним. -->
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          История прибывших — {{ station() }}
        </div>
      </ng-template>
      <ng-container *nzModalContent>
        <div class="bar">
          <b>Прибывшие {{ terminalNames().join('+') }}</b>
          <span class="spacer"></span>
          <input nz-input nzSize="small" class="search" placeholder="Поиск вагона…"
                 [ngModel]="search()" (ngModelChange)="onSearch($event)" />
          <span class="lbl">С</span>
          <input class="date" type="date" [ngModel]="from()" (ngModelChange)="from.set($event); load()" />
          <span class="lbl">По</span>
          <input class="date" type="date" [ngModel]="to()" (ngModelChange)="to.set($event); load()" />
          <button nz-button nzSize="small" (click)="resetDates()">Сегодня/Вчера</button>
          <button nz-button nzType="text" nzSize="small" nz-tooltip nzTooltipTitle="Обновить" (click)="load()">
            <span nz-icon nzType="sync"></span>
          </button>
          <button nz-button nzType="text" nzSize="small" nz-tooltip nzTooltipTitle="Свернуть все вагоны"
                  (click)="collapseAll()">
            <span nz-icon nzType="eye-invisible"></span>
          </button>
        </div>

        <nz-spin [nzSpinning]="loading()">
          <div class="tbl-wrap">
            <table class="tbl">
              <thead>
                <tr>
                  <th class="c-date">Дата</th>
                  <th class="c-idx">Индекс</th>
                  <th class="c-plan">План</th>
                  <th class="c-fact">Факт</th>
                  <th class="c-otkl">Откл</th>
                  @for (t of terminals(); track t.name) { <th>{{ t.name }}</th> }
                </tr>
              </thead>
              <tbody>
                @for (g of filteredGroups(); track g.key) {
                  <tr>
                    <td class="c-date">{{ fmtD(g.date_prib_d) }}</td>
                    <td class="c-idx num">{{ g.index_pp || '—' }}</td>
                    <td class="c-plan">{{ fmtDT(g.plan_jd) }}</td>
                    <td class="c-fact">{{ fmtT(g.date_prib) }}</td>
                    <td class="c-otkl num" [class.late]="g.otkl.startsWith('+')"
                        [class.early]="g.otkl.startsWith('-')">{{ g.otkl || '—' }}</td>
                    @for (t of terminals(); track t.name) {
                      <td class="c-term">
                        @for (sg of subsFor(g, t.name); track sg.key) {
                          <div class="sg" (click)="toggle(g.key, sg.key)">
                            <span nz-icon [nzType]="isOpen(g.key, sg.key) ? 'down' : 'right'" class="tw"></span>
                            <span [class.hit]="isHit(sg)">{{ sg.display }}</span>
                          </div>
                          @if (isOpen(g.key, sg.key)) {
                            <div class="vagons">
                              @for (v of sg.vagons; track v.id) {
                                <span class="chip" [class.hit]="matches(v.vagon)">
                                  {{ v.vagon }}@if (v.shipments) { ({{ v.shipments }}) }
                                </span>
                              }
                            </div>
                          }
                        } @empty { <span class="mut">—</span> }
                      </td>
                    }
                  </tr>
                } @empty {
                  <tr><td [attr.colspan]="5 + terminals().length" class="empty">Нет прибывших за период</td></tr>
                }
              </tbody>
            </table>
          </div>
        </nz-spin>
      </ng-container>
    </nz-modal>
  `,
  styles: [`
    .ttl { cursor: move; user-select: none; }
    .bar { display: flex; align-items: center; gap: var(--space-sm); flex-wrap: wrap; margin-bottom: var(--space-sm); }
    .spacer { flex: 1 1 auto; }
    .lbl { color: var(--color-text-secondary); font-size: var(--font-size-sm); }
    .search { width: 160px; }
    .date { height: 26px; padding: 0 6px; border: 1px solid var(--color-border, #d9d9d9);
            border-radius: var(--radius-sm); font-size: var(--font-size-sm); color: inherit; background: transparent; }
    .tbl-wrap { max-height: 62vh; overflow: auto; }
    .tbl { width: 100%; border-collapse: collapse; font-size: var(--font-size-sm); }
    .tbl th { position: sticky; top: 0; background: var(--color-bg-subtle); font-weight: 600;
              padding: 4px 8px; border: 1px solid var(--color-border-light); text-align: center; z-index: 1; }
    .tbl td { padding: 3px 8px; border: 1px solid var(--color-border-light); vertical-align: top; }
    .c-date, .c-fact, .c-otkl { text-align: center; white-space: nowrap; }
    .c-plan { white-space: nowrap; text-align: center; }
    .num { font-variant-numeric: tabular-nums; }
    .c-otkl.late { color: var(--color-danger); }
    .c-otkl.early { color: var(--color-success); }
    .sg { display: flex; align-items: center; gap: 4px; cursor: pointer; border-radius: var(--radius-sm);
          padding: 1px 4px; white-space: nowrap; }
    .sg:hover { background: var(--color-bg-hover); }
    .tw { font-size: 10px; color: var(--color-text-muted); }
    .vagons { display: flex; flex-wrap: wrap; gap: 3px; padding: 2px 0 4px 18px; }
    .chip { font-variant-numeric: tabular-nums; border: 1px solid var(--color-border-dark);
            border-radius: var(--radius-sm); padding: 0 4px; }
    .hit { background: var(--color-warning); border-radius: var(--radius-sm); }
    .mut { color: var(--color-text-muted); }
    .empty { text-align: center; color: var(--color-text-secondary); padding: var(--space-md); }
  `],
})
export class ArrivalsHistoryComponent implements OnInit {
  private readonly api = inject(ArrivalsApiService);
  private readonly msg = inject(NzMessageService);

  /** Станция (заголовок окна) и её терминалы (колонки/фильтр naznach). */
  readonly station = input.required<string>();
  readonly terminals = input.required<TerminalTarget[]>();
  readonly closed = output<void>();

  readonly loading = signal(false);
  readonly groups = signal<ArrivalGroup[]>([]);
  readonly from = signal('');
  readonly to = signal('');
  readonly search = signal('');
  /** Развёрнутые подгруппы: ключ `group.key::sub.key`. */
  readonly open = signal<Set<string>>(new Set());

  readonly terminalNames = computed(() => this.terminals().map((t) => t.name));

  ngOnInit(): void {
    void this.load();
  }

  async load(): Promise<void> {
    this.loading.set(true);
    try {
      const res = await this.api.getArrivals(this.terminalNames(), this.from(), this.to());
      this.groups.set(res.groups);
      this.from.set(res.from);
      this.to.set(res.to);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.loading.set(false);
    }
  }

  resetDates(): void {
    this.from.set('');
    this.to.set('');
    void this.load();
  }

  /** Поиск: при вводе разворачиваем всё (эталон gtport), совпадения подсвечены. */
  onSearch(q: string): void {
    this.search.set(q);
    if (q.trim()) {
      const all = new Set<string>();
      for (const g of this.groups()) for (const sg of g.sub_groups) all.add(g.key + '::' + sg.key);
      this.open.set(all);
    }
  }

  readonly filteredGroups = computed(() => {
    const q = this.search().trim().toUpperCase();
    if (!q) return this.groups();
    return this.groups().filter((g) =>
      g.index_pp.toUpperCase().includes(q) ||
      g.sub_groups.some((sg) => sg.display.toUpperCase().includes(q) ||
        sg.vagons.some((v) => v.vagon.toUpperCase().includes(q))),
    );
  });

  subsFor(g: ArrivalGroup, naznach: string): ArrivalSubgroup[] {
    return g.sub_groups.filter((sg) => sg.naznach === naznach);
  }

  matches(text: string): boolean {
    const q = this.search().trim().toUpperCase();
    return !!q && text.toUpperCase().includes(q);
  }

  isHit(sg: ArrivalSubgroup): boolean {
    const q = this.search().trim().toUpperCase();
    return !!q && (sg.display.toUpperCase().includes(q) || sg.vagons.some((v) => v.vagon.toUpperCase().includes(q)));
  }

  toggle(gk: string, sk: string): void {
    const next = new Set(this.open());
    const key = gk + '::' + sk;
    next.has(key) ? next.delete(key) : next.add(key);
    this.open.set(next);
  }

  isOpen(gk: string, sk: string): boolean {
    return this.open().has(gk + '::' + sk);
  }

  collapseAll(): void {
    this.open.set(new Set());
    this.search.set('');
  }

  // ── Форматы времени (МСК naive, отдаём как пришло — без сдвигов) ──────────
  /** дд.мм.гг */
  fmtD(ts: string | null): string {
    if (!ts || ts.length < 10) return '—';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)}.${ts.slice(2, 4)}`;
  }
  /** дд.мм.гг чч:мм */
  fmtDT(ts: string | null): string {
    if (!ts || ts.length < 16) return '—';
    return `${this.fmtD(ts)} ${ts.slice(11, 16)}`;
  }
  /** чч:мм */
  fmtT(ts: string | null): string {
    if (!ts || ts.length < 16) return '—';
    return ts.slice(11, 16);
  }
}
