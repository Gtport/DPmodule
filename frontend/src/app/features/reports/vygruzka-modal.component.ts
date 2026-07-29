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
import { addDaysIso, todayMsk, yesterdayMsk } from '../../shared/msk-date';
import { ArrivalsApiService, TerminalTarget } from '../home/arrivals-api.service';
import { VygruzkaApiService, VygruzkaDay, VygruzkaPeriod } from './vygruzka-api.service';

/**
 * Перемещаемая модалка «Выгрузка за период» (перенос gtport
 * OperatorToolsCargoPeriod): сохранённые учётные листы «Грузовой работы»
 * терминала по суткам. Колонки — по линиям учёта из справочника
 * port_cargo_line (в gtport трёхкратные семейства coal_/metal_/chugun_ были
 * зашиты в колонки vigr_gut); у терминала с одной линией — плоская таблица с
 * примечанием, с несколькими — группа из 8 колонок на линию. Строка
 * СРЕДНЕСУТ. — среднее по имеющимся дням (как в gtport: делитель — число
 * записей, не календарных суток). Эффективность подсвечивается: ≥100 —
 * зелёным, ≤90 — красным, между — жёлтым. Excel — «Выгрузка» + «Погрузка»
 * (если у терминала есть строки погрузки). Дневной лист смотрят и правят в
 * «Грузовой работе» (кнопка на странице отчётов).
 */
@Component({
  selector: 'app-vygruzka-modal',
  imports: [
    FormsModule, DragDropModule, NzButtonModule, NzIconModule, NzModalModule,
    NzSelectModule, NzSpinModule, NzTooltipModule,
  ],
  template: `
    <nz-modal [nzVisible]="true" [nzTitle]="ttl" [nzFooter]="null" [nzWidth]="multi() ? '1500px' : '1000px'"
              [nzMask]="false" (nzOnCancel)="closed.emit()">
      <ng-template #ttl>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          <span class="tbadge" [style.background]="period()?.color || 'transparent'">{{ terminal() }}</span>
          Выгрузка за период
          <span class="sub">{{ fmtDate(from()) }} – {{ fmtDate(to()) }} · дней с данными: {{ days().length }}</span>
        </div>
      </ng-template>

      <ng-container *nzModalContent>
        <div class="filters">
          <nz-select class="term" nzSize="small" [ngModel]="terminal()" (ngModelChange)="pickTerminal($event)">
            @for (t of terminals(); track t.name) { <nz-option [nzValue]="t.name" [nzLabel]="t.name"></nz-option> }
          </nz-select>
          <label class="fl">С <input type="date" class="date" [ngModel]="from()" (ngModelChange)="from.set($event)" /></label>
          <label class="fl">По <input type="date" class="date" [ngModel]="to()" (ngModelChange)="to.set($event)" /></label>
          <button nz-button nzType="primary" nzSize="small" [nzLoading]="loading()" (click)="load()">
            <span nz-icon nzType="search"></span> Загрузить
          </button>
          <button nz-button nzSize="small" (click)="preset('month')">Текущий месяц</button>
          <button nz-button nzSize="small" (click)="preset('30')">30 дней</button>
          <span class="spacer"></span>
          <button nz-button nzType="text" nzSize="small" (click)="exportExcel()" [disabled]="!days().length"
                  nz-tooltip nzTooltipTitle="Экспорт в Excel">
            <span nz-icon nzType="download"></span>
          </button>
        </div>

        @if (loading()) {
          <div class="center"><nz-spin nzSimple></nz-spin></div>
        } @else {
          <div class="dp-tbl-wrap" style="max-height: 64vh">
            <table class="dp-tbl">
              <thead>
                @if (multi()) {
                  <tr>
                    <th rowspan="2" class="c-date">Дата</th>
                    @for (l of lineDefs(); track l.cargo_key) {
                      <th colspan="8" class="grp" [style.background]="period()?.color || null">{{ l.label }}</th>
                    }
                    <th rowspan="2" class="c-prim">Прим.</th>
                  </tr>
                  <tr>
                    @for (l of lineDefs(); track l.cargo_key) {
                      <th class="c-n">Ост18</th><th class="c-n">Приб</th><th class="c-n">Выгр</th><th class="c-n">Ост</th>
                      <th class="c-n">Полез</th><th class="c-n">Полн</th><th class="c-n">Прост</th><th class="c-n">Эфф</th>
                    }
                  </tr>
                } @else {
                  <tr>
                    <th class="c-date">Дата</th>
                    <th class="c-n">Ост18</th><th class="c-n">Прибыло</th><th class="c-n">Выгрузка</th><th class="c-n">Остаток</th>
                    <th class="c-n">Полезное</th><th class="c-n">Полное</th><th class="c-n">Простой (ч)</th><th class="c-n">Эфф</th>
                    <th class="c-prim">Прим.</th>
                  </tr>
                }
              </thead>
              <tbody>
                @for (d of daysDesc(); track d.date) {
                  <tr>
                    <td class="c num">{{ fmtDate(d.date) }}</td>
                    @for (l of lineDefs(); track l.cargo_key) {
                      @if (lineOf(d, l.cargo_key); as ln) {
                        <td class="c num">{{ ln.ost_18 }}</td>
                        <td class="c num">{{ ln.prib }}</td>
                        <td class="c num">{{ ln.vigr_fact }}</td>
                        <td class="c num">{{ ln.ost }}</td>
                        <td class="c num">{{ ln.useful_formation }}</td>
                        <td class="c num">{{ ln.total_formation }}</td>
                        <td class="c num">{{ downtimeH(ln.downtime) }}</td>
                        <td class="c num" [style.background]="effBg(ln.effectiv)">{{ ln.effectiv }}%</td>
                      }
                    }
                    <td class="ell" [title]="primOf(d)">{{ primOf(d) }}</td>
                  </tr>
                } @empty {
                  <tr><td [attr.colspan]="2 + lineDefs().length * 8" class="empty">Учётных листов за период нет</td></tr>
                }
                @if (days().length) {
                  <tr class="tot">
                    <td>СРЕДНЕСУТ.</td>
                    @for (l of lineDefs(); track l.cargo_key) {
                      @if (avgOf(l.cargo_key); as a) {
                        <td class="c num">{{ a.ost18 }}</td><td class="c num">{{ a.prib }}</td>
                        <td class="c num">{{ a.vigr }}</td><td class="c num">{{ a.ost }}</td>
                        <td class="c num">{{ a.useful }}</td><td class="c num">{{ a.total }}</td>
                        <td class="c num">{{ a.downtime }}</td>
                        <td class="c num" [style.background]="effBg(a.eff)">{{ a.eff }}%</td>
                      }
                    }
                    <td></td>
                  </tr>
                }
              </tbody>
            </table>
          </div>
          <p class="hint">СРЕДНЕСУТ. — среднее по дням с данными ({{ days().length }}), как в gtport. Дневной лист правится в «Грузовой работе».</p>
        }
      </ng-container>
    </nz-modal>
  `,
  styles: [`
    .ttl { cursor: move; user-select: none; display: flex; align-items: center; gap: var(--space-sm); }
    .ttl .sub { color: var(--color-text-muted); font-weight: 400; font-size: var(--font-size-sm); }
    .tbadge { padding: 0 8px; border-radius: var(--radius-sm); border: 1px solid var(--color-border); font-size: var(--font-size-sm); }
    .filters { display: flex; align-items: center; gap: var(--space-sm); margin-bottom: var(--space-sm); flex-wrap: wrap; }
    .term { width: 130px; }
    .fl { font-size: var(--font-size-sm); color: var(--color-text-secondary); display: inline-flex; align-items: center; gap: 4px; }
    .date { padding: 3px 6px; border: 1px solid var(--color-border); border-radius: var(--radius-sm); }
    .spacer { flex: 1 1 auto; }
    .center { display: flex; justify-content: center; padding: var(--space-lg); }
    .c { text-align: center; white-space: nowrap; }
    .num { font-variant-numeric: tabular-nums; }
    .grp { text-align: center; font-weight: 600; }
    .c-date { width: 84px; } .c-n { width: 52px; } .c-prim { width: 160px; }
    .ell { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 200px; }
    .tot td { background: var(--color-warning-bg); font-weight: 600; }
    .empty { text-align: center; color: var(--color-text-secondary); padding: var(--space-md); }
    .hint { margin: var(--space-xs) 0 0; color: var(--color-text-muted); font-size: var(--font-size-sm); }
  `],
})
export class VygruzkaModalComponent implements OnInit {
  private readonly api = inject(VygruzkaApiService);
  private readonly arrivals = inject(ArrivalsApiService);
  private readonly msg = inject(NzMessageService);

  readonly closed = output<void>();

  readonly loading = signal(false);
  readonly terminals = signal<TerminalTarget[]>([]);
  readonly terminal = signal('');
  readonly from = signal(todayMsk().slice(0, 8) + '01');
  readonly to = signal(yesterdayMsk());
  readonly period = signal<VygruzkaPeriod | null>(null);

  readonly days = computed(() => this.period()?.days ?? []);
  /** На экране свежие сверху (как в gtport, ORDER BY date DESC). */
  readonly daysDesc = computed(() => [...this.days()].reverse());
  /** Линии учёта — из первого дня (состав задаёт справочник, одинаков по дням). */
  readonly lineDefs = computed(() => this.days()[0]?.lines.map((l) => ({ cargo_key: l.cargo_key, label: l.label })) ?? []);
  readonly multi = computed(() => this.lineDefs().length > 1);

  ngOnInit(): void {
    void this.init();
  }

  private async init(): Promise<void> {
    try {
      const ts = await this.arrivals.getTerminals();
      this.terminals.set(ts);
      if (ts.length && !this.terminal()) this.terminal.set(ts[0].name);
    } catch { /* реестр не критичен */ }
    await this.load();
  }

  pickTerminal(t: string): void {
    this.terminal.set(t);
    void this.load();
  }

  preset(p: 'month' | '30'): void {
    this.to.set(yesterdayMsk());
    this.from.set(p === 'month' ? todayMsk().slice(0, 8) + '01' : addDaysIso(todayMsk(), -30));
    void this.load();
  }

  async load(): Promise<void> {
    if (!this.terminal()) return;
    this.loading.set(true);
    try {
      this.period.set(await this.api.period(this.terminal(), this.from(), this.to()));
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
      this.period.set(null);
    } finally {
      this.loading.set(false);
    }
  }

  lineOf(d: VygruzkaDay, key: string) {
    return d.lines.find((l) => l.cargo_key === key) ?? null;
  }

  /** Примечание дня: у многолинейных терминалов показываем первое непустое. */
  primOf(d: VygruzkaDay): string {
    return d.lines.map((l) => l.prim).find((p) => p) ?? '';
  }

  /** «Ч:ММ» → часы с десятыми («26:30» → 26.5); пусто → 0. */
  downtimeH(v: string): string {
    if (!v) return '0';
    const [h, m] = v.split(':').map(Number);
    const hours = (h || 0) + (m || 0) / 60;
    return String(Math.round(hours * 10) / 10);
  }

  /** Подсветка эффективности как в gtport: ≥100 зелёная, ≤90 красная, между — жёлтая. */
  effBg(eff: number): string | null {
    if (!eff) return null;
    if (eff >= 100) return '#E8F5E9';
    if (eff <= 90) return '#FFEBEE';
    return '#FFF9C4';
  }

  /** Среднесуточные по линии — среднее по имеющимся дням (делитель — их число). */
  avgOf(key: string) {
    const days = this.days();
    if (!days.length) return null;
    let ost18 = 0, prib = 0, vigr = 0, ost = 0, useful = 0, total = 0, eff = 0, dt = 0;
    for (const d of days) {
      const l = this.lineOf(d, key);
      if (!l) continue;
      ost18 += l.ost_18; prib += l.prib; vigr += l.vigr_fact; ost += l.ost;
      useful += l.useful_formation; total += l.total_formation; eff += l.effectiv;
      dt += Number(this.downtimeH(l.downtime));
    }
    const n = days.length;
    const r = (v: number) => Math.round(v / n);
    return {
      ost18: r(ost18), prib: r(prib), vigr: r(vigr), ost: r(ost),
      useful: r(useful), total: r(total), eff: r(eff),
      downtime: String(Math.round((dt / n) * 10) / 10),
    };
  }

  /** «2026-07-24» → «24.07.26». */
  fmtDate(d: string): string {
    if (!d || d.length < 10) return '—';
    return `${d.slice(8, 10)}.${d.slice(5, 7)}.${d.slice(2, 4)}`;
  }

  // ── Excel: лист «Выгрузка» (+ «Погрузка», если есть строки погрузки) ────────
  async exportExcel(): Promise<void> {
    const days = this.days();
    if (!days.length) return;
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

      const defs = this.lineDefs();
      const multi = defs.length > 1;
      const metric = ['Ост18', 'Приб', 'Выгр', 'Ост', 'Полез', 'Полное', 'Прост (ч)', 'Эфф'];
      const head = ['Дата',
        ...defs.flatMap((l) => metric.map((m) => (multi ? `${l.label}: ${m}` : m))),
        'Примечание'];

      const rows: unknown[][] = [head.map((h) => cell(h, 'E0E0E0', true))];
      for (const d of days) { // в Excel по возрастанию даты (как gtport)
        const r: unknown[] = [cell(this.fmtDate(d.date))];
        for (const def of defs) {
          const l = this.lineOf(d, def.cargo_key);
          r.push(
            cell(l?.ost_18 ?? 0, '', false, true), cell(l?.prib ?? 0, '', false, true),
            cell(l?.vigr_fact ?? 0, '', false, true), cell(l?.ost ?? 0, '', false, true),
            cell(l?.useful_formation ?? 0, '', false, true), cell(l?.total_formation ?? 0, '', false, true),
            cell(Number(this.downtimeH(l?.downtime ?? '')), '', false, true),
            cell(l?.effectiv ?? 0, '', false, true),
          );
        }
        r.push(cell(this.primOf(d)));
        rows.push(r);
      }
      const avgRow: unknown[] = [cell('СРЕДНЕСУТ.', 'FFF9C4', true)];
      for (const def of defs) {
        const a = this.avgOf(def.cargo_key)!;
        avgRow.push(...[a.ost18, a.prib, a.vigr, a.ost, a.useful, a.total, Number(a.downtime), a.eff]
          .map((v) => cell(v, 'FFF9C4', true, true)));
      }
      avgRow.push(cell('', 'FFF9C4'));
      rows.push(avgRow);

      const ws = XLSX.utils.aoa_to_sheet(rows as never[][]);
      ws['!cols'] = [10, ...defs.flatMap(() => [7, 7, 7, 7, 7, 7, 9, 7]), 24].map((wch) => ({ wch }));
      const wb = XLSX.utils.book_new();
      XLSX.utils.book_append_sheet(wb, ws, 'Выгрузка');

      // Лист «Погрузка»: значения «факт/план/ост» по строкам погрузки терминала.
      const loadDefs = days.find((d) => d.load.length)?.load.map((l) => ({ key: l.cargo_key, label: l.label })) ?? [];
      if (loadDefs.length) {
        const lrows: unknown[][] = [
          ['Дата', ...loadDefs.map((l) => l.label)].map((h) => cell(h, 'E0E0E0', true)),
        ];
        for (const d of days) {
          lrows.push([
            cell(this.fmtDate(d.date)),
            ...loadDefs.map((def) => {
              const l = d.load.find((x) => x.cargo_key === def.key);
              return cell(l ? `${l.load_fact}/${l.plan}/${l.ost}` : '');
            }),
          ]);
        }
        const lws = XLSX.utils.aoa_to_sheet(lrows as never[][]);
        lws['!cols'] = [10, ...loadDefs.map(() => 14)].map((wch) => ({ wch }));
        XLSX.utils.book_append_sheet(wb, lws, 'Погрузка');
      }

      XLSX.writeFile(wb, `выгрузка_${this.terminal()}_${this.fmtDate(this.from())}-${this.fmtDate(this.to())}.xlsx`);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    }
  }
}
