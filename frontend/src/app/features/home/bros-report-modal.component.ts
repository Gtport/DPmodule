import { Component, ElementRef, OnInit, computed, inject, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { DragDropModule } from '@angular/cdk/drag-drop';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzSelectModule } from 'ng-zorro-antd/select';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzMessageService } from 'ng-zorro-antd/message';
import { toBlob } from 'html-to-image';
import { apiErrorMessage } from '../../core/api/api-error';
import { addDaysIso, todayMsk } from '../../shared/msk-date';
import { ArrivalsApiService } from './arrivals-api.service';
import { ReferenceApiService } from '../reference/reference-api.service';
import { BrosApiService, BrosReportRow, BrosReasonCode } from './bros-api.service';

/** Суточный агрегат периода. */
interface DailyStat { date: string; count: number; created: number; lifted: number; }

/**
 * Перемещаемая модалка «Брошенные — отчёт за период» (перенос gtport BrosReport):
 * броски за период с суммарным простоем (суток), суточная динамика и сводка.
 * Экспорт Excel (данные + динамика), PNG-снимок таблицы и отправка сводки в MAX.
 *
 * Разбивка суток по кодам (05/01-согл/01-несогл) считается по журналу и требует
 * агрегации — следующая итерация (пока показываем суммарный простой и текущий код).
 */
@Component({
  selector: 'app-bros-report-modal',
  imports: [
    FormsModule, DragDropModule, NzModalModule, NzButtonModule, NzIconModule,
    NzSelectModule, NzSpinModule, NzTooltipModule,
  ],
  template: `
    <nz-modal [nzVisible]="true" [nzTitle]="ttl" [nzFooter]="null" nzWidth="1120px"
              [nzMask]="false" (nzOnCancel)="closed.emit()">
      <ng-template #ttl>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          Брошенные — отчёт за период
        </div>
      </ng-template>

      <ng-container *nzModalContent>
        <div class="bar">
          <input type="date" class="date" [ngModel]="start()" (ngModelChange)="start.set($event)" />
          <span class="dash">—</span>
          <input type="date" class="date" [ngModel]="end()" (ngModelChange)="end.set($event)" />
          <nz-select class="term" nzSize="small" [ngModel]="terminal()" (ngModelChange)="terminal.set($event)">
            <nz-option nzValue="" nzLabel="Все терминалы"></nz-option>
            @for (t of terminals(); track t) { <nz-option [nzValue]="t" [nzLabel]="t"></nz-option> }
          </nz-select>
          <button nz-button nzType="primary" nzSize="small" [nzLoading]="loading()" (click)="load()">
            <span nz-icon nzType="search"></span> Загрузить
          </button>
          <span class="spacer"></span>
          <button nz-button nzSize="small" (click)="exportExcel()" nz-tooltip nzTooltipTitle="Экспорт в Excel"
                  [disabled]="rows().length === 0">
            <span nz-icon nzType="file-excel"></span>
          </button>
          <button nz-button nzSize="small" (click)="exportPng()" nz-tooltip nzTooltipTitle="Сохранить картинку"
                  [disabled]="rows().length === 0">
            <span nz-icon nzType="picture"></span>
          </button>
          <button nz-button nzType="primary" nzSize="small" [nzLoading]="sending()" (click)="sendToMax()"
                  nz-tooltip nzTooltipTitle="Сводку в общий чат MAX" [disabled]="rows().length === 0">
            <span nz-icon nzType="send"></span> В MAX
          </button>
        </div>

        <!-- Сводка -->
        <div class="tiles">
          <div class="tile"><b>{{ rows().length }}</b><span>поездов</span></div>
          <div class="tile"><b>{{ totalVagons() }}</b><span>вагонов</span></div>
          <div class="tile"><b>{{ totalDays() }}</b><span>поездо-суток</span></div>
          <div class="tile"><b>{{ avgDays() }}</b><span>ср. суток/поезд</span></div>
          <div class="tile"><b>{{ totalCreated() }}</b><span>новых бросков</span></div>
          <div class="tile"><b>{{ totalLifted() }}</b><span>поднято</span></div>
        </div>

        <!-- Разбивка поездо-суток по типам причин -->
        <div class="tiles">
          <div class="tile c05"><b>{{ daysCode05() }}</b><span>к.05 (заявка)</span></div>
          <div class="tile ok"><b>{{ daysAgreed() }}</b><span>01 согл. (письмо)</span></div>
          <div class="tile bad"><b>{{ daysNotAgreed() }}</b><span>01 несогл. (РЖД)</span></div>
          <div class="tile"><b>{{ daysOther() }}</b><span>прочие</span></div>
          <div class="tile prot"><b>{{ protectedPct() }}%</b><span>согласованный простой</span></div>
        </div>

        @if (loading()) {
          <div class="center"><nz-spin nzSimple></nz-spin></div>
        } @else {
          <div class="tbl-wrap" id="bros-report-tbl">
            <table class="tbl">
              <thead>
                <tr>
                  <th class="c-idx">Индекс</th>
                  <th>Станция</th>
                  <th class="c-dor">Дор</th>
                  <th class="c-dt">Дата броса</th>
                  <th class="c-dt">Дата подъёма</th>
                  <th class="c-code">Код</th>
                  <th class="c-days">Суток</th>
                  <th class="c-b" nz-tooltip nzTooltipTitle="Суток по коду 05 (заявка)">к.05</th>
                  <th class="c-b" nz-tooltip nzTooltipTitle="Суток по коду 01 согласованному (письмо)">01✓</th>
                  <th class="c-b" nz-tooltip nzTooltipTitle="Суток по коду 01 несогласованному">01✗</th>
                  <th class="c-b" nz-tooltip nzTooltipTitle="Суток по прочим кодам / без журнала">проч</th>
                  <th class="c-vc">Ваг</th>
                  <th>Состав</th>
                </tr>
              </thead>
              <tbody>
                @for (r of rows(); track r.id) {
                  <tr [style.background]="rowBg(r)">
                    <td class="num idx" [title]="r.index_1">{{ r.index_1 || '—' }}</td>
                    <td class="ell" [title]="r.station_br">{{ r.station_br || '—' }}</td>
                    <td class="c">{{ r.doroga_br || '—' }}</td>
                    <td class="c">{{ fmtDate(r.date_br) }}</td>
                    <td class="c">{{ r.date_pod_fact ? fmtDate(r.date_pod_fact) : (r.status_br ? 'активен' : '—') }}</td>
                    <td class="c">
                      @if (r.reason) {
                        <span class="code" nz-tooltip [nzTooltipTitle]="codeDesc(r.reason)">{{ r.reason }}</span>
                      } @else { — }
                    </td>
                    <td class="c days" [class.warn]="r.days_total >= 4" [class.danger]="r.days_total >= 7">{{ r.days_total }}</td>
                    <td class="c b c05">{{ r.days_code05 || '' }}</td>
                    <td class="c b ok">{{ r.days_code01_agreed || '' }}</td>
                    <td class="c b bad">{{ r.days_code01_notagreed || '' }}</td>
                    <td class="c b">{{ r.days_other || '' }}</td>
                    <td class="c num">{{ r.vagon_count }}</td>
                    <td class="ell" [title]="r.sostav">{{ r.sostav || '—' }}</td>
                  </tr>
                } @empty {
                  <tr><td colspan="13" class="empty">Нет данных за выбранный период</td></tr>
                }
              </tbody>
            </table>
          </div>
        }
        <p class="hint">Суток — суммарный простой (дата броса → подъём или сегодня), разбивка по кодам из журнала.
          «Согласованный простой» — есть юр. оформление (заявка 05 или письмо 01-согл), остальное — ответственность РЖД.</p>
      </ng-container>
    </nz-modal>
  `,
  styles: [`
    .ttl { cursor: move; user-select: none; }
    .bar { display: flex; align-items: center; gap: var(--space-sm); margin-bottom: var(--space-sm); flex-wrap: wrap; }
    .date { padding: 3px 6px; border: 1px solid var(--color-border); border-radius: var(--radius-sm); }
    .dash { color: var(--color-text-muted); }
    .term { width: 150px; }
    .spacer { flex: 1 1 auto; }
    .tiles { display: flex; gap: var(--space-sm); flex-wrap: wrap; margin-bottom: var(--space-sm); }
    .tile { flex: 1 1 0; min-width: 90px; text-align: center; padding: 6px 8px;
            border: 1px solid var(--color-border-light); border-radius: var(--radius-sm); background: var(--color-bg-subtle); }
    .tile b { display: block; font-size: 1.25rem; font-variant-numeric: tabular-nums; line-height: 1.1; }
    .tile span { font-size: var(--font-size-sm); color: var(--color-text-muted); }
    .center { display: flex; justify-content: center; padding: var(--space-xl); }
    .tbl-wrap { max-height: 56vh; overflow: auto; background: #fff; }
    .tbl { width: 100%; border-collapse: collapse; font-size: var(--font-size-sm); table-layout: fixed; }
    .tbl th { position: sticky; top: 0; background: var(--color-bg-subtle); font-weight: 600;
              padding: 4px 8px; border: 1px solid var(--color-border-light); text-align: center; z-index: 1; }
    .tbl td { padding: 3px 8px; border: 1px solid var(--color-border-light); }
    .c-idx { width: 118px; } .c-dor { width: 44px; } .c-dt { width: 92px; }
    .c-code { width: 46px; } .c-days { width: 52px; } .c-b { width: 42px; } .c-vc { width: 44px; }
    .b { font-variant-numeric: tabular-nums; }
    td.b.c05 { color: #1565c0; } td.b.ok { color: #2e7d32; } td.b.bad { color: #c62828; font-weight: 600; }
    .tile.c05 b { color: #1565c0; } .tile.ok b { color: #2e7d32; } .tile.bad b { color: #c62828; }
    .tile.prot { background: #f3e5f5; } .tile.prot b { color: #7b1fa2; }
    .num { font-variant-numeric: tabular-nums; }
    .idx, .ell { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .c { text-align: center; }
    .code { font-weight: 600; cursor: help; text-decoration: underline dotted; text-underline-offset: 2px; }
    .days { font-weight: 600; }
    .days.warn { color: #d46b08; } .days.danger { color: var(--color-danger, #cf1322); }
    .empty { text-align: center; color: var(--color-text-secondary); padding: var(--space-md); }
    .hint { margin: var(--space-xs) 0 0; color: var(--color-text-muted); font-size: var(--font-size-sm); }
  `],
})
export class BrosReportModalComponent implements OnInit {
  private readonly api = inject(BrosApiService);
  private readonly arrivals = inject(ArrivalsApiService);
  private readonly ref = inject(ReferenceApiService);
  private readonly msg = inject(NzMessageService);
  private readonly host = inject(ElementRef<HTMLElement>);

  readonly closed = output<void>();

  readonly start = signal(addDaysIso(todayMsk(), -30));
  readonly end = signal(todayMsk());
  readonly terminal = signal('');
  readonly loading = signal(false);
  readonly sending = signal(false);

  readonly rows = signal<BrosReportRow[]>([]);
  readonly terminals = signal<string[]>([]);
  readonly codes = signal<BrosReasonCode[]>([]);
  private readonly termColor = signal<Record<string, string>>({});

  private readonly codesMap = computed(() => {
    const m: Record<string, string> = {};
    for (const c of this.codes()) m[c.code] = c.description;
    return m;
  });

  // ── Сводка ────────────────────────────────────────────────────────────────
  readonly totalVagons = computed(() => this.rows().reduce((s, r) => s + r.vagon_count, 0));
  readonly totalDays = computed(() => this.rows().reduce((s, r) => s + r.days_total, 0));
  readonly avgDays = computed(() => {
    const n = this.rows().length;
    return n ? Math.round((this.totalDays() / n) * 10) / 10 : 0;
  });
  // Разбивка поездо-суток по типам причин (агрегация журнала на бэке).
  readonly daysCode05 = computed(() => this.rows().reduce((s, r) => s + r.days_code05, 0));
  readonly daysAgreed = computed(() => this.rows().reduce((s, r) => s + r.days_code01_agreed, 0));
  readonly daysNotAgreed = computed(() => this.rows().reduce((s, r) => s + r.days_code01_notagreed, 0));
  readonly daysOther = computed(() => this.rows().reduce((s, r) => s + r.days_other, 0));
  // «Согласованный» простой (заявка 05 + письмо 01-согл) — есть юр. оформление.
  readonly protectedPct = computed(() => {
    const t = this.totalDays();
    return t ? Math.round(((this.daysCode05() + this.daysAgreed()) / t) * 100) : 0;
  });
  private readonly dailyStats = computed<DailyStat[]>(() => this.computeDaily());
  readonly totalCreated = computed(() => this.dailyStats().reduce((s, d) => s + d.created, 0));
  readonly totalLifted = computed(() => this.dailyStats().reduce((s, d) => s + d.lifted, 0));

  ngOnInit(): void {
    void this.loadRefs();
    void this.load();
  }

  async load(): Promise<void> {
    this.loading.set(true);
    try {
      this.rows.set(await this.api.report(this.start(), this.end(), this.terminal()));
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
      this.rows.set([]);
    } finally {
      this.loading.set(false);
    }
  }

  private async loadRefs(): Promise<void> {
    try {
      const [codes, terms] = await Promise.all([
        this.api.getReasonCodes(),
        this.arrivals.getTerminals(),
      ]);
      this.codes.set(codes);
      this.terminals.set(terms.map((t) => t.name));
      const cm: Record<string, string> = {};
      for (const t of terms) cm[t.name] = t.color;
      this.termColor.set(cm);
    } catch {
      /* справочники не критичны */
    }
  }

  rowBg(r: BrosReportRow): string | null { return this.termColor()[r.gruzpol_s] ?? null; }
  codeDesc(code: string): string { return this.codesMap()[code] || 'Код не найден в справочнике'; }

  private computeDaily(): DailyStat[] {
    const s = dayNum(this.start()), e = dayNum(this.end());
    if (s === null || e === null || e < s) return [];
    const out: DailyStat[] = [];
    for (let d = s; d <= e; d++) {
      let count = 0, created = 0, lifted = 0;
      for (const r of this.rows()) {
        const br = dayNum(r.date_br);
        if (br === null) continue;
        const pf = r.date_pod_fact ? dayNum(r.date_pod_fact) : null;
        if (br <= d && (pf === null || pf >= d)) count++;
        if (br === d) created++;
        if (pf === d) lifted++;
      }
      out.push({ date: dayIso(d), count, created, lifted });
    }
    return out;
  }

  // ── Экспорт Excel ───────────────────────────────────────────────────────────
  async exportExcel(): Promise<void> {
    if (this.rows().length === 0) return;
    try {
      const XLSX = await import('xlsx-js-style');
      const wb = XLSX.utils.book_new();

      const data = this.rows().map((r) => ({
        'Индекс': r.index_1, 'Станция': r.station_br, 'Дорога': r.doroga_br,
        'Дата броса': this.fmtDate(r.date_br), 'Дата подъёма': r.date_pod_fact ? this.fmtDate(r.date_pod_fact) : '',
        'Код': r.reason, 'Суток': r.days_total,
        'из них к.05': r.days_code05, '01 согл.': r.days_code01_agreed,
        '01 несогл.': r.days_code01_notagreed, 'прочие': r.days_other,
        'Вагонов': r.vagon_count, 'Состав': r.sostav, 'История планов': r.plan_history,
      }));
      XLSX.utils.book_append_sheet(wb, XLSX.utils.json_to_sheet(data), 'Брошенные');

      const daily = this.dailyStats().map((d) => ({
        'Дата': this.fmtDate(d.date), 'Наличие': d.count, 'Брошено': d.created, 'Поднято': d.lifted,
      }));
      daily.push({ 'Дата': 'ИТОГО', 'Наличие': this.totalDays(), 'Брошено': this.totalCreated(), 'Поднято': this.totalLifted() });
      XLSX.utils.book_append_sheet(wb, XLSX.utils.json_to_sheet(daily), 'Динамика по дням');

      const analysis = [
        { 'Показатель': 'Всего поездо-суток', 'Значение': this.totalDays() },
        { 'Показатель': 'Код 05 (заявка)', 'Значение': this.daysCode05() },
        { 'Показатель': 'Код 01 согласованный (письмо)', 'Значение': this.daysAgreed() },
        { 'Показатель': 'Код 01 несогласованный (РЖД)', 'Значение': this.daysNotAgreed() },
        { 'Показатель': 'Прочие / без журнала', 'Значение': this.daysOther() },
        { 'Показатель': 'Согласованный простой, %', 'Значение': this.protectedPct() },
      ];
      XLSX.utils.book_append_sheet(wb, XLSX.utils.json_to_sheet(analysis), 'Анализ причин');

      const label = this.terminal() || 'все';
      XLSX.writeFile(wb, `Брошенные_${label}_${this.start()}—${this.end()}.xlsx`);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    }
  }

  // ── PNG / MAX ────────────────────────────────────────────────────────────────
  private async png(): Promise<Blob> {
    const el = this.host.nativeElement.querySelector('#bros-report-tbl') as HTMLElement | null;
    if (!el) throw new Error('таблица не найдена');
    const blob = await toBlob(el, { pixelRatio: 2, backgroundColor: '#ffffff' });
    if (!blob) throw new Error('не удалось отрисовать картинку');
    return blob;
  }

  async exportPng(): Promise<void> {
    try {
      const blob = await this.png();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `Брошенные_${todayMsk()}.png`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    }
  }

  async sendToMax(): Promise<void> {
    this.sending.set(true);
    try {
      const blob = await this.png();
      const res = await this.ref.sendImage('oper', '', blob, `Брошенные_${todayMsk()}.png`,
        `Брошенные поезда на ${this.fmtDate(todayMsk())}`);
      if (res.chats === 0) {
        this.msg.warning('Нет настроенного маршрута рассылки (форма «oper»)');
      } else if (Object.keys(res.failed).length) {
        this.msg.warning(`Отправлено в ${res.sent.length}, не ушло — ${Object.keys(res.failed).join(', ')}`);
      } else {
        this.msg.success(`Отправлено в чаты (${res.sent.join(', ')})`);
      }
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.sending.set(false);
    }
  }

  /** «2026-07-24T00:00:00» / «2026-07-24» → «24.07.26»; пусто → «—». */
  fmtDate(ts: string | null): string {
    if (!ts || ts.length < 10) return '—';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)}.${ts.slice(2, 4)}`;
  }
}

/** «YYYY-MM-DD…» → номер дня (дни от эпохи, полдень UTC — без сноса на границе). */
function dayNum(ts: string | null): number | null {
  if (!ts || ts.length < 10) return null;
  const t = Date.parse(ts.slice(0, 10) + 'T12:00:00Z');
  if (Number.isNaN(t)) return null;
  return Math.floor(t / 86400000);
}

/** Номер дня → «YYYY-MM-DD». */
function dayIso(n: number): string {
  return new Date(n * 86400000 + 43200000).toISOString().slice(0, 10);
}
