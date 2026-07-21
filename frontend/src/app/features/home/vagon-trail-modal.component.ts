import { Component, OnInit, inject, input, output, signal } from '@angular/core';
import { DragDropModule } from '@angular/cdk/drag-drop';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { TrailOp, TrailVisit, VagonTrail, VagonTrailApiService } from './vagon-trail-api.service';

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
          }
          <span class="spacer"></span>
          <button nz-button nzSize="small" nzType="primary" [nzLoading]="pulling()" (click)="pull()"
                  nz-tooltip [nzTooltipTitle]="pullHint()">
            <span nz-icon nzType="cloud-download"></span> Обновить из АСУ
          </button>
          <button nz-button nzType="text" nzSize="small" nz-tooltip nzTooltipTitle="Скачать историю (Excel)"
                  [disabled]="!trail()?.count" (click)="exportExcel()">
            <span nz-icon nzType="download"></span>
          </button>
        </div>

        <nz-spin [nzSpinning]="loading()">
          <div class="tbl-wrap">
            <table class="tbl">
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
                  <tr class="visit" (click)="toggle(i)">
                    <td class="c-st">
                      <span nz-icon [nzType]="isOpen(i) ? 'down' : 'right'" class="tw"></span>
                      {{ v.station || ('код ' + v.stan_op) }}
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
    .tbl-wrap { max-height: 60vh; overflow: auto; }
    .tbl { width: 100%; border-collapse: collapse; font-size: var(--font-size-sm); }
    .tbl th { position: sticky; top: 0; background: var(--color-bg-subtle); font-weight: 600;
              padding: 4px 8px; border: 1px solid var(--color-border-light); text-align: center; z-index: 1; }
    .tbl td { padding: 3px 8px; border: 1px solid var(--color-border-light); }
    .visit { cursor: pointer; }
    .visit:hover { background: var(--color-bg-hover); }
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
      if (!t.count) await this.pull();
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

  /** Экспорт — ПОЛНЫЙ трейл (свёртка нужна на экране, в файле удобнее всё). */
  async exportExcel(): Promise<void> {
    const t = this.trail();
    if (!t?.count) return;
    const XLSX = await import('xlsx');
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
    const wb = XLSX.utils.book_new();
    XLSX.utils.book_append_sheet(wb, XLSX.utils.json_to_sheet(rows), 'История движения');
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
