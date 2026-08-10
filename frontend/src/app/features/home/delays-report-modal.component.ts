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
import { addDaysIso, todayMsk } from '../../shared/msk-date';
import { ArrivalsApiService } from './arrivals-api.service';
import { DelayEpisode, DelaysApiService } from './delays-api.service';
import { VagonTrailModalComponent } from './vagon-trail-modal.component';

/** Поезд-группа эпизодов периода — та же агрегация, что в «Задержанных вагонах». */
interface ReportGroup {
  key: string;
  index: string;
  terminal: string;
  station: string;
  doroga: string;
  kind: number; // 5, если в группе есть брошенные (бросают весь поезд)
  vagons: DelayEpisode[];
  minFrom: string | null;
  /** Самое позднее окончание; показывается, только если открытых эпизодов в группе нет. */
  maxTo: string | null;
  openNow: boolean;
  maxDur: number;
  maxHours: number;
}

/**
 * Перемещаемая модалка «Отчёт по простоям» — исторический разрез памяти о
 * задержках (vagon_delay, статусы 4/5) за период, по аналогии с отчётом
 * «Брошенных»: выбор терминала, периода и вида (все / простой / брошенные).
 * Эпизоды агрегированы по поездам, как в модалке «Задержанные вагоны»
 * (решение владельца 10.08.2026): строка = поезд (индекс + станция), разворот
 * до вагонов, клик по вагону открывает «Историю движения вагона». «В периоде»
 * — длительность, обрезанная границами периода (открытый эпизод — до
 * «сейчас»); экспорт в Excel — плоские эпизоды. Сводки по станциям на экране
 * нет (решение владельца 10.08.2026 — «пока убрать»), хотя сервер её отдаёт
 * (DelayReport.stations/total_*). Аналога в gtport не было — подсистема новая.
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
          @if (filteredRecords().length) { <span class="sub">· {{ filteredRecords().length }} ваг. / {{ groups().length }} поездов</span> }
        </div>
      </ng-template>

      <ng-container *nzModalContent>
        <div class="filters">
          <nz-select class="term" nzSize="small" [ngModel]="terminal()" (ngModelChange)="terminal.set($event)">
            <nz-option nzValue="" nzLabel="Все терминалы"></nz-option>
            @for (t of terminals(); track t) { <nz-option [nzValue]="t" [nzLabel]="t"></nz-option> }
          </nz-select>
          <nz-select class="kind" nzSize="small" [ngModel]="kindFilter()" (ngModelChange)="kindFilter.set($event)">
            <nz-option nzValue="" nzLabel="Все задержки"></nz-option>
            <nz-option nzValue="4" nzLabel="Долгий простой"></nz-option>
            <nz-option nzValue="5" nzLabel="Брошенные"></nz-option>
          </nz-select>
          <label class="fl">Начало <input type="date" class="date" [ngModel]="start()" (ngModelChange)="start.set($event)" /></label>
          <label class="fl">Конец <input type="date" class="date" [ngModel]="end()" (ngModelChange)="end.set($event)" /></label>
          <button nz-button nzType="primary" nzSize="small" [nzLoading]="loading()" (click)="load()">
            <span nz-icon nzType="search"></span> Загрузить
          </button>
          <button nz-button nzSize="small" (click)="lastDays(7)">7 дней</button>
          <button nz-button nzSize="small" (click)="lastDays(30)">30 дней</button>
          <span class="spacer"></span>
          <button nz-button nzType="text" nzSize="small" (click)="exportExcel()" [disabled]="!filteredRecords().length"
                  nz-tooltip nzTooltipTitle="Экспорт в Excel">
            <span nz-icon nzType="download"></span>
          </button>
        </div>

        @if (loading()) {
          <div class="center"><nz-spin nzSimple></nz-spin></div>
        } @else if (report()) {
          <div class="dp-tbl-wrap" style="max-height: 62vh">
            <table class="dp-tbl">
              <thead><tr>
                <th class="c-idx">Индекс</th><th class="c-n">Вагонов</th><th class="c-term">Терминал</th>
                <th>Станция</th><th class="c-dor">Дорога</th><th class="c-kind">Вид</th>
                <th class="c-dt">Стоит с</th><th class="c-dt">По</th>
                <th class="c-h">Длит.</th><th class="c-h">В периоде</th>
              </tr></thead>
              <tbody>
                @for (g of groups(); track g.key) {
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
                    <td class="c">{{ g.openNow ? 'стоит' : fmtDT(g.maxTo) }}</td>
                    <td class="c num">{{ fmtDur(g.maxDur) }}</td>
                    <td class="c num"><b>{{ fmtDur(g.maxHours) }}</b></td>
                  </tr>
                  @if (isOpen(g.key)) {
                    @for (e of g.vagons; track e.id) {
                      <tr class="vag">
                        <td class="c-idx"></td>
                        <td class="c num" colspan="2">
                          @if (e.trip_id) {
                            <a class="lnk" (click)="openTrail(e)" nz-tooltip nzTooltipTitle="История движения вагона">{{ e.vagon }}</a>
                          } @else { {{ e.vagon }} }
                        </td>
                        <td class="ell" colspan="2">{{ e.station_name || ('код ' + e.station_code) }}</td>
                        <td class="c"><span class="dtag" [class.dtag5]="e.kind === 5">{{ e.kind === 5 ? 'брошен' : 'простой' }}</span></td>
                        <td class="c">{{ fmtDT(e.date_from) }}</td>
                        <td class="c">{{ e.date_to ? fmtDT(e.date_to) : 'стоит' }}</td>
                        <td class="c num">{{ fmtDur(fullHours(e)) }}</td>
                        <td class="c num">{{ fmtDur(e.hours_in_period) }}</td>
                      </tr>
                    }
                  }
                } @empty {
                  <tr><td colspan="10" class="empty">Задержек за период нет</td></tr>
                }
              </tbody>
            </table>
          </div>
          <p class="hint">Строка = поезд (эпизоды периода по индексу), клик — вагоны. «В периоде» — часть простоя внутри выбранных дат; открытые эпизоды считаются до текущего момента, «Стоит с»/«По» и длительности поезда — по самому раннему/позднему/долгому вагону.</p>
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
    .kind { width: 150px; }
    .dp-tbl-wrap { max-height: none; margin-bottom: var(--space-sm); }
    .dp-tbl th { white-space: nowrap; }
    .grp { cursor: pointer; }
    .grp:hover td { background: var(--color-bg-hover); }
    /* Заливки строк нет (решение владельца): брошенные — красным индексом и бейджем. */
    .grp.g5 .c-idx { color: var(--color-danger-text); font-weight: 600; }
    .vag td { background: var(--color-bg-subtle); }
    .tw { font-size: 10px; color: var(--color-text-muted); margin-right: 4px; }
    .c { text-align: center; white-space: nowrap; }
    .num { font-variant-numeric: tabular-nums; }
    .ell { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 260px; }
    .c-n { width: 70px; } .c-dor { width: 62px; } .c-h { width: 84px; }
    .c-idx { width: 130px; } .c-kind { width: 84px; } .c-dt { width: 112px; }
    .warn { color: var(--color-danger-text); }
    .lnk { color: var(--color-primary-active); text-decoration: underline; cursor: pointer; }
    .dtag { display: inline-block; padding: 0 6px; border-radius: 10px; font-size: 11px;
            background: var(--color-warning-bg); border: 1px solid var(--color-warning); }
    .dtag5 { color: var(--color-danger-text); border-color: var(--color-danger); }
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
  readonly kindFilter = signal('');
  readonly loading = signal(false);
  readonly report = signal<{ records: DelayEpisode[] } | null>(null);
  readonly open = signal<Set<string>>(new Set());
  readonly trailFor = signal<{ trip_id: string; vagon: string } | null>(null);

  /** Эпизоды периода с учётом фильтра по виду (все / простой / брошенные). */
  readonly filteredRecords = computed(() => {
    const kf = this.kindFilter();
    const recs = this.report()?.records ?? [];
    return kf ? recs.filter((e) => String(e.kind) === kf) : recs;
  });

  /** Группы-поезда (индекс + станция), как в «Задержанных вагонах»; вид группы — 5, если есть брошенные. */
  readonly groups = computed<ReportGroup[]>(() => {
    const map = new Map<string, ReportGroup>();
    for (const e of this.filteredRecords()) {
      const key = `${e.index}|${e.station_code}`;
      let g = map.get(key);
      if (!g) {
        g = {
          key, index: e.index, terminal: e.gruzpol_s,
          station: e.station_name || ('код ' + e.station_code), doroga: e.doroga,
          kind: e.kind, vagons: [], minFrom: e.date_from, maxTo: null, openNow: false,
          maxDur: 0, maxHours: 0,
        };
        map.set(key, g);
      }
      g.vagons.push(e);
      if (e.kind === 5) g.kind = 5;
      if (!g.terminal && e.gruzpol_s) g.terminal = e.gruzpol_s;
      if (e.date_from && (!g.minFrom || e.date_from < g.minFrom)) g.minFrom = e.date_from;
      if (!e.date_to) g.openNow = true;
      else if (!g.maxTo || e.date_to > g.maxTo) g.maxTo = e.date_to;
      if (this.fullHours(e) > g.maxDur) g.maxDur = this.fullHours(e);
      if (e.hours_in_period > g.maxHours) g.maxHours = e.hours_in_period;
    }
    return [...map.values()].sort((a, b) => b.maxHours - a.maxHours); // дольше всех — сверху
  });

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

  /** Экспорт эпизодов периода (с учётом фильтров терминала и вида). */
  async exportExcel(): Promise<void> {
    const recs = this.filteredRecords();
    if (!recs.length) return;
    try {
      const XLSX = await import('xlsx-js-style');
      const wb = XLSX.utils.book_new();
      const durD = (h: number) => (h ? Math.round((h / 24) * 100) / 100 : 0); // сутки, сотые

      const sh = [
        ['Вагон', 'Индекс', 'Родит. индекс', 'Терминал', 'Станция', 'Дорога', 'Вид', 'Стоит с', 'По', 'Длит., сут', 'В периоде, сут'],
        ...recs.map((e) => [
          e.vagon, e.index, e.index_main, e.gruzpol_s, e.station_name || e.station_code, e.doroga,
          e.kind === 5 ? 'брошен' : 'простой',
          this.fmtDT(e.date_from), e.date_to ? this.fmtDT(e.date_to) : 'стоит',
          durD(this.fullHours(e)), durD(e.hours_in_period),
        ]),
      ];
      const ws = XLSX.utils.aoa_to_sheet(sh);
      ws['!cols'] = [11, 15, 15, 10, 26, 10, 10, 14, 14, 11, 13].map((wch) => ({ wch }));
      XLSX.utils.book_append_sheet(wb, ws, 'Эпизоды');

      const label = this.terminal() || 'все_терминалы';
      XLSX.writeFile(wb, `простои_${label}_${this.fmtDate(this.start())}-${this.fmtDate(this.end())}.xlsx`);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    }
  }
}
