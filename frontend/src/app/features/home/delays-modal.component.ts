import { Component, OnInit, computed, inject, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { DragDropModule } from '@angular/cdk/drag-drop';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzSelectModule } from 'ng-zorro-antd/select';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { DelayEpisode, DelaysApiService } from './delays-api.service';
import { DelaysReportModalComponent } from './delays-report-modal.component';
import { VagonTrailModalComponent } from './vagon-trail-modal.component';

/** Поезд-группа задержанных вагонов (агрегация открытых эпизодов по индексу). */
interface DelayGroup {
  key: string;
  index: string;
  terminal: string;
  station: string;
  doroga: string;
  kind: number; // 5, если в группе есть брошенные (бросают весь поезд)
  vagons: DelayEpisode[];
  minFrom: string | null;
  maxHours: number;
}

/**
 * Перемещаемая модалка «Задержанные вагоны» — оперативный список: кто стоит
 * ПРЯМО СЕЙЧАС (открытые эпизоды vagon_delay, статусы 4/5). Агрегация по
 * индексу поезда, разворот до вагонов; терминал обязателен (решение владельца);
 * фильтр по виду (все / простой / брошенные). Клик по вагону — «История
 * движения вагона». Исторический разрез — кнопкой «Отчёт» (период + терминал).
 */
@Component({
  selector: 'app-delays-modal',
  imports: [
    FormsModule, DragDropModule, NzButtonModule, NzIconModule, NzModalModule,
    NzSelectModule, NzSpinModule, NzTooltipModule,
    DelaysReportModalComponent, VagonTrailModalComponent,
  ],
  template: `
    <nz-modal [nzVisible]="true" [nzTitle]="ttl" [nzFooter]="null" nzWidth="1060px"
              [nzMask]="false" (nzOnCancel)="closed.emit()">
      <ng-template #ttl>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          Задержанные вагоны
          @if (episodes().length) { <span class="sub">· {{ episodes().length }} ваг. / {{ groups().length }} поездов</span> }
        </div>
      </ng-template>

      <ng-container *nzModalContent>
        <div class="bar">
          <nz-select class="kind" nzSize="small" [ngModel]="kindFilter()" (ngModelChange)="kindFilter.set($event)">
            <nz-option nzValue="" nzLabel="Все задержки"></nz-option>
            <nz-option nzValue="4" nzLabel="Долгий простой"></nz-option>
            <nz-option nzValue="5" nzLabel="Брошенные"></nz-option>
          </nz-select>
          <nz-select class="term" nzSize="small" [ngModel]="terminalFilter()" (ngModelChange)="terminalFilter.set($event)">
            <nz-option nzValue="" nzLabel="Все терминалы"></nz-option>
            @for (t of terminals(); track t) { <nz-option [nzValue]="t" [nzLabel]="t"></nz-option> }
          </nz-select>
          <span class="spacer"></span>
          <button nz-button nzSize="small" (click)="showReport.set(true)"
                  nz-tooltip nzTooltipTitle="Отчёт по простоям за период (терминал, даты)">
            <span nz-icon nzType="bar-chart"></span> Отчёт
          </button>
          <button nz-button nzType="text" nzSize="small" (click)="load()" [nzLoading]="loading()"
                  nz-tooltip nzTooltipTitle="Обновить">
            <span nz-icon nzType="reload"></span>
          </button>
        </div>

        <nz-spin [nzSpinning]="loading()">
          <div class="tbl-wrap">
            <table class="tbl">
              <thead><tr>
                <th class="c-idx">Индекс</th><th class="c-n">Вагонов</th><th class="c-term">Терминал</th>
                <th>Станция</th><th class="c-dor">Дорога</th><th class="c-kind">Вид</th>
                <th class="c-dt">Стоит с</th><th class="c-h">Длит.</th>
              </tr></thead>
              <tbody>
                @for (g of groups(); track g.key; let i = $index) {
                  <tr class="grp" [class.g5]="g.kind === 5" (click)="toggle(g.key)">
                    <td class="c-idx num">
                      <span nz-icon [nzType]="isOpen(g.key) ? 'down' : 'right'" class="tw"></span>
                      {{ g.index || '—' }}
                    </td>
                    <td class="c num">{{ g.vagons.length }}</td>
                    <td class="c">{{ g.terminal || '—' }}</td>
                    <td class="ell" [title]="g.station">{{ g.station }}</td>
                    <td class="c">{{ g.doroga || '—' }}</td>
                    <td class="c"><span class="dtag" [class.dtag5]="g.kind === 5">{{ g.kind === 5 ? 'брошен' : 'простой' }}</span></td>
                    <td class="c">{{ fmtDT(g.minFrom) }}</td>
                    <td class="c num"><b>{{ fmtDur(g.maxHours) }}</b></td>
                  </tr>
                  @if (isOpen(g.key)) {
                    @for (v of g.vagons; track v.id) {
                      <tr class="vag">
                        <td class="c-idx"></td>
                        <td class="c num" colspan="2">
                          @if (v.trip_id) {
                            <a class="lnk" (click)="openTrail(v)" nz-tooltip nzTooltipTitle="История движения вагона">{{ v.vagon }}</a>
                          } @else { {{ v.vagon }} }
                        </td>
                        <td class="ell" colspan="2">{{ v.station_name || ('код ' + v.station_code) }}</td>
                        <td class="c"><span class="dtag" [class.dtag5]="v.kind === 5">{{ v.kind === 5 ? 'брошен' : 'простой' }}</span></td>
                        <td class="c">{{ fmtDT(v.date_from) }}</td>
                        <td class="c num">{{ fmtDur(v.hours_in_period) }}</td>
                      </tr>
                    }
                  }
                } @empty {
                  <tr><td colspan="8" class="empty">
                    @if (loading()) { Загрузка… } @else { Задержанных вагонов нет }
                  </td></tr>
                }
              </tbody>
            </table>
          </div>
        </nz-spin>
        <p class="hint">Строка = поезд (открытые эпизоды по индексу), клик — вагоны. «Стоит с» — начало стоянки самого раннего вагона; брошенные красным.</p>
      </ng-container>
    </nz-modal>

    @if (showReport()) {
      <app-delays-report-modal (closed)="showReport.set(false)" />
    }
    @if (trailFor(); as tf) {
      <app-vagon-trail-modal [vagonId]="tf.trip_id" [vagon]="tf.vagon" (closed)="trailFor.set(null)" />
    }
  `,
  styles: [`
    .ttl { cursor: move; user-select: none; }
    .ttl .sub { color: var(--color-text-muted); font-weight: 400; font-size: var(--font-size-sm); }
    .bar { display: flex; align-items: center; gap: var(--space-sm); margin-bottom: var(--space-sm); }
    .kind { width: 170px; }
    .spacer { flex: 1 1 auto; }
    .tbl-wrap { max-height: 62vh; overflow: auto; }
    .tbl { width: 100%; border-collapse: collapse; font-size: var(--font-size-sm); }
    .tbl th { position: sticky; top: 0; background: var(--color-bg-subtle); font-weight: 600;
              padding: 4px 8px; border: 1px solid var(--color-border-light); text-align: center; z-index: 1; white-space: nowrap; }
    .tbl td { padding: 3px 8px; border: 1px solid var(--color-border-light); }
    .grp { cursor: pointer; }
    .grp:hover td { background: var(--color-bg-hover); }
    /* Заливки строк нет (решение владельца): брошенные — красным индексом и бейджем. */
    .grp.g5 .c-idx { color: var(--color-danger); font-weight: 600; }
    .vag td { background: var(--color-bg-subtle); }
    .term { width: 150px; }
    .tw { font-size: 10px; color: var(--color-text-muted); margin-right: 4px; }
    .c { text-align: center; white-space: nowrap; }
    .num { font-variant-numeric: tabular-nums; }
    .ell { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 240px; }
    .c-idx { width: 130px; } .c-n { width: 70px; } .c-term { width: 86px; }
    .c-dor { width: 62px; } .c-kind { width: 86px; } .c-dt { width: 108px; } .c-h { width: 84px; }
    .dtag { display: inline-block; padding: 0 6px; border-radius: 10px; font-size: 11px;
            background: var(--color-warning-bg); border: 1px solid var(--color-warning); }
    .dtag5 { color: var(--color-danger); border-color: var(--color-danger); }
    .lnk { color: #0958d9; text-decoration: underline; cursor: pointer; }
    .empty { text-align: center; color: var(--color-text-secondary); padding: var(--space-md); }
    .hint { margin: var(--space-xs) 0 0; color: var(--color-text-muted); font-size: var(--font-size-sm); }
  `],
})
export class DelaysModalComponent implements OnInit {
  private readonly api = inject(DelaysApiService);
  private readonly msg = inject(NzMessageService);

  readonly closed = output<void>();

  readonly loading = signal(false);
  readonly episodes = signal<DelayEpisode[]>([]);
  readonly kindFilter = signal('');
  readonly terminalFilter = signal('');

  /** Терминалы из самих эпизодов (без похода за справочником). */
  readonly terminals = computed(() =>
    [...new Set(this.episodes().map((e) => e.gruzpol_s).filter(Boolean))].sort());
  readonly open = signal<Set<string>>(new Set());
  readonly showReport = signal(false);
  readonly trailFor = signal<{ trip_id: string; vagon: string } | null>(null);

  /** Группы-поезда: индекс + станция; вид группы — 5, если есть брошенные. */
  readonly groups = computed<DelayGroup[]>(() => {
    const kf = this.kindFilter(), tf = this.terminalFilter();
    const eps = this.episodes().filter((e) =>
      (!kf || String(e.kind) === kf) && (!tf || e.gruzpol_s === tf));
    const map = new Map<string, DelayGroup>();
    for (const e of eps) {
      const key = `${e.index}|${e.station_code}`;
      let g = map.get(key);
      if (!g) {
        g = {
          key, index: e.index, terminal: e.gruzpol_s,
          station: e.station_name || ('код ' + e.station_code), doroga: e.doroga,
          kind: e.kind, vagons: [], minFrom: e.date_from, maxHours: 0,
        };
        map.set(key, g);
      }
      g.vagons.push(e);
      if (e.kind === 5) g.kind = 5;
      if (!g.terminal && e.gruzpol_s) g.terminal = e.gruzpol_s;
      if (e.date_from && (!g.minFrom || e.date_from < g.minFrom)) g.minFrom = e.date_from;
      if (e.hours_in_period > g.maxHours) g.maxHours = e.hours_in_period;
    }
    return [...map.values()].sort((a, b) => b.maxHours - a.maxHours); // дольше всех — сверху
  });

  ngOnInit(): void {
    void this.load();
  }

  async load(): Promise<void> {
    this.loading.set(true);
    try {
      this.episodes.set(await this.api.current());
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.loading.set(false);
    }
  }

  toggle(key: string): void {
    const next = new Set(this.open());
    next.has(key) ? next.delete(key) : next.add(key);
    this.open.set(next);
  }

  isOpen(key: string): boolean {
    return this.open().has(key);
  }

  openTrail(e: DelayEpisode): void {
    this.trailFor.set({ trip_id: e.trip_id, vagon: e.vagon });
  }

  fmtDur(hours: number): string {
    if (!hours || hours <= 0) return '—';
    if (hours < 24) return `${Math.round(hours * 10) / 10} ч`;
    return `${Math.round((hours / 24) * 10) / 10} сут`;
  }

  /** «2026-07-24T08:05:00» → «24.07 08:05»; пусто → «—». */
  fmtDT(ts: string | null): string {
    if (!ts || ts.length < 16) return '—';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)} ${ts.slice(11, 16)}`;
  }
}
