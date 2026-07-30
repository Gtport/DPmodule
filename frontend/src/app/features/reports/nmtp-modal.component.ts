import { Component, OnInit, computed, inject, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { DragDropModule } from '@angular/cdk/drag-drop';
import { toBlob } from 'html-to-image';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { NzSwitchModule } from 'ng-zorro-antd/switch';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { blobErrorMessage } from '../../shared/blob-error';
import { todayMsk } from '../../shared/msk-date';
import { ArrivalsApiService, TerminalTarget } from '../home/arrivals-api.service';
import { MaxApiService } from './max-api.service';
import { NmtpApiService, NmtpMode, NmtpReport, NmtpTrainRow } from './nmtp-api.service';

/** Группа шапки первого уровня: подпись клиента + сколько колонок накрывает. */
interface HeadGroup {
  label: string;
  span: number;
}

/**
 * Перемещаемая модалка «Подход вагонов {терминал}» по форме порта (НМТП) —
 * экранное представление той же формы, что серверная книга .xlsx: шапка в три
 * уровня (клиент / станции погрузки / марка), секции по станциям терминалов и
 * дорогам, «БРОШЕННЫЕ ПОЕЗДА» (в колонке прибытия — дата бросания), подвал
 * с вагонами/тоннажом. Цвета повторяют книгу: жёлтая колонка «итого», серые
 * секции, голубые брошенные, оранжевое «прочее».
 *
 * Переключатель «скрыть перестановки» — режим gtport UseNaznachOnly (строго по
 * назначению, без поездов «НА {сосед}»; актуален для терминалов одной станции —
 * АЭ/ГУТ-2). «Ожид. прибытие» — только плановым поездам, время владивостокское
 * (+7 к МСК — как в книге, порт живёт в местном времени). «В MAX» — картинкой
 * по маршруту формы `nmtp` (миграция 000054), Excel — серверный .xlsx в том же
 * режиме, что на экране.
 */
@Component({
  selector: 'app-nmtp-modal',
  imports: [
    FormsModule, DragDropModule, NzButtonModule, NzIconModule, NzModalModule,
    NzSpinModule, NzSwitchModule, NzTooltipModule,
  ],
  template: `
    <nz-modal [nzVisible]="true" [nzTitle]="ttl" [nzFooter]="null" nzWidth="1500px"
              [nzMask]="false" (nzOnCancel)="closed.emit()">
      <ng-template #ttl>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          <span class="tbadge" [style.background]="terminalColor()">{{ terminal() }}</span>
          Подход вагонов (форма порта)
          <span class="sub">поездов: {{ report()?.trains_active ?? 0 }}</span>
        </div>
      </ng-template>

      <ng-container *nzModalContent>
        <div class="filters">
          <nz-switch nzSize="small" [ngModel]="naznachOnly()" (ngModelChange)="setMode($event)"></nz-switch>
          <span class="mode" nz-tooltip
                nzTooltipTitle="Строго по назначению: без поездов, переставляемых на соседний терминал (режим gtport «по назначению»)">
            скрыть перестановки
          </span>
          <button nz-button nzType="primary" nzSize="small" [nzLoading]="loading()" (click)="load()">
            <span nz-icon nzType="reload"></span> Обновить
          </button>
          <span class="spacer"></span>
          <button nz-button nzSize="small" [nzLoading]="sending()" (click)="sendToMax()" [disabled]="!report()"
                  nz-tooltip nzTooltipTitle="Отправить картинкой в чаты MAX">
            <span nz-icon nzType="send"></span> В MAX
          </button>
          <button nz-button nzType="text" nzSize="small" (click)="exportPng()" [disabled]="!report()"
                  nz-tooltip nzTooltipTitle="Сохранить как картинку">
            <span nz-icon nzType="camera"></span>
          </button>
          <button nz-button nzType="text" nzSize="small" [nzLoading]="downloading()" (click)="exportExcel()"
                  [disabled]="!report()" nz-tooltip nzTooltipTitle="Скачать книгу .xlsx (собирает сервер)">
            <span nz-icon nzType="download"></span>
          </button>
        </div>

        @if (loading()) {
          <div class="center"><nz-spin nzSimple></nz-spin></div>
        } @else if (report(); as r) {
          <div class="dp-tbl-wrap" style="max-height: 70vh" id="nmtp-tbl">
            <table class="dp-tbl nmtp">
              <thead>
                <tr>
                  <th rowspan="3" class="c-idx">ПОЕЗД</th>
                  <th rowspan="3">СТАНЦИЯ</th>
                  <th rowspan="3" class="c-d">ДАТА<br /><small>(принято к перевозке)</small></th>
                  <th rowspan="3" class="c-note">ПРИМЕЧАНИЕ</th>
                  <th rowspan="3" class="c-vag">ВАГОН<br /><small>(для контроля)</small></th>
                  <th rowspan="3" class="c-d">ожид. дата приб.</th>
                  <th rowspan="3" class="c-t">ожид. время приб. <small>(влад.)</small></th>
                  @for (g of headGroups(); track $index) {
                    <th [attr.colspan]="g.span" class="grp-l">{{ g.label }}</th>
                  }
                  @if (r.has_other) { <th rowspan="3" class="other grp-l">ПРОЧЕЕ</th> }
                  <th rowspan="3" class="itogo grp-l">итого</th>
                </tr>
                <tr>
                  @for (c of r.columns; track $index) {
                    <th class="st" [class.grp-l]="groupStart()[$index]">{{ c.station }}</th>
                  }
                </tr>
                <tr>
                  @for (c of r.columns; track $index) {
                    <th class="mark" [class.grp-l]="groupStart()[$index]">{{ c.mark }}</th>
                  }
                </tr>
              </thead>
              <tbody>
                @for (s of r.sections; track s.label) {
                  <tr class="section">
                    <td [attr.colspan]="fixedCols + cargoCols()">{{ s.label }}</td>
                    <td class="c">{{ s.total || '' }}</td>
                  </tr>
                  @for (row of s.rows; track $index) {
                    <tr>
                      <td class="c">{{ row.index }}</td>
                      <td class="c">{{ row.station_oper }}</td>
                      <td class="c">{{ fmtDate(row.date_nach) }}</td>
                      <td class="c">{{ row.note }}</td>
                      <td class="c num">{{ row.control_vagon }}</td>
                      <td class="c">{{ progDate(row) }}</td>
                      <td class="c">{{ progTime(row) }}</td>
                      @for (c of r.columns; track $index; let ci = $index) {
                        <td class="c num cnt" [class.grp-l]="groupStart()[ci]">{{ row.counts[ci] || '' }}</td>
                      }
                      @if (r.has_other) {
                        <td class="c num cnt grp-l" [class.other]="row.counts[r.columns.length]">
                          {{ row.counts[r.columns.length] || '' }}
                        </td>
                      }
                      <td class="c num cnt itogo grp-l">{{ row.total }}</td>
                    </tr>
                  }
                }
                <tr class="counter">
                  <td [attr.colspan]="fixedCols + cargoCols()">кол-во поездов в движении (составы от 20 ваг.):</td>
                  <td class="c itogo">{{ r.trains_active }}</td>
                </tr>

                <tr class="banner"><td [attr.colspan]="fixedCols + cargoCols() + 1">
                  БРОШЕННЫЕ ПОЕЗДА (в колонке прибытия — дата бросания)
                </td></tr>
                @for (s of abandonedNonEmpty(); track s.label) {
                  <tr class="section ab">
                    <td [attr.colspan]="fixedCols + cargoCols()">{{ s.label }}</td>
                    <td class="c">{{ s.total || '' }}</td>
                  </tr>
                  @for (row of s.rows; track $index) {
                    <tr>
                      <td class="c">{{ row.index }}</td>
                      <td class="c">{{ row.station_oper }}</td>
                      <td class="c">{{ fmtDate(row.date_nach) }}</td>
                      <td class="c">{{ row.note }}</td>
                      <td class="c num">{{ row.control_vagon }}</td>
                      <td class="c">{{ fmtDate(row.date_bros ?? null) }}</td>
                      <td class="c"></td>
                      @for (c of r.columns; track $index; let ci = $index) {
                        <td class="c num cnt" [class.grp-l]="groupStart()[ci]">{{ row.counts[ci] || '' }}</td>
                      }
                      @if (r.has_other) {
                        <td class="c num cnt grp-l" [class.other]="row.counts[r.columns.length]">
                          {{ row.counts[r.columns.length] || '' }}
                        </td>
                      }
                      <td class="c num cnt itogo grp-l">{{ row.total }}</td>
                    </tr>
                  }
                }
                <tr class="counter">
                  <td [attr.colspan]="fixedCols + cargoCols()">кол-во брошенных поездов (составы от 20 ваг.):</td>
                  <td class="c itogo">{{ r.trains_abandoned }}</td>
                </tr>

                <tr class="foot">
                  <td [attr.colspan]="fixedCols">ВСЕГО вагонов</td>
                  @for (c of r.columns; track $index; let ci = $index) {
                    <td class="c num cnt" [class.grp-l]="groupStart()[ci]">{{ r.col_counts[ci] || '' }}</td>
                  }
                  @if (r.has_other) {
                    <td class="c num cnt grp-l">{{ r.col_counts[r.columns.length] || '' }}</td>
                  }
                  <td class="c num cnt itogo grp-l">{{ r.total_vagons }}</td>
                </tr>
                <tr class="foot">
                  <td [attr.colspan]="fixedCols">тоннаж (тыс. т.)</td>
                  @for (c of r.columns; track $index; let ci = $index) {
                    <td class="c num" [class.grp-l]="groupStart()[ci]">{{ fmtTons(r.col_tons[ci]) }}</td>
                  }
                  @if (r.has_other) {
                    <td class="c num grp-l">{{ fmtTons(r.col_tons[r.columns.length]) }}</td>
                  }
                  <td class="c num itogo grp-l">{{ fmtTons(r.total_tons) }}</td>
                </tr>
              </tbody>
            </table>

            <div class="below">
              <div>Прогноз выгрузки по подходу (ваг/сут): <b>{{ r.unload_forecast.toFixed(1) }}</b></div>
              @if (r.norm > 0) {
                <div>Нагрузка на ж/д сеть: загрузка <b>{{ (r.total_vagons / 1000).toFixed(3) }}</b> тыс. ваг ·
                  норма <b>{{ r.norm }}</b> · ниже нормы на
                  <b>{{ ((1 - r.total_vagons / r.norm) * 100).toFixed(1) }}%</b></div>
              }
              @if (r.client_tons.length) {
                <div>Тоннаж по клиентам (тыс. т.):
                  @for (ct of r.client_tons; track ct.client; let last = $last) {
                    {{ ct.client }} — <b>{{ fmtTons(ct.tons) }}</b>@if (!last) { · }
                  }
                </div>
              }
            </div>
          </div>
        }
      </ng-container>
    </nz-modal>
  `,
  styles: [`
    .ttl { cursor: move; user-select: none; display: flex; align-items: center; gap: var(--space-sm); }
    .ttl .sub { color: var(--color-text-muted); font-weight: 400; font-size: var(--font-size-sm); }
    .tbadge { padding: 0 8px; border-radius: var(--radius-sm); border: 1px solid var(--color-border); font-size: var(--font-size-sm); }
    .filters { display: flex; align-items: center; gap: var(--space-sm); margin-bottom: var(--space-sm); flex-wrap: wrap; }
    .mode { font-size: var(--font-size-sm); color: var(--color-text-secondary); cursor: default; }
    .spacer { flex: 1 1 auto; }
    .center { display: flex; justify-content: center; padding: var(--space-lg); }
    .c { text-align: center; white-space: nowrap; }
    .num { font-variant-numeric: tabular-nums; }
    .cnt { font-weight: 600; }
    .nmtp th { text-align: center; vertical-align: middle; }
    .nmtp th small { font-weight: 400; }
    .nmtp .st { max-width: 90px; white-space: normal; font-size: 11px; }
    .nmtp .mark { background: #e4e4e4; font-size: 11px; }
    /* Утолщённый стык групп клиентов — как в книге. */
    .nmtp .grp-l { border-left: 2px solid var(--color-text, #000); }
    .nmtp .itogo { background: #ffffcc; }
    .nmtp .other { background: #ffe7ba; }
    .nmtp .section td { background: #e4e4e4; font-weight: 600; text-align: center;
      border-top: 2px solid var(--color-text, #000); border-bottom: 2px solid var(--color-text, #000); }
    .nmtp .section.ab td { background: #abe9ff; }
    .nmtp .banner td { font-weight: 600; text-align: center; }
    .nmtp .counter td { text-align: right; font-weight: 600; }
    .nmtp .counter td.itogo { text-align: center; }
    .nmtp .foot td { font-weight: 600; text-align: center; }
    .c-idx { min-width: 110px; } .c-d { width: 74px; } .c-t { width: 70px; }
    .c-note { width: 96px; } .c-vag { width: 84px; }
    .below { padding: var(--space-sm) 2px; display: flex; flex-direction: column; gap: 2px;
      font-size: var(--font-size-sm); background: #fff; }
  `],
})
export class NmtpModalComponent implements OnInit {
  private readonly api = inject(NmtpApiService);
  private readonly arrivals = inject(ArrivalsApiService);
  private readonly max = inject(MaxApiService);
  private readonly msg = inject(NzMessageService);

  /** Терминал (ports.name_s), обязателен. */
  readonly terminal = input.required<string>();
  readonly closed = output<void>();

  readonly loading = signal(false);
  readonly sending = signal(false);
  readonly downloading = signal(false);
  readonly naznachOnly = signal(false);
  readonly report = signal<NmtpReport | null>(null);
  private readonly terminals = signal<TerminalTarget[]>([]);

  /** Фиксированные колонки слева (как в книге). */
  readonly fixedCols = 7;

  /** Колонок в матрице груза (с «прочим»). */
  readonly cargoCols = computed(() => {
    const r = this.report();
    return r ? r.columns.length + (r.has_other ? 1 : 0) : 0;
  });

  /** Группы клиентов первого уровня шапки (merge одинаковых подряд). */
  readonly headGroups = computed<HeadGroup[]>(() => {
    const cols = this.report()?.columns ?? [];
    const out: HeadGroup[] = [];
    for (const c of cols) {
      const last = out[out.length - 1];
      if (last && last.label === c.group) last.span++;
      else out.push({ label: c.group, span: 1 });
    }
    return out;
  });

  /** Первая колонка каждой группы — утолщённая левая граница. */
  readonly groupStart = computed<boolean[]>(() => {
    const cols = this.report()?.columns ?? [];
    return cols.map((c, i) => i === 0 || c.group !== cols[i - 1].group);
  });

  /** Пустые секции брошенных не показываем (как в книге). */
  readonly abandonedNonEmpty = computed(() =>
    (this.report()?.abandoned ?? []).filter((s) => s.rows.length > 0));

  readonly terminalColor = computed(() =>
    this.terminals().find((t) => t.name === this.terminal())?.color || 'transparent');

  ngOnInit(): void {
    void this.loadTerminals();
    void this.load();
  }

  private async loadTerminals(): Promise<void> {
    try {
      this.terminals.set(await this.arrivals.getTerminals());
    } catch { /* реестр нужен только для подкраски — не критичен */ }
  }

  private mode(): NmtpMode {
    return this.naznachOnly() ? 'naznach' : '';
  }

  setMode(v: boolean): void {
    this.naznachOnly.set(v);
    void this.load();
  }

  async load(): Promise<void> {
    this.loading.set(true);
    try {
      this.report.set(await this.api.report(this.terminal(), this.mode()));
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
      this.report.set(null);
    } finally {
      this.loading.set(false);
    }
  }

  /** «2026-07-24…» → «24.07.26». */
  fmtDate(ts: string | null): string {
    if (!ts || ts.length < 10) return '';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)}.${ts.slice(2, 4)}`;
  }

  /** Тоннаж: 3 знака, ноль — пусто. */
  fmtTons(v: number): string {
    return v > 0 ? v.toFixed(3) : '';
  }

  /**
   * Ожидаемое прибытие — только плановым поездам (правило владельца
   * 30.07.2026), время владивостокское: +7 ч к московскому, как в книге.
   */
  private progVlad(row: NmtpTrainRow): Date | null {
    if (!row.planned || !row.prog) return null;
    const d = new Date(row.prog);
    return isNaN(d.getTime()) ? null : new Date(d.getTime() + 7 * 3600_000);
  }

  progDate(row: NmtpTrainRow): string {
    const d = this.progVlad(row);
    if (!d) return '';
    const p = (n: number) => String(n).padStart(2, '0');
    return `${p(d.getDate())}.${p(d.getMonth() + 1)}.${String(d.getFullYear()).slice(2)}`;
  }

  progTime(row: NmtpTrainRow): string {
    const d = this.progVlad(row);
    if (!d) return '';
    const p = (n: number) => String(n).padStart(2, '0');
    return `${p(d.getHours())}:${p(d.getMinutes())}`;
  }

  // ── PNG / MAX / Excel ──────────────────────────────────────────────────────
  private async png(): Promise<Blob> {
    // Содержимое модалки живёт в overlay-портале CDK, вне host — ищем по документу.
    const el = document.querySelector('#nmtp-tbl') as HTMLElement | null;
    if (!el) throw new Error('таблица не найдена');
    const maxH = el.style.maxHeight;
    el.style.maxHeight = 'none';
    try {
      const blob = await toBlob(el, { pixelRatio: 2, backgroundColor: '#ffffff' });
      if (!blob) throw new Error('не удалось отрисовать картинку');
      return blob;
    } finally {
      el.style.maxHeight = maxH;
    }
  }

  async exportPng(): Promise<void> {
    try {
      const blob = await this.png();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `Подход вагонов ${this.terminal()} ${todayMsk()}.png`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    }
  }

  /** Картинка по маршруту формы nmtp (чат терминала, миграция 000054). */
  async sendToMax(): Promise<void> {
    this.sending.set(true);
    try {
      const blob = await this.png();
      const caption = `Подход вагонов ${this.terminal()} на ${todayMsk()}`;
      const res = await this.max.sendImage('nmtp', this.terminal(), blob,
        `Подход вагонов ${this.terminal()} ${todayMsk()}.png`, caption);
      if (res.chats === 0) {
        this.msg.warning('Нет настроенного маршрута рассылки (форма «nmtp»)');
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

  /** Книга .xlsx с сервера — в текущем режиме отбора. */
  async exportExcel(): Promise<void> {
    this.downloading.set(true);
    try {
      const blob = await this.api.excel(this.terminal(), this.mode());
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `Подход вагонов ${this.terminal()} ${todayMsk()}.xlsx`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      this.msg.error(await blobErrorMessage(err));
    } finally {
      this.downloading.set(false);
    }
  }
}
