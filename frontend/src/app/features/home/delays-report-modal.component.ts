import { Component, OnInit, inject, output, signal } from '@angular/core';
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
import { addDaysIso, todayMsk } from '../../shared/msk-date';
import { ArrivalsApiService } from './arrivals-api.service';
import { DelayEpisode, DelayReport, DelaysApiService } from './delays-api.service';
import { VagonTrailModalComponent } from './vagon-trail-modal.component';

/**
 * Перемещаемая модалка «Отчёт по простоям» — исторический разрез памяти о
 * задержках (vagon_delay, статусы 4/5) за период, по аналогии с отчётом
 * «Брошенных»: выбор терминала и периода. Главная метрика — вагоно-часы простоя
 * ЗА ПЕРИОД (длительность эпизода, обрезанная границами периода; открытый —
 * до «сейчас»). Сверху итоги и агрегат по станциям (тяжёлые сверху), ниже —
 * сами эпизоды; клик по вагону открывает «Историю движения вагона». Метрики
 * «стоят сейчас» здесь нет — оперативный список живёт в «Задержанных вагонах».
 * Аналога в gtport не было — подсистема новая.
 */
@Component({
  selector: 'app-delays-report-modal',
  imports: [
    FormsModule, DragDropModule, NzButtonModule, NzIconModule, NzModalModule,
    NzSelectModule, NzSpinModule, NzTooltipModule, VagonTrailModalComponent,
  ],
  template: `
    <nz-modal [nzVisible]="true" [nzTitle]="ttl" [nzFooter]="null" nzWidth="1120px"
              [nzMask]="false" (nzOnCancel)="closed.emit()">
      <ng-template #ttl>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          Отчёт по простоям — {{ terminal() || 'все терминалы' }}
          &nbsp;<span class="sub">{{ fmtDate(start()) }} – {{ fmtDate(end()) }}</span>
        </div>
      </ng-template>

      <ng-container *nzModalContent>
        <div class="filters">
          <nz-select class="term" nzSize="small" [ngModel]="terminal()" (ngModelChange)="terminal.set($event)">
            <nz-option nzValue="" nzLabel="Все терминалы"></nz-option>
            @for (t of terminals(); track t) { <nz-option [nzValue]="t" [nzLabel]="t"></nz-option> }
          </nz-select>
          <label class="fl">Начало <input type="date" class="date" [ngModel]="start()" (ngModelChange)="start.set($event)" /></label>
          <label class="fl">Конец <input type="date" class="date" [ngModel]="end()" (ngModelChange)="end.set($event)" /></label>
          <button nz-button nzType="primary" nzSize="small" [nzLoading]="loading()" (click)="load()">
            <span nz-icon nzType="search"></span> Загрузить
          </button>
          <button nz-button nzSize="small" (click)="lastDays(7)">7 дней</button>
          <button nz-button nzSize="small" (click)="lastDays(30)">30 дней</button>
          <span class="spacer"></span>
          <button nz-button nzType="text" nzSize="small" (click)="exportExcel()" [disabled]="!report()?.records?.length"
                  nz-tooltip nzTooltipTitle="Экспорт в Excel">
            <span nz-icon nzType="download"></span>
          </button>
        </div>

        @if (loading()) {
          <div class="center"><nz-spin nzSimple></nz-spin></div>
        } @else if (report(); as r) {
          <div class="tiles">
            <div class="tile"><b>{{ fmtDur(r.total_hours) }}</b><span>Вагоно-простой за период</span></div>
            <div class="tile"><b>{{ r.total_episodes }}</b><span>Эпизодов</span></div>
            <div class="tile"><b>{{ r.total_vagons }}</b><span>Вагонов</span></div>
          </div>

          <div class="box-h">По станциям (вагоно-часы за период, тяжёлые сверху)</div>
          <div class="tbl-wrap" style="max-height: 30vh">
            <table class="tbl">
              <thead><tr>
                <th>Станция</th><th class="c-dor">Дорога</th><th class="c-n">Эпизодов</th>
                <th class="c-n">Вагонов</th>
                <th class="c-h">Простой</th><th class="c-h">Брошены</th><th class="c-h">Всего</th>
              </tr></thead>
              <tbody>
                @for (s of r.stations; track s.station_code + s.station_name) {
                  <tr>
                    <td class="ell" [title]="s.station_name">{{ s.station_name || ('код ' + s.station_code) }}</td>
                    <td class="c">{{ s.doroga || '—' }}</td>
                    <td class="c num">{{ s.episodes }}</td>
                    <td class="c num">{{ s.vagons }}</td>
                    <td class="c num">{{ s.hours4 ? fmtDur(s.hours4) : '-' }}</td>
                    <td class="c num d5">{{ s.hours5 ? fmtDur(s.hours5) : '-' }}</td>
                    <td class="c num"><b>{{ fmtDur(s.hours) }}</b></td>
                  </tr>
                } @empty {
                  <tr><td colspan="7" class="empty">Задержек за период нет</td></tr>
                }
              </tbody>
            </table>
          </div>

          <div class="box-h">Эпизоды (свежие сверху)</div>
          <div class="tbl-wrap" style="max-height: 34vh">
            <table class="tbl">
              <thead><tr>
                <th class="c-vag">Вагон</th><th class="c-idx">Индекс</th><th class="c-term">Терминал</th>
                <th>Станция</th><th class="c-dor">Дорога</th><th class="c-kind">Вид</th>
                <th class="c-dt">Стоит с</th><th class="c-dt">По</th>
                <th class="c-h">Длит.</th><th class="c-h">В периоде</th>
              </tr></thead>
              <tbody>
                @for (e of r.records; track e.id) {
                  <tr>
                    <td class="c num">
                      @if (e.trip_id) {
                        <a class="idx" (click)="openTrail(e)" nz-tooltip nzTooltipTitle="История движения вагона">{{ e.vagon }}</a>
                      } @else { {{ e.vagon }} }
                    </td>
                    <td class="c num">{{ e.index || '—' }}</td>
                    <td class="c">{{ e.gruzpol_s || '—' }}</td>
                    <td class="ell" [title]="e.station_name">{{ e.station_name || ('код ' + e.station_code) }}</td>
                    <td class="c">{{ e.doroga || '—' }}</td>
                    <td class="c"><span class="dtag" [class.dtag5]="e.kind === 5">{{ e.kind === 5 ? 'брошен' : 'простой' }}</span></td>
                    <td class="c">{{ fmtDT(e.date_from) }}</td>
                    <td class="c">{{ e.date_to ? fmtDT(e.date_to) : 'стоит' }}</td>
                    <td class="c num">{{ fmtDur(fullHours(e)) }}</td>
                    <td class="c num">{{ fmtDur(e.hours_in_period) }}</td>
                  </tr>
                } @empty {
                  <tr><td colspan="10" class="empty">Задержек за период нет</td></tr>
                }
              </tbody>
            </table>
          </div>
          <p class="hint">«В периоде» — часть простоя внутри выбранных дат; открытые эпизоды считаются до текущего момента. Клик по вагону — история движения.</p>
        }
      </ng-container>
    </nz-modal>

    @if (trailFor(); as tf) {
      <app-vagon-trail-modal [vagonId]="tf.trip_id" [vagon]="tf.vagon" (closed)="trailFor.set(null)" />
    }
  `,
  styles: [`
    .ttl { cursor: move; user-select: none; }
    .ttl .sub { color: var(--color-text-muted); font-weight: 400; font-size: var(--font-size-sm); }
    .filters { display: flex; align-items: center; gap: var(--space-sm); margin-bottom: var(--space-sm); flex-wrap: wrap; }
    .term { width: 150px; }
    .c-term { width: 86px; }
    .fl { font-size: var(--font-size-sm); color: var(--color-text-secondary); display: inline-flex; align-items: center; gap: 4px; }
    .date { padding: 3px 6px; border: 1px solid var(--color-border); border-radius: var(--radius-sm); }
    .spacer { flex: 1 1 auto; }
    .center { display: flex; justify-content: center; padding: var(--space-lg); }
    .tiles { display: flex; gap: var(--space-sm); flex-wrap: wrap; margin-bottom: var(--space-md); }
    .tile { flex: 1 1 0; min-width: 110px; text-align: center; padding: 6px 8px;
            border: 1px solid var(--color-border-light); border-radius: var(--radius-sm); background: var(--color-bg-surface); }
    .tile b { display: block; font-size: 1.25rem; font-variant-numeric: tabular-nums; line-height: 1.1; }
    .tile span { font-size: var(--font-size-sm); color: var(--color-text-muted); }
    .box-h { font-weight: 600; font-size: var(--font-size-sm); margin: 0 0 var(--space-xs); }
    .tbl-wrap { overflow: auto; margin-bottom: var(--space-sm); }
    .tbl { width: 100%; border-collapse: collapse; font-size: var(--font-size-sm); }
    .tbl th { position: sticky; top: 0; background: var(--color-bg-subtle); font-weight: 600;
              padding: 4px 8px; border: 1px solid var(--color-border-light); text-align: center; z-index: 1; white-space: nowrap; }
    .tbl td { padding: 3px 8px; border: 1px solid var(--color-border-light); }
    .c { text-align: center; white-space: nowrap; }
    .num { font-variant-numeric: tabular-nums; }
    .ell { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 260px; }
    .c-dor { width: 62px; } .c-n { width: 78px; } .c-h { width: 84px; }
    .c-vag { width: 92px; } .c-idx { width: 116px; } .c-kind { width: 84px; } .c-dt { width: 112px; }
    .warn { color: var(--color-danger); }
    .d5 { color: var(--color-danger); }
    .idx { color: #0958d9; text-decoration: underline; cursor: pointer; }
    .dtag { display: inline-block; padding: 0 6px; border-radius: 10px; font-size: 11px;
            background: var(--color-warning-bg); border: 1px solid var(--color-warning); }
    .dtag5 { color: var(--color-danger); border-color: var(--color-danger); }
    /* Заливки строк нет (решение владельца) — открытый эпизод виден по «стоит» в колонке «По». */
    .empty { text-align: center; color: var(--color-text-secondary); padding: var(--space-md); }
    .hint { margin: var(--space-xs) 0 0; color: var(--color-text-muted); font-size: var(--font-size-sm); }
  `],
})
export class DelaysReportModalComponent implements OnInit {
  private readonly api = inject(DelaysApiService);
  private readonly arrivals = inject(ArrivalsApiService);
  private readonly msg = inject(NzMessageService);

  readonly closed = output<void>();

  readonly start = signal(addDaysIso(todayMsk(), -6));
  readonly end = signal(todayMsk());
  readonly terminal = signal('');
  readonly terminals = signal<string[]>([]);
  readonly loading = signal(false);
  readonly report = signal<DelayReport | null>(null);
  readonly trailFor = signal<{ trip_id: string; vagon: string } | null>(null);

  ngOnInit(): void {
    void this.loadTerminals();
    void this.load();
  }

  private async loadTerminals(): Promise<void> {
    try {
      this.terminals.set((await this.arrivals.getTerminals()).map((t) => t.name));
    } catch {
      /* справочник не критичен */
    }
  }

  async load(): Promise<void> {
    this.loading.set(true);
    try {
      this.report.set(await this.api.report(this.start(), this.end(), this.terminal()));
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
      this.report.set(null);
    } finally {
      this.loading.set(false);
    }
  }

  lastDays(n: number): void {
    this.start.set(addDaysIso(todayMsk(), -(n - 1)));
    this.end.set(todayMsk());
    void this.load();
  }

  openTrail(e: DelayEpisode): void {
    this.trailFor.set({ trip_id: e.trip_id, vagon: e.vagon });
  }

  /** Полная длительность эпизода: закрытый — hours из базы, открытый — «в периоде». */
  fullHours(e: DelayEpisode): number {
    return e.hours ?? e.hours_in_period;
  }

  /** Часы → человеческая длительность: до суток — часы, дальше — сутки с десятыми. */
  fmtDur(hours: number): string {
    if (!hours || hours <= 0) return '—';
    if (hours < 24) return `${Math.round(hours * 10) / 10} ч`;
    return `${Math.round((hours / 24) * 10) / 10} сут`;
  }

  /** «2026-07-24…» → «24.07.2026»; пусто → «—». */
  fmtDate(ts: string | null): string {
    if (!ts || ts.length < 10) return '—';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)}.${ts.slice(0, 4)}`;
  }

  /** «2026-07-24T08:05:00» → «24.07 08:05»; пусто → «—». */
  fmtDT(ts: string | null): string {
    if (!ts || ts.length < 16) return '—';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)} ${ts.slice(11, 16)}`;
  }

  /** Экспорт: два листа — агрегат по станциям и все эпизоды периода. */
  async exportExcel(): Promise<void> {
    const r = this.report();
    if (!r?.records?.length) return;
    try {
      const XLSX = await import('xlsx-js-style');
      const wb = XLSX.utils.book_new();
      const durD = (h: number) => (h ? Math.round((h / 24) * 100) / 100 : 0); // сутки, сотые

      const sh1 = [
        ['Станция', 'Дорога', 'Эпизодов', 'Вагонов', 'Простой, сут', 'Брошены, сут', 'Всего, сут'],
        ...r.stations.map((s) => [
          s.station_name || s.station_code, s.doroga, s.episodes, s.vagons,
          durD(s.hours4), durD(s.hours5), durD(s.hours),
        ]),
        ['ИТОГО', '', r.total_episodes, r.total_vagons, '', '', durD(r.total_hours)],
      ];
      const ws1 = XLSX.utils.aoa_to_sheet(sh1);
      ws1['!cols'] = [26, 10, 10, 10, 12, 12, 12].map((wch) => ({ wch }));
      XLSX.utils.book_append_sheet(wb, ws1, 'По станциям');

      const sh2 = [
        ['Вагон', 'Индекс', 'Родит. индекс', 'Терминал', 'Станция', 'Дорога', 'Вид', 'Стоит с', 'По', 'Длит., сут', 'В периоде, сут'],
        ...r.records.map((e) => [
          e.vagon, e.index, e.index_main, e.gruzpol_s, e.station_name || e.station_code, e.doroga,
          e.kind === 5 ? 'брошен' : 'простой',
          this.fmtDT(e.date_from), e.date_to ? this.fmtDT(e.date_to) : 'стоит',
          durD(this.fullHours(e)), durD(e.hours_in_period),
        ]),
      ];
      const ws2 = XLSX.utils.aoa_to_sheet(sh2);
      ws2['!cols'] = [11, 15, 15, 10, 26, 10, 10, 14, 14, 11, 13].map((wch) => ({ wch }));
      XLSX.utils.book_append_sheet(wb, ws2, 'Эпизоды');

      const label = this.terminal() || 'все_терминалы';
      XLSX.writeFile(wb, `простои_${label}_${this.fmtDate(this.start())}-${this.fmtDate(this.end())}.xlsx`);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    }
  }
}
