import { Component, OnInit, inject, input, output, signal } from '@angular/core';
import { DragDropModule } from '@angular/cdk/drag-drop';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { AuthService } from '../../core/auth/auth.service';
import { TrailDelay, TrailOp, TrailVisit, VagonTrail, VagonTrailApiService } from './vagon-trail-api.service';

/**
 * Ширины колонок выгрузки в единицах Excel — порядок как в строке экспорта:
 * Вагон, Станция, Код станции, Дорога, Время операции, Код операции, Операция,
 * Операция (кратко), Индекс поезда (значения задал владелец).
 */
const TRAIL_COL_WIDTHS = [12.3, 25, 8.4, 8.4, 17, 8, 44, 17, 17];

/**
 * Модалка «История движения вагона» (ПКМ по вагону в истории прибывших).
 *
 * Порядок (решение владельца): сначала показываем то, что уже сохранено в базе,
 * с ПЕРИОДОМ полученной истории (первая и последняя операция) — если период не
 * устраивает, кнопка «Обновить из АСУ» делает стандартный запрос 601 (дата
 * погрузки −1 … сегодня) и перезаписывает трейл. Если в базе пусто — запрос
 * уходит сразу, без лишнего клика.
 *
 * Строка = визит станции: первая и последняя операция (на станции их бывает
 * много, в первом приближении это не нужно), разворот показывает все. Коды
 * сматчены на бэке: станция → stations, операция → cargo_operations, индекс
 * нормализован. Экспорт в Excel — полный трейл.
 *
 * ПЕРЕИСПОЛЬЗОВАНИЕ (модалку планируется вешать и на другие таблицы вагонов).
 * Нужны два входа: `vagonId` — id РЕЙСА и `vagon` — номер для заголовка. id —
 * это `dislocation.id` = `vagon_history.id` (вагон/станция/дата, одна и та же
 * строка), поэтому пункт ПКМ добавляется в любую таблицу, где id уже есть:
 * «Ближайшие поезда» (`NearestVagon.id`) и «Перестановки» (`RearrVagonDTO.ID`)
 * — без правок бэкенда. «Пропавшие вагоны» id НЕ отдают (`MissingVagon`: только
 * номер вагона) — там сперва добавить `id` в DTO, иначе рейс не адресовать.
 * Строка истории заводится на ПЕРВОМ появлении вагона в снимке (applyHistory),
 * то есть существует и для вагонов в пути, не только для прибывших.
 */
@Component({
  selector: 'app-vagon-trail-modal',
  imports: [
    DragDropModule, NzButtonModule, NzIconModule, NzModalModule, NzSpinModule, NzTooltipModule,
  ],
  template: `
    <nz-modal [nzVisible]="true" [nzTitle]="title" [nzFooter]="null" nzWidth="900px"
              [nzMask]="false" (nzOnCancel)="closed.emit()">
      <ng-template #title>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          История движения вагона — {{ vagon() }}
        </div>
      </ng-template>
      <ng-container *nzModalContent>
        <div class="bar">
          @if (trail(); as t) {
            @if (t.count) {
              <span>В базе: <b>{{ fmtDT(t.from) }}</b> — <b>{{ fmtDT(t.to) }}</b></span>
              <span class="mut">операций: {{ t.count }} · станций: {{ t.visits.length }}</span>
            } @else {
              <span class="mut">Истории в базе нет</span>
            }
            @if (t.delay_hours) {
              <span class="dsum" nz-tooltip nzTooltipTitle="Суммарные задержки (простои ≥ порога и бросания) за рейс">
                в рейсе {{ fmtDur(t.trip_hours) }}, из них стоял <b>{{ fmtDur(t.delay_hours) }}</b>
                ({{ t.delays.length }} эп.)
              </span>
            }
          }
          <span class="spacer"></span>
          @if (canEdit()) {
            <button nz-button nzSize="small" nzType="primary" [nzLoading]="pulling()" (click)="pull()"
                    nz-tooltip [nzTooltipTitle]="pullHint()">
              <span nz-icon nzType="cloud-download"></span> Обновить из АСУ
            </button>
          }
          <button nz-button nzType="text" nzSize="small" nz-tooltip nzTooltipTitle="Скачать историю (Excel)"
                  [disabled]="!trail()?.count" (click)="exportExcel()">
            <span nz-icon nzType="download"></span>
          </button>
        </div>

        <nz-spin [nzSpinning]="loading()">
          <div class="dp-tbl-wrap">
            <table class="dp-tbl">
              <thead>
                <tr>
                  <th class="c-st">Станция</th>
                  <th class="c-road">Дорога</th>
                  <th class="c-dt">Прибыл (первая оп.)</th>
                  <th class="c-op">Операция</th>
                  <th class="c-dt">Убыл (последняя оп.)</th>
                  <th class="c-op">Операция</th>
                  <th class="c-idx">Индекс</th>
                  <th class="c-cnt">Оп.</th>
                </tr>
              </thead>
              <tbody>
                @for (v of trail()?.visits ?? []; track $index; let i = $index) {
                  <tr class="visit" [class.delayed]="v.delay" (click)="toggle(i)">
                    <td class="c-st">
                      <span nz-icon [nzType]="isOpen(i) ? 'down' : 'right'" class="tw"></span>
                      {{ v.station || ('код ' + v.stan_op) }}
                      @if (v.delay; as d) {
                        <span class="dtag" [class.dtag5]="d.kind === 5"
                              nz-tooltip [nzTooltipTitle]="delayHint(d)">
                          {{ d.kind === 5 ? 'брошен' : 'простой' }} {{ fmtDur(d.hours) }}
                        </span>
                      }
                    </td>
                    <td class="c-road mut">{{ v.road || '—' }}</td>
                    <td class="c-dt">{{ fmtDT(v.first.date_op) }}</td>
                    <td class="c-op">{{ opName(v.first) }}</td>
                    <td class="c-dt">{{ v.count > 1 ? fmtDT(v.last.date_op) : '—' }}</td>
                    <td class="c-op">{{ v.count > 1 ? opName(v.last) : '—' }}</td>
                    <td class="c-idx num">{{ v.last.index }}</td>
                    <td class="c-cnt num">{{ v.count }}</td>
                  </tr>
                  @if (isOpen(i)) {
                    @for (o of v.ops; track $index) {
                      <tr class="op">
                        <td class="c-st"></td>
                        <td class="c-road"></td>
                        <td class="c-dt">{{ fmtDT(o.date_op) }}</td>
                        <td class="c-op" colspan="2">{{ o.oper || ('код ' + o.kop_vmd) }}</td>
                        <td class="c-op mut">{{ o.oper_s }}</td>
                        <td class="c-idx num">{{ o.index }}</td>
                        <td class="c-cnt"></td>
                      </tr>
                    }
                  }
                } @empty {
                  <tr><td colspan="8" class="empty">
                    @if (loading() || pulling()) { Загрузка… } @else { Истории движения нет }
                  </td></tr>
                }
              </tbody>
            </table>
          </div>
        </nz-spin>

        @if (unmatchedDelays().length) {
          <div class="dlist">
            <div class="dlist-title">Задержки вне сохранённого трейла:</div>
            @for (d of unmatchedDelays(); track $index) {
              <div>
                <span class="dtag" [class.dtag5]="d.kind === 5">
                  {{ d.kind === 5 ? 'брошен' : 'простой' }} {{ fmtDur(d.hours) }}
                </span>
                {{ d.station_name || ('код ' + d.station_code) }} ·
                {{ fmtDT(d.date_from) }} — {{ d.date_to ? fmtDT(d.date_to) : 'стоит сейчас' }}
              </div>
            }
          </div>
        }

        <p class="hint">Клик по станции — все операции на ней. Время — московское, как пришло от АСУ.</p>
      </ng-container>
    </nz-modal>
  `,
  styles: [`
    .ttl { cursor: move; user-select: none; }
    .bar { display: flex; align-items: center; gap: var(--space-sm); flex-wrap: wrap;
           margin-bottom: var(--space-sm); font-size: var(--font-size-sm); }
    .spacer { flex: 1 1 auto; }
    .mut { color: var(--color-text-muted); }
    .visit { cursor: pointer; }
    .visit:hover { background: var(--color-bg-hover); }
    .visit.delayed td { background: var(--color-warning-bg); }
    .dsum { color: var(--color-text-secondary); }
    .dtag { display: inline-block; margin-left: 6px; padding: 0 6px; border-radius: 10px;
            font-size: 11px; white-space: nowrap; background: var(--color-warning-bg);
            border: 1px solid var(--color-warning); }
    .dtag5 { color: var(--color-danger-text); border-color: var(--color-danger); }
    .dlist { margin-top: var(--space-sm); font-size: var(--font-size-sm); }
    .dlist-title { font-weight: 600; margin-bottom: 2px; }
    .dlist .dtag { margin-left: 0; margin-right: 4px; }
    .op td { background: var(--color-bg-subtle); }
    .tw { font-size: 10px; color: var(--color-text-muted); margin-right: 4px; }
    .c-dt, .c-idx, .c-cnt { text-align: center; white-space: nowrap; }
    .num { font-variant-numeric: tabular-nums; }
    .empty { text-align: center; color: var(--color-text-secondary); padding: var(--space-md); }
    .hint { margin: var(--space-xs) 0 0; color: var(--color-text-muted); font-size: var(--font-size-sm); }
  `],
})
export class VagonTrailModalComponent implements OnInit {
  private readonly api = inject(VagonTrailApiService);
  private readonly msg = inject(NzMessageService);
  /** Запрос 601 в АСУ (POST pull) — правка данных: порог operator. */
  readonly canEdit = inject(AuthService).canEdit;

  /** id рейса (строка vagon_history) и номер вагона для заголовка. */
  readonly vagonId = input.required<string>();
  readonly vagon = input.required<string>();
  readonly closed = output<void>();

  readonly loading = signal(false);
  readonly pulling = signal(false);
  readonly trail = signal<VagonTrail | null>(null);
  readonly open = signal<Set<number>>(new Set());

  ngOnInit(): void {
    void this.load();
  }

  /** Сначала база; пусто — сразу запрос в АСУ (лишний клик не нужен). */
  private async load(): Promise<void> {
    this.loading.set(true);
    try {
      const t = await this.api.get(this.vagonId());
      this.trail.set(t);
      // Пустой сохранённый трейл → автозапрос в АСУ, но только тому, кому
      // можно писать (клиенту бэкенд ответит 403 — не дёргаем зря).
      if (!t.count && this.canEdit()) await this.pull();
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.loading.set(false);
    }
  }

  /** «Обновить из АСУ»: отказ провайдера (частый по выбывшим) не стирает экран. */
  async pull(): Promise<void> {
    this.pulling.set(true);
    try {
      const t = await this.api.pull(this.vagonId());
      this.trail.set(t);
      this.open.set(new Set());
      this.msg.success(t.count ? `История обновлена: ${t.count} оп.` : 'АСУ вернула пустую историю.');
    } catch (err) {
      this.msg.error(`АСУ не отдала историю: ${apiErrorMessage(err)}`);
    } finally {
      this.pulling.set(false);
    }
  }

  pullHint(): string {
    const d = this.trail()?.date_nach;
    return d ? `Запрос с ${this.fmtD(this.shiftDay(d, -1))} по сегодня (дата погрузки −1)`
             : 'Запрос истории продвижения в АСУ';
  }

  toggle(i: number): void {
    const next = new Set(this.open());
    next.has(i) ? next.delete(i) : next.add(i);
    this.open.set(next);
  }

  isOpen(i: number): boolean {
    return this.open().has(i);
  }

  opName(o: TrailOp): string {
    return o.oper_s || o.oper || (o.kop_vmd ? 'код ' + o.kop_vmd : '—');
  }

  /** Часы → человеческая длительность: до суток — часы, дальше — сутки с десятыми. */
  fmtDur(hours: number): string {
    if (!hours || hours <= 0) return '—';
    if (hours < 24) return `${Math.round(hours * 10) / 10} ч`;
    return `${Math.round((hours / 24) * 10) / 10} сут`;
  }

  delayHint(d: TrailDelay): string {
    const kind = d.kind === 5 ? 'Брошен' : 'Долгий простой';
    const to = d.date_to ? this.fmtDT(d.date_to) : 'стоит сейчас';
    return `${kind}: ${this.fmtDT(d.date_from)} — ${to} (${this.fmtDur(d.hours)})`;
  }

  /** Эпизоды, не привязанные ни к одному визиту (трейла нет или он не покрывает стоянку). */
  unmatchedDelays(): TrailDelay[] {
    const t = this.trail();
    if (!t?.delays?.length) return [];
    const matched = new Set(
      t.visits.filter((v) => v.delay).map((v) => `${v.delay!.station_code}|${v.delay!.date_from}`),
    );
    return t.delays.filter((d) => !matched.has(`${d.station_code}|${d.date_from}`));
  }

  /** Экспорт — ПОЛНЫЙ трейл (свёртка нужна на экране, в файле удобнее всё). */
  async exportExcel(): Promise<void> {
    const t = this.trail();
    if (!t?.count) return;
    const XLSX = await import('xlsx-js-style');
    const rows = t.visits.flatMap((v: TrailVisit) => v.ops.map((o) => ({
      'Вагон': t.vagon,
      'Станция': v.station || '',
      'Код станции': v.stan_op,
      'Дорога': v.road,
      'Время операции': this.fmtDT(o.date_op),
      'Код операции': o.kop_vmd,
      'Операция': o.oper,
      'Операция (кратко)': o.oper_s,
      'Индекс поезда': o.index,
    })));
    const ws = XLSX.utils.json_to_sheet(rows);
    // Ширины — в единицах Excel (поле width, а не wch: wch добавляет отступ).
    ws['!cols'] = TRAIL_COL_WIDTHS.map((width) => ({ width }));
    // Выравнивание по центру: и заголовки, и данные.
    const range = XLSX.utils.decode_range(ws['!ref'] ?? 'A1');
    for (let r = range.s.r; r <= range.e.r; r++) {
      for (let c = range.s.c; c <= range.e.c; c++) {
        const cell = ws[XLSX.utils.encode_cell({ r, c })];
        if (cell) cell.s = { alignment: { horizontal: 'center', vertical: 'center' } };
      }
    }
    const wb = XLSX.utils.book_new();
    XLSX.utils.book_append_sheet(wb, ws, 'История движения');
    XLSX.writeFile(wb, `История_движения_${t.vagon}.xlsx`);
  }

  // ── Форматы времени (МСК naive, отдаём как пришло — без сдвигов) ──────────
  /** дд.мм.гг */
  fmtD(ts: string): string {
    if (!ts || ts.length < 10) return '—';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)}.${ts.slice(2, 4)}`;
  }
  /** дд.мм.гг чч:мм */
  fmtDT(ts: string): string {
    if (!ts || ts.length < 16) return '—';
    return `${this.fmtD(ts)} ${ts.slice(11, 16)}`;
  }

  /** Сдвиг даты в строке «ГГГГ-ММ-ДД…» на N суток (только для подсказки). */
  private shiftDay(ts: string, days: number): string {
    const d = new Date(`${ts.slice(0, 10)}T00:00:00`);
    d.setDate(d.getDate() + days);
    const p = (n: number) => String(n).padStart(2, '0');
    return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
  }
}
