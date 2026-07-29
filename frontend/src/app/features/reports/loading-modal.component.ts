import { Component, OnInit, computed, inject, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { DragDropModule } from '@angular/cdk/drag-drop';
import { toBlob } from 'html-to-image';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzRadioModule } from 'ng-zorro-antd/radio';
import { NzSelectModule } from 'ng-zorro-antd/select';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { addDaysIso, todayMsk, yesterdayMsk } from '../../shared/msk-date';
import { ArrivalsApiService, TerminalTarget } from '../home/arrivals-api.service';
import { LoadingApiService, LoadingDailyRow } from './loading-api.service';
import { MaxApiService } from './max-api.service';

/** Строка сводки терминала: метка отправителя × группа груза. */
interface SummaryLine {
  sms1: string;
  group: string;
  count: number;
  avg: number; // среднесуточно с начала месяца
}

/** Сводка одного терминала (колонка вида «Сводка»). */
interface TerminalSummary {
  name: string;
  color: string;
  lines: SummaryLine[];
  groups: { group: string; count: number; avg: number }[]; // «Итого {группа}» при >1 группе
  total: { count: number; avg: number };
}

/**
 * Перемещаемая модалка «Погрузка в адрес портов» — обе формы gtport
 * (LoadingReport «день/с начала месяца» и LoadingReportPeriod «за период»)
 * одной формой с двумя видами (решение владельца 29.07.2026):
 * - «Сводка» — терминалы рядом, по каждому: отправитель (sms_1) × кол-во ×
 *   ср.сут (среднесуточно всегда с начала месяца, как в gtport); итоги по
 *   группам груза и общий; PNG и «В MAX» (сводка → сводный маршрут,
 *   срез терминала → его маршрут — как gtport слал 4 картинки);
 * - «По дням» — пивот «дата × отправитель» по одному терминалу за период,
 *   строки СР.СУТ. (на календарные сутки) и ИТОГО, Excel.
 *
 * Разбивка «уголь/металл» — по cargo_group из словаря грузов (в gtport была
 * зашита клиентом «ЕВРАЗ ТК» в SQL). Автообновления раз в 5 минут из gtport
 * нет (решение владельца, как в «Подходе») — есть «Обновить».
 */
@Component({
  selector: 'app-loading-modal',
  imports: [
    FormsModule, DragDropModule, NzButtonModule, NzIconModule, NzModalModule,
    NzRadioModule, NzSelectModule, NzSpinModule, NzTooltipModule,
  ],
  template: `
    <nz-modal [nzVisible]="true" [nzTitle]="ttl" [nzFooter]="null" [nzWidth]="view() === 'summary' ? '780px' : '1200px'"
              [nzMask]="false" (nzOnCancel)="closed.emit()">
      <ng-template #ttl>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          Погрузка в адрес портов
        </div>
      </ng-template>

      <ng-container *nzModalContent>
        <div class="filters">
          <nz-radio-group [ngModel]="view()" (ngModelChange)="switchView($event)" nzSize="small">
            <label nz-radio-button nzValue="summary">Сводка</label>
            <label nz-radio-button nzValue="daily">По дням</label>
          </nz-radio-group>

          @if (view() === 'summary') {
            <nz-radio-group [ngModel]="mode()" (ngModelChange)="mode.set($event)" nzSize="small">
              <label nz-radio-button nzValue="day">За день</label>
              <label nz-radio-button nzValue="month">С нач. месяца</label>
            </nz-radio-group>
            <label class="fl">Дата <input type="date" class="date" [ngModel]="date()" (ngModelChange)="pickDate($event)" /></label>
            <button nz-button nzType="primary" nzSize="small" [nzLoading]="loading()" (click)="loadSummary()">
              <span nz-icon nzType="reload"></span> Обновить
            </button>
            <span class="spacer"></span>
            <button nz-button nzSize="small" [nzLoading]="sending()" (click)="sendToMax()" [disabled]="!summaries().length"
                    nz-tooltip nzTooltipTitle="Сводка — в сводный чат, срез терминала — в его чат">
              <span nz-icon nzType="send"></span> В MAX
            </button>
            <button nz-button nzType="text" nzSize="small" (click)="exportPng()" [disabled]="!summaries().length"
                    nz-tooltip nzTooltipTitle="Сохранить как картинку">
              <span nz-icon nzType="camera"></span>
            </button>
          } @else {
            <nz-select class="term" nzSize="small" nzPlaceHolder="Терминал"
                       [ngModel]="terminal()" (ngModelChange)="pickTerminal($event)">
              @for (t of terminals(); track t.name) { <nz-option [nzValue]="t.name" [nzLabel]="t.name"></nz-option> }
            </nz-select>
            <label class="fl">С <input type="date" class="date" [ngModel]="from()" (ngModelChange)="from.set($event)" /></label>
            <label class="fl">По <input type="date" class="date" [ngModel]="to()" (ngModelChange)="to.set($event)" /></label>
            <button nz-button nzType="primary" nzSize="small" [nzLoading]="loading()" (click)="loadDaily()">
              <span nz-icon nzType="search"></span> Загрузить
            </button>
            <button nz-button nzSize="small" (click)="preset('month')">Текущий месяц</button>
            <button nz-button nzSize="small" (click)="preset('7')">7 дней</button>
            <button nz-button nzSize="small" (click)="preset('30')">30 дней</button>
            <span class="spacer"></span>
            <button nz-button nzType="text" nzSize="small" (click)="exportExcel()" [disabled]="!pivot().dates.length"
                    nz-tooltip nzTooltipTitle="Экспорт в Excel">
              <span nz-icon nzType="download"></span>
            </button>
          }
        </div>

        @if (loading()) {
          <div class="center"><nz-spin nzSimple></nz-spin></div>
        } @else if (view() === 'summary') {
          <div class="sumwrap" id="loading-summary">
            @for (s of summaries(); track s.name) {
              <div class="port">
                <div class="phead" [style.background]="s.color">{{ s.name }} {{ periodLabel() }}</div>
                <table class="dp-tbl">
                  <thead><tr><th>Отпр</th><th class="c-n">кол-во</th><th class="c-n">ср.сут</th></tr></thead>
                  <tbody>
                    @for (l of s.lines; track l.sms1 + '|' + l.group) {
                      <tr>
                        <td class="ell" [title]="l.sms1">{{ l.sms1 || '—' }}</td>
                        <td class="c num">{{ l.count }}</td>
                        <td class="c num avg">{{ fmtAvg(l.avg) }}</td>
                      </tr>
                    } @empty {
                      <tr><td colspan="3" class="empty">нет погрузки</td></tr>
                    }
                    @if (s.groups.length > 1) {
                      @for (g of s.groups; track g.group) {
                        <tr class="tot">
                          <td>Итого {{ g.group || 'прочее' }}</td>
                          <td class="c num">{{ g.count }}</td>
                          <td class="c num avg">{{ fmtAvg(g.avg) }}</td>
                        </tr>
                      }
                    }
                    <tr class="tot grand">
                      <td>Итого</td>
                      <td class="c num">{{ s.total.count }}</td>
                      <td class="c num avg">{{ fmtAvg(s.total.avg) }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
            }
          </div>
          <p class="hint">«ср.сут» — среднесуточная погрузка с начала месяца по выбранную дату (как в gtport). Разбивка по группам груза из словаря.</p>
        } @else {
          <div class="dp-tbl-wrap" style="max-height: 62vh">
            <table class="dp-tbl">
              <thead><tr>
                <th class="c-date">Дата</th>
                @for (c of pivot().cols; track c.key) { <th class="c-n" [style.background]="c.totalOf ? 'var(--color-bg-subtle)' : null">{{ c.title }}</th> }
                <th class="c-n all">Всего</th>
              </tr></thead>
              <tbody>
                @for (d of pivot().dates; track d) {
                  <tr>
                    <td class="c num">{{ fmtDate(d) }}</td>
                    @for (c of pivot().cols; track c.key) {
                      <td class="c num" [class.subtot]="c.totalOf">{{ pivot().cell[d + '|' + c.key] || '' }}</td>
                    }
                    <td class="c num all">{{ pivot().rowTotal[d] || 0 }}</td>
                  </tr>
                } @empty {
                  <tr><td [attr.colspan]="pivot().cols.length + 2" class="empty">Погрузки за период нет</td></tr>
                }
                @if (pivot().dates.length) {
                  <tr class="tot avgrow">
                    <td>СР.СУТ.</td>
                    @for (c of pivot().cols; track c.key) { <td class="c num">{{ fmtAvg((pivot().colTotal[c.key] || 0) / pivot().days) }}</td> }
                    <td class="c num">{{ fmtAvg(pivot().grand / pivot().days) }}</td>
                  </tr>
                  <tr class="tot grand">
                    <td>ИТОГО</td>
                    @for (c of pivot().cols; track c.key) { <td class="c num">{{ pivot().colTotal[c.key] || 0 }}</td> }
                    <td class="c num">{{ pivot().grand }}</td>
                  </tr>
                }
              </tbody>
            </table>
          </div>
          <p class="hint">СР.СУТ. — на календарные сутки периода ({{ pivot().days }}). Колонки «Всего по группе» появляются, когда у терминала грузы разных групп.</p>
        }
      </ng-container>
    </nz-modal>
  `,
  styles: [`
    .ttl { cursor: move; user-select: none; }
    .filters { display: flex; align-items: center; gap: var(--space-sm); margin-bottom: var(--space-sm); flex-wrap: wrap; }
    .fl { font-size: var(--font-size-sm); color: var(--color-text-secondary); display: inline-flex; align-items: center; gap: 4px; }
    .date { padding: 3px 6px; border: 1px solid var(--color-border); border-radius: var(--radius-sm); }
    .term { width: 130px; }
    .spacer { flex: 1 1 auto; }
    .center { display: flex; justify-content: center; padding: var(--space-lg); }
    .sumwrap { display: flex; gap: var(--space-md); align-items: flex-start; background: var(--color-bg-surface); padding: var(--space-sm); }
    .port { width: 226px; }
    .phead { padding: 4px 8px; font-weight: 600; border: 1px solid var(--color-border); border-bottom: 0; }
    .c { text-align: center; white-space: nowrap; }
    .num { font-variant-numeric: tabular-nums; }
    .avg { color: var(--color-text-muted); }
    .ell { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 130px; }
    .c-n { width: 58px; } .c-date { width: 86px; }
    .tot td { background: var(--color-bg-subtle); font-weight: 600; }
    .tot.grand td { background: var(--color-border-light); }
    .avgrow td { font-weight: 600; }
    .subtot { background: var(--color-bg-subtle); }
    .all { background: var(--color-bg-subtle); font-weight: 600; }
    .empty { text-align: center; color: var(--color-text-secondary); padding: var(--space-md); }
    .hint { margin: var(--space-xs) 0 0; color: var(--color-text-muted); font-size: var(--font-size-sm); }
  `],
})
export class LoadingModalComponent implements OnInit {
  private readonly api = inject(LoadingApiService);
  private readonly arrivals = inject(ArrivalsApiService);
  private readonly max = inject(MaxApiService);
  private readonly msg = inject(NzMessageService);

  readonly closed = output<void>();

  /** Стартовый вид: «Сводка» — из верхнего ряда «Справок», «По дням» — из отчётов. */
  readonly initialView = input<'summary' | 'daily'>('summary');

  readonly view = signal<'summary' | 'daily'>('summary');
  readonly loading = signal(false);
  readonly sending = signal(false);
  readonly terminals = signal<TerminalTarget[]>([]);

  // «Сводка»: дата + режим; данные всегда с 1-го числа месяца по дату.
  readonly date = signal(yesterdayMsk());
  readonly mode = signal<'day' | 'month'>('day');
  readonly monthRows = signal<LoadingDailyRow[]>([]);

  // «По дням»: терминал + произвольный период.
  readonly terminal = signal('');
  readonly from = signal(todayMsk().slice(0, 8) + '01');
  readonly to = signal(yesterdayMsk());
  readonly dailyRows = signal<LoadingDailyRow[]>([]);

  /** Сводки терминалов из месячных строк (кол-во за период, ср.сут за месяц). */
  readonly summaries = computed<TerminalSummary[]>(() => {
    const d = this.date();
    const days = Number(d.slice(8, 10)) || 1; // делитель ср.сут — день месяца (как gtport)
    const inPeriod = (r: LoadingDailyRow) => this.mode() === 'month' || r.day.slice(0, 10) === d;
    return this.terminals().map((t) => {
      const rows = this.monthRows().filter((r) => r.gruzpol_s === t.name);
      const byLine = new Map<string, { sms1: string; group: string; count: number; month: number }>();
      const byGroup = new Map<string, { count: number; month: number }>();
      let total = 0, totalMonth = 0;
      for (const r of rows) {
        const key = r.sms_1 + '|' + r.cargo_group;
        const line = byLine.get(key) ?? { sms1: r.sms_1, group: r.cargo_group, count: 0, month: 0 };
        const grp = byGroup.get(r.cargo_group) ?? { count: 0, month: 0 };
        if (inPeriod(r)) { line.count += r.vagon_count; grp.count += r.vagon_count; total += r.vagon_count; }
        line.month += r.vagon_count; grp.month += r.vagon_count; totalMonth += r.vagon_count;
        byLine.set(key, line);
        byGroup.set(r.cargo_group, grp);
      }
      // УГОЛЬ первым, прочие группы за ним (в gtport металл шёл в конце).
      const groupOrder = (g: string) => (g === 'УГОЛЬ' ? '0' : '1') + g;
      const lines = [...byLine.values()]
        .filter((l) => l.count > 0 || l.month > 0)
        .sort((a, b) => groupOrder(a.group).localeCompare(groupOrder(b.group), 'ru') || a.sms1.localeCompare(b.sms1, 'ru'))
        .map((l) => ({ sms1: l.sms1, group: l.group, count: l.count, avg: l.month / days }));
      const groups = [...byGroup.entries()]
        .sort((a, b) => groupOrder(a[0]).localeCompare(groupOrder(b[0]), 'ru'))
        .map(([group, v]) => ({ group, count: v.count, avg: v.month / days }));
      return {
        name: t.name, color: t.color, lines, groups,
        total: { count: total, avg: totalMonth / days },
      };
    });
  });

  /** Пивот «дата × отправитель» для вида «По дням» (только выбранный терминал). */
  readonly pivot = computed(() => {
    const rows = this.dailyRows().filter((r) => r.gruzpol_s === this.terminal());
    const groups = [...new Set(rows.map((r) => r.cargo_group))];
    const multi = groups.length > 1;
    const groupOrder = (g: string) => (g === 'УГОЛЬ' ? '0' : '1') + g;
    groups.sort((a, b) => groupOrder(a).localeCompare(groupOrder(b), 'ru'));

    const cols: { key: string; title: string; totalOf?: string }[] = [];
    for (const g of groups) {
      const senders = [...new Set(rows.filter((r) => r.cargo_group === g).map((r) => r.sms_1))]
        .sort((a, b) => a.localeCompare(b, 'ru'));
      for (const s of senders) {
        cols.push({ key: s + '|' + g, title: multi ? `${s || '—'} (${(g || 'прочее').toLowerCase()})` : (s || '—') });
      }
      if (multi) {
        cols.push({ key: '#' + g, title: `Всего ${(g || 'прочее').toLowerCase()}`, totalOf: g });
      }
    }

    const cell: Record<string, number> = {};
    const rowTotal: Record<string, number> = {};
    const colTotal: Record<string, number> = {};
    const dateSet = new Set<string>();
    let grand = 0;
    for (const r of rows) {
      const d = r.day.slice(0, 10);
      dateSet.add(d);
      const keys = [r.sms_1 + '|' + r.cargo_group, ...(multi ? ['#' + r.cargo_group] : [])];
      for (const k of keys) {
        cell[d + '|' + k] = (cell[d + '|' + k] || 0) + r.vagon_count;
        colTotal[k] = (colTotal[k] || 0) + r.vagon_count;
      }
      rowTotal[d] = (rowTotal[d] || 0) + r.vagon_count;
      grand += r.vagon_count;
    }
    const dates = [...dateSet].sort();
    const days = Math.max(1,
      Math.round((Date.parse(this.to()) - Date.parse(this.from())) / 86400000) + 1);
    return { cols, dates, cell, rowTotal, colTotal, grand, days };
  });

  ngOnInit(): void {
    void this.init();
  }

  private async init(): Promise<void> {
    try {
      const ts = await this.arrivals.getTerminals();
      this.terminals.set(ts);
      if (ts.length && !this.terminal()) this.terminal.set(ts[0].name);
    } catch { /* без реестра сводка будет пустой — тост даст загрузка */ }
    await this.loadSummary();
    if (this.initialView() === 'daily') this.switchView('daily');
  }

  periodLabel(): string {
    const d = this.date();
    const dd = `${d.slice(8, 10)}.${d.slice(5, 7)}.${d.slice(2, 4)}`;
    return this.mode() === 'month' ? `01-${dd}` : dd;
  }

  switchView(v: 'summary' | 'daily'): void {
    this.view.set(v);
    if (v === 'daily' && !this.dailyRows().length) void this.loadDaily();
  }

  pickDate(d: string): void {
    this.date.set(d);
    void this.loadSummary();
  }

  pickTerminal(t: string): void {
    this.terminal.set(t);
    void this.loadDaily();
  }

  preset(p: 'month' | '7' | '30'): void {
    this.to.set(yesterdayMsk());
    if (p === 'month') this.from.set(todayMsk().slice(0, 8) + '01');
    else this.from.set(addDaysIso(todayMsk(), -Number(p)));
    void this.loadDaily();
  }

  async loadSummary(): Promise<void> {
    this.loading.set(true);
    try {
      const d = this.date();
      const rep = await this.api.report(d.slice(0, 8) + '01', d);
      this.monthRows.set(rep.rows);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
      this.monthRows.set([]);
    } finally {
      this.loading.set(false);
    }
  }

  async loadDaily(): Promise<void> {
    this.loading.set(true);
    try {
      const rep = await this.api.report(this.from(), this.to());
      this.dailyRows.set(rep.rows);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
      this.dailyRows.set([]);
    } finally {
      this.loading.set(false);
    }
  }

  /** Средние: до суток — с одним знаком, иначе целое. */
  fmtAvg(v: number): string {
    if (!v) return '0';
    return v < 10 ? String(Math.round(v * 10) / 10) : String(Math.round(v));
  }

  /** «2026-07-24» → «24.07.26». */
  fmtDate(d: string): string {
    return `${d.slice(8, 10)}.${d.slice(5, 7)}.${d.slice(2, 4)}`;
  }

  // ── PNG / MAX (вид «Сводка») ────────────────────────────────────────────────
  private async png(el: HTMLElement): Promise<Blob> {
    const blob = await toBlob(el, { pixelRatio: 2, backgroundColor: '#ffffff' });
    if (!blob) throw new Error('не удалось отрисовать картинку');
    return blob;
  }

  private summaryEl(): HTMLElement {
    // Содержимое модалки живёт в overlay-портале CDK, вне host — ищем по документу.
    const el = document.querySelector('#loading-summary') as HTMLElement | null;
    if (!el) throw new Error('сводка не найдена');
    return el;
  }

  async exportPng(): Promise<void> {
    try {
      const blob = await this.png(this.summaryEl());
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `Погрузка_${this.date()}.png`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    }
  }

  /**
   * Как в gtport: сводная картинка → сводный маршрут (''), затем срез каждого
   * терминала → его маршрут. Ошибки по чатам показываем, отправку не срываем.
   */
  async sendToMax(): Promise<void> {
    this.sending.set(true);
    try {
      const caption = `Погрузка ${this.mode() === 'month' ? 'с начала месяца на ' : ''}${this.periodLabel()}`;
      const all = await this.png(this.summaryEl());
      const results = [await this.max.sendImage('pogruzka', '', all, `Погрузка_${this.date()}.png`, caption)];
      const ports = this.summaryEl().querySelectorAll<HTMLElement>('.port');
      for (let i = 0; i < ports.length; i++) {
        const name = this.terminals()[i]?.name;
        if (!name) continue;
        const blob = await this.png(ports[i]);
        results.push(await this.max.sendImage('pogruzka', name, blob, `Погрузка_${name}_${this.date()}.png`, `${caption} — ${name}`));
      }
      const sent = results.flatMap((r) => r.sent);
      const failed = results.flatMap((r) => Object.keys(r.failed));
      if (!sent.length && !failed.length) {
        this.msg.warning('Нет настроенного маршрута рассылки (форма «pogruzka»)');
      } else if (failed.length) {
        this.msg.warning(`Отправлено в ${sent.length}, не ушло — ${failed.join(', ')}`);
      } else {
        this.msg.success(`Отправлено в чаты (${[...new Set(sent)].join(', ')})`);
      }
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.sending.set(false);
    }
  }

  // ── Excel (вид «По дням»; раскладка как в gtport LoadingReportPeriod) ───────
  async exportExcel(): Promise<void> {
    const p = this.pivot();
    if (!p.dates.length) return;
    try {
      const XLSX = await import('xlsx-js-style');
      const border = { style: 'thin', color: { rgb: '000000' } };
      const borders = { top: border, bottom: border, left: border, right: border };
      const center = { horizontal: 'center', vertical: 'center' };
      const cell = (v: unknown, fill = '', bold = false, num = false) => ({
        v, t: num ? 'n' : 's',
        s: {
          font: { sz: 11, bold }, border: borders, alignment: center,
          ...(fill ? { fill: { fgColor: { rgb: fill } } } : {}),
        },
      });

      const rows: unknown[][] = [];
      rows.push([cell('Дата', 'E0E0E0', true), ...p.cols.map((c) => cell(c.title, 'E0E0E0', true)), cell('Всего', 'E0E0E0', true)]);
      for (const d of p.dates) {
        rows.push([
          cell(this.fmtDate(d)),
          ...p.cols.map((c) => cell(p.cell[d + '|' + c.key] || 0, c.totalOf ? 'F5F5F5' : '', !!c.totalOf, true)),
          cell(p.rowTotal[d] || 0, 'F5F5F5', true, true),
        ]);
      }
      rows.push([
        cell('СРЕДНЕСУТ.', 'E3F2FD', true),
        ...p.cols.map((c) => cell(Math.round(((p.colTotal[c.key] || 0) / p.days) * 10) / 10, 'E3F2FD', true, true)),
        cell(Math.round((p.grand / p.days) * 10) / 10, 'E3F2FD', true, true),
      ]);
      rows.push([
        cell('ИТОГО', 'FFF9C4', true),
        ...p.cols.map((c) => cell(p.colTotal[c.key] || 0, 'FFF9C4', true, true)),
        cell(p.grand, 'FFF9C4', true, true),
      ]);

      const ws = XLSX.utils.aoa_to_sheet(rows as never[][]);
      ws['!cols'] = [14, ...p.cols.map(() => 18), 14].map((wch) => ({ wch }));
      const wb = XLSX.utils.book_new();
      XLSX.utils.book_append_sheet(wb, ws, 'Погрузка');
      XLSX.writeFile(wb, `погрузка_${this.terminal()}_${this.fmtDate(this.from())}-${this.fmtDate(this.to())}.xlsx`);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    }
  }
}
