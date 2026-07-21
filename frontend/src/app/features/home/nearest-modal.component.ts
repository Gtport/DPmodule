import { Component, OnInit, computed, inject, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { DragDropModule } from '@angular/cdk/drag-drop';
import { NzRadioModule } from 'ng-zorro-antd/radio';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzDropDownModule, NzContextMenuService, NzDropdownMenuComponent } from 'ng-zorro-antd/dropdown';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { TerminalTarget } from './arrivals-api.service';
import { NearestApiService, NearestTrain, NearestVagon } from './nearest-api.service';

/**
 * Перемещаемая модалка «Ближайшие поезда — <станция>» (перенос gtport Nearest):
 * Время (план — зелёным / прогноз) · Индекс · Станция (дорога, остаток км;
 * красным — брошенные) · составы по терминалам станции. ПКМ по поезду:
 * «Натурный лист» (печать; Собственник — наше поле owner, в gtport был пуст),
 * «СМС» (телеграмма: по назначению / перестановка; копировать/печать),
 * «Экспорт поезда в Excel». В тулбаре — печать всей таблицы и обновление.
 */
@Component({
  selector: 'app-nearest-modal',
  imports: [
    FormsModule, DragDropModule, NzRadioModule, NzButtonModule, NzIconModule,
    NzModalModule, NzSpinModule, NzTooltipModule, NzDropDownModule,
  ],
  template: `
    <nz-modal [nzVisible]="true" [nzTitle]="title" [nzFooter]="null" nzWidth="1100px"
              [nzMask]="false" (nzOnCancel)="closed.emit()">
      <ng-template #title>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          Ближайшие поезда — {{ station() }}
        </div>
      </ng-template>
      <ng-container *nzModalContent>
        <div class="bar">
          <b>Подход {{ terminalNames().join('+') }}</b>
          <span class="mut">поездов: {{ shown().length }}</span>
          <nz-radio-group [ngModel]="mode()" (ngModelChange)="mode.set($event)" nzSize="small">
            <label nz-radio-button nzValue="plan">Только план</label>
            <label nz-radio-button nzValue="all">Весь прогноз</label>
          </nz-radio-group>
          <span class="spacer"></span>
          <button nz-button nzType="text" nzSize="small" nz-tooltip nzTooltipTitle="Печать таблицы"
                  (click)="printTable()">
            <span nz-icon nzType="printer"></span>
          </button>
          <button nz-button nzType="text" nzSize="small" nz-tooltip nzTooltipTitle="Обновить" (click)="load()">
            <span nz-icon nzType="sync"></span>
          </button>
        </div>

        <nz-spin [nzSpinning]="loading()">
          <div class="tbl-wrap">
            <table class="tbl">
              <thead>
                <tr>
                  <th class="c-dt">Прибытие</th>
                  <th class="c-idx">Индекс</th>
                  <th class="c-st">Станция</th>
                  @for (t of terminals(); track t.name) { <th>{{ t.name }}</th> }
                </tr>
              </thead>
              <tbody>
                @for (t of shown(); track t.key) {
                  <tr (contextmenu)="openMenu($event, t, menu)">
                    <td class="c" [class.plan]="t.has_plan"
                        [title]="t.has_plan ? 'по нитке плана' : 'прогноз/расчёт'">{{ fmtDT(t.time_jd) }}</td>
                    <td class="c num">{{ t.index || '—' }}</td>
                    <td [class.danger]="t.broshen">
                      {{ t.station_oper }}@if (t.doroga_oper) { <span class="mut">({{ t.doroga_oper }})</span> }
                      @if (t.rasst != null) { <span class="mut">{{ t.rasst }} км</span> }
                      @if (t.broshen) { <b> БРОШЕН</b> }
                    </td>
                    @for (term of terminals(); track term.name) {
                      <td class="c-term">
                        @for (sg of subsFor(t, term.name); track sg.key) {
                          <div class="sg">{{ sg.display }}</div>
                        } @empty { <span class="mut">—</span> }
                      </td>
                    }
                  </tr>
                } @empty {
                  <tr><td [attr.colspan]="3 + terminals().length" class="empty">Нет поездов в подходе</td></tr>
                }
              </tbody>
            </table>
          </div>
        </nz-spin>
        <p class="hint">ПКМ по поезду — натурный лист, СМС, экспорт в Excel.</p>
      </ng-container>
    </nz-modal>

    <nz-dropdown-menu #menu="nzDropdownMenu">
      <ul nz-menu>
        <li nz-menu-item (click)="openNatur()">Натурный лист</li>
        <li nz-menu-item (click)="openSms()">СМС</li>
        <li nz-menu-item (click)="exportTrain()">Экспорт поезда в Excel</li>
      </ul>
    </nz-dropdown-menu>

    <!-- Натурный лист поезда -->
    @if (naturOpen()) {
      <nz-modal [nzVisible]="true" [nzTitle]="naturTtl" nzWidth="860px" [nzMask]="false"
                (nzOnCancel)="naturOpen.set(false)" nzOkText="Распечатать" (nzOnOk)="printNatur()">
        <ng-template #naturTtl>
          <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
            Натурный лист поезда {{ ctx()?.index }} ({{ fmtDT(ctx()?.time_jd ?? null) }})
          </div>
        </ng-template>
        <ng-container *nzModalContent>
          <div class="tbl-wrap natur">
            <table class="tbl">
              <thead>
                <tr><th>№</th><th>Вагон</th><th>Накладная</th><th>Состав</th><th>Груз</th><th>Собственник</th></tr>
              </thead>
              <tbody>
                @for (v of naturVagons(); track v.id) {
                  <tr>
                    <td class="c num">{{ v.npp_vag ?? '—' }}</td>
                    <td class="c num">{{ v.vagon }}</td>
                    <td class="c num">{{ v.invoice || '—' }}</td>
                    <td>{{ subDisplay(v) }}</td>
                    <td>{{ cargo(v) }}</td>
                    <td>{{ v.owner || '—' }}</td>
                  </tr>
                }
              </tbody>
            </table>
          </div>
        </ng-container>
      </nz-modal>
    }

    <!-- СМС-телеграмма -->
    @if (smsOpen()) {
      <nz-modal [nzVisible]="true" [nzTitle]="smsTtl" nzWidth="560px" [nzMask]="false"
                (nzOnCancel)="smsOpen.set(false)" [nzFooter]="smsFoot">
        <ng-template #smsTtl>
          <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
            СМС — поезд {{ ctx()?.index }}
          </div>
        </ng-template>
        <ng-container *nzModalContent>
          <pre class="sms">{{ smsText() }}</pre>
        </ng-container>
        <ng-template #smsFoot>
          <button nz-button (click)="copySms()"><span nz-icon nzType="copy"></span> Скопировать</button>
          <button nz-button nzType="primary" (click)="printSms()"><span nz-icon nzType="printer"></span> Распечатать</button>
        </ng-template>
      </nz-modal>
    }
  `,
  styles: [`
    .ttl { cursor: move; user-select: none; }
    .bar { display: flex; align-items: center; gap: var(--space-sm); margin-bottom: var(--space-sm); }
    .spacer { flex: 1 1 auto; }
    .mut { color: var(--color-text-secondary); font-size: var(--font-size-sm); }
    .tbl-wrap { max-height: 62vh; overflow: auto; }
    .natur { max-height: 56vh; }
    .tbl { width: 100%; border-collapse: collapse; font-size: var(--font-size-sm); }
    .tbl th { position: sticky; top: 0; background: var(--color-bg-subtle); font-weight: 600;
              padding: 4px 8px; border: 1px solid var(--color-border-light); text-align: center; z-index: 1; }
    .tbl td { padding: 3px 8px; border: 1px solid var(--color-border-light); vertical-align: top; }
    .c, .c-dt, .c-idx { text-align: center; white-space: nowrap; }
    .num { font-variant-numeric: tabular-nums; }
    .plan { color: var(--color-success); font-weight: 600; }
    .danger { color: var(--color-danger); }
    .sg { white-space: nowrap; }
    .empty { text-align: center; color: var(--color-text-secondary); padding: var(--space-md); }
    .hint { margin: var(--space-xs) 0 0; color: var(--color-text-muted); font-size: var(--font-size-sm); }
    .sms { white-space: pre-wrap; font-family: inherit; margin: 0; font-size: var(--font-size-base); }
  `],
})
export class NearestModalComponent implements OnInit {
  private readonly api = inject(NearestApiService);
  private readonly msg = inject(NzMessageService);
  private readonly ctxMenu = inject(NzContextMenuService);

  readonly station = input.required<string>();
  readonly terminals = input.required<TerminalTarget[]>();
  readonly closed = output<void>();

  readonly loading = signal(false);
  readonly trains = signal<NearestTrain[]>([]);
  /** Режим показа: только плановые нитки (дефолт) либо весь прогноз. */
  readonly mode = signal<'plan' | 'all'>('plan');
  readonly shown = computed(() =>
    this.mode() === 'plan' ? this.trains().filter((t) => t.has_plan) : this.trains());
  /** Поезд под курсором ПКМ — цель действий. */
  readonly ctx = signal<NearestTrain | null>(null);
  readonly naturOpen = signal(false);
  readonly smsOpen = signal(false);

  readonly terminalNames = computed(() => this.terminals().map((t) => t.name));

  ngOnInit(): void {
    void this.load();
  }

  async load(): Promise<void> {
    this.loading.set(true);
    try {
      this.trains.set(await this.api.getNearest(this.terminalNames()));
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.loading.set(false);
    }
  }

  subsFor(t: NearestTrain, naznach: string) {
    return t.sub_groups.filter((sg) => sg.naznach === naznach);
  }

  openMenu(ev: MouseEvent, t: NearestTrain, menu: NzDropdownMenuComponent): void {
    ev.preventDefault();
    this.ctx.set(t);
    this.ctxMenu.create(ev, menu);
  }

  // ── Натурный лист ────────────────────────────────────────────────────────
  openNatur(): void {
    if (this.ctx()) this.naturOpen.set(true);
  }

  /** Вагоны поезда по порядку номера в составе (натурный лист). */
  readonly naturVagons = computed<NearestVagon[]>(() => {
    const t = this.ctx();
    if (!t) return [];
    return t.sub_groups.flatMap((sg) => sg.vagons);
  });

  subDisplay(v: NearestVagon): string {
    const t = this.ctx();
    const sg = t?.sub_groups.find((s) => s.vagons.some((x) => x.id === v.id));
    return sg?.display ?? '—';
  }

  cargo(v: NearestVagon): string {
    const parts = [v.cargo_s || '—'];
    if (v.ves != null) parts.push(`${v.ves} тн`);
    return parts.join(' ');
  }

  printNatur(): void {
    const t = this.ctx();
    if (!t) return;
    const rows = this.naturVagons().map((v) =>
      `<tr><td>${v.npp_vag ?? '—'}</td><td>${v.vagon}</td><td>${v.invoice || '—'}</td>` +
      `<td>${this.subDisplay(v)}</td><td>${this.cargo(v)}</td><td>${v.owner || '—'}</td></tr>`).join('');
    printHtml(`Натурный лист поезда ${t.index}`,
      `<h3>Натурный лист поезда ${t.index} (${this.fmtDT(t.time_jd)}) — ${this.station()}</h3>
       <table><thead><tr><th>№</th><th>Вагон</th><th>Накладная</th><th>Состав</th><th>Груз</th><th>Собственник</th></tr></thead>
       <tbody>${rows}</tbody></table>`);
  }

  // ── СМС ──────────────────────────────────────────────────────────────────
  openSms(): void {
    if (this.ctx()) this.smsOpen.set(true);
  }

  /** Телеграмма (перенос смысла gtport generateSMSText): разбивка «по
   *  назначению» / «перестановка», номера вагонов и метка sms_1. */
  readonly smsText = computed(() => {
    const t = this.ctx();
    if (!t) return '';
    const own = t.sub_groups.filter((sg) => sg.naznach === sg.gruzpol_s);
    const moved = t.sub_groups.filter((sg) => sg.naznach !== sg.gruzpol_s);
    const lines: string[] = [
      `В поезде с индексом ${t.index || '—'} — ${t.vagon_count} ваг. на ${this.station()}:`,
    ];
    const block = (title: string, sgs: typeof own) => {
      if (!sgs.length) return;
      lines.push(title);
      for (const sg of sgs) {
        const dest = sg.naznach === sg.gruzpol_s ? sg.naznach : `${sg.gruzpol_s} → ${sg.naznach}`;
        const sms = sg.sms_1 ? ` ${sg.sms_1}` : '';
        lines.push(`  ${sg.vagon_count} ваг. ${dest}${sms}: ${sg.vagons.map((v) => v.vagon).join(', ')}`);
      }
    };
    block('по назначению:', own);
    block('перестановка:', moved);
    return lines.join('\n');
  });

  async copySms(): Promise<void> {
    await navigator.clipboard.writeText(this.smsText());
    this.msg.success('Текст скопирован.');
  }

  printSms(): void {
    printHtml(`СМС — поезд ${this.ctx()?.index ?? ''}`, `<pre>${this.smsText()}</pre>`);
  }

  // ── Excel поезда ─────────────────────────────────────────────────────────
  async exportTrain(): Promise<void> {
    const t = this.ctx();
    if (!t) return;
    const XLSX = await import('xlsx-js-style');
    const wb = XLSX.utils.book_new();
    XLSX.utils.book_append_sheet(wb, XLSX.utils.json_to_sheet([{
      'Индекс': t.index, 'Прибытие': this.fmtDT(t.time_jd),
      'Источник времени': t.has_plan ? 'план' : 'прогноз/расчёт',
      'Станция операции': t.station_oper, 'Дорога': t.doroga_oper,
      'Дистанция, км': t.rasst ?? '', 'Всего вагонов': t.vagon_count,
      'Вес, т': Math.round(t.ves), 'Статус': t.broshen ? 'БРОШЕН' : 'ОК',
    }]), 'Инфо о поезде');
    const vagons = t.sub_groups.flatMap((sg) => sg.vagons.map((v) => ({
      '№': v.npp_vag ?? '', 'Вагон': v.vagon, 'Накладная': v.invoice,
      'Груз': v.cargo_s, 'Вес': v.ves ?? '', 'Состав': sg.display,
      'Назначение': v.naznach, 'Грузополучатель': v.gruzpol_s,
      'Собственник': v.owner || '—',
    })));
    XLSX.utils.book_append_sheet(wb, XLSX.utils.json_to_sheet(vagons), 'Вагоны');
    XLSX.writeFile(wb, `Поезд_${t.index || 'без_индекса'}.xlsx`);
  }

  // ── Печать всей таблицы ──────────────────────────────────────────────────
  printTable(): void {
    const head = ['Прибытие', 'Индекс', 'Станция', ...this.terminalNames()];
    const rows = this.shown().map((t) => {
      const cells = [
        this.fmtDT(t.time_jd) + (t.has_plan ? ' (план)' : ''),
        t.index || '—',
        `${t.station_oper} ${t.doroga_oper ? '(' + t.doroga_oper + ')' : ''} ${t.rasst != null ? t.rasst + ' км' : ''}${t.broshen ? ' БРОШЕН' : ''}`,
        ...this.terminalNames().map((n) => this.subsFor(t, n).map((sg) => sg.display).join('; ') || '—'),
      ];
      return `<tr${t.broshen ? ' class="danger"' : ''}>${cells.map((c) => `<td>${c}</td>`).join('')}</tr>`;
    }).join('');
    printHtml(`Ближайшие поезда — ${this.station()}`,
      `<h3>Ближайшие поезда — ${this.station()}</h3>
       <table><thead><tr>${head.map((h) => `<th>${h}</th>`).join('')}</tr></thead><tbody>${rows}</tbody></table>`);
  }

  /** дд.мм.гг чч:мм */
  fmtDT(ts: string | null): string {
    if (!ts || ts.length < 16) return '—';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)}.${ts.slice(2, 4)} ${ts.slice(11, 16)}`;
  }
}

/** Печать через отдельное окно (надёжно в SPA; стили — минимальные печатные). */
function printHtml(title: string, body: string): void {
  const w = window.open('', '_blank', 'width=900,height=700');
  if (!w) return;
  w.document.write(`<!doctype html><html><head><meta charset="utf-8"><title>${title}</title>
    <style>
      body { font-family: Arial, sans-serif; font-size: 12px; margin: 16px; }
      table { border-collapse: collapse; width: 100%; }
      th, td { border: 1px solid #999; padding: 3px 6px; text-align: left; }
      th { background: #eee; }
      tr.danger td { color: #d32f2f; font-weight: bold; }
      pre { font-family: Arial, sans-serif; font-size: 14px; white-space: pre-wrap; }
    </style></head><body>${body}</body></html>`);
  w.document.close();
  w.focus();
  w.print();
}
