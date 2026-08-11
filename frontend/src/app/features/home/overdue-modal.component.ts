import { Component, OnInit, computed, inject, output, signal } from '@angular/core';
import { DragDropModule } from '@angular/cdk/drag-drop';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { OverdueApiService, OverdueGroup, OverdueVagon } from './overdue-api.service';
import { OverdueReportModalComponent } from './overdue-report-modal.component';
import { VagonTrailModalComponent } from './vagon-trail-modal.component';

/**
 * Перемещаемая модалка «Просрочка доставки» — вагоны текущего снимка, у которых
 * нормативный срок доставки уже истёк, а они ещё в пути (delay > 0, прибывшие
 * 10/12 не показываются — их просрочка зафиксирована в истории). Строка =
 * накладная (единица претензии к перевозчику по ст. 97 УЖТ: пени считаются от
 * провозной платы по накладной), разворот до вагонов; клик по вагону — «История
 * движения вагона». Отчёт по СВЕРШИВШИМСЯ просрочкам (прибыл позже норматива,
 * из истории рейсов) — кнопкой «Отчёт для претензии».
 */
@Component({
  selector: 'app-overdue-modal',
  imports: [
    DragDropModule, NzButtonModule, NzIconModule, NzModalModule,
    NzSpinModule, NzTooltipModule, OverdueReportModalComponent, VagonTrailModalComponent,
  ],
  template: `
    <nz-modal [nzVisible]="true" [nzTitle]="ttl" [nzFooter]="null" nzWidth="1120px"
              [nzMask]="false" (nzOnCancel)="closed.emit()">
      <ng-template #ttl>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          Просрочка доставки
          @if (vagonCount()) { <span class="sub">· {{ vagonCount() }} ваг. / {{ groups().length }} накладных</span> }
        </div>
      </ng-template>

      <ng-container *nzModalContent>
        <div class="bar">
          <span class="spacer"></span>
          <button nz-button nzSize="small" (click)="showReport.set(true)"
                  nz-tooltip nzTooltipTitle="Excel по фактам просрочки из истории рейсов (прибыл позже норматива) — для претензионной работы">
            <span nz-icon nzType="file-excel"></span> Отчёт для претензии
          </button>
          <button nz-button nzType="text" nzSize="small" (click)="exportExcel()" [disabled]="!groups().length"
                  nz-tooltip nzTooltipTitle="Экспорт текущего списка в Excel">
            <span nz-icon nzType="download"></span>
          </button>
          <button nz-button nzType="text" nzSize="small" (click)="load()" [nzLoading]="loading()"
                  nz-tooltip nzTooltipTitle="Обновить">
            <span nz-icon nzType="reload"></span>
          </button>
        </div>

        <nz-spin [nzSpinning]="loading()">
          <div class="dp-tbl-wrap" style="max-height: 62vh">
            <table class="dp-tbl">
              <thead><tr>
                <th class="c-inv">Накладная</th><th class="c-n">Вагонов</th>
                <th>Грузоотправитель</th><th>Ст. отправления</th><th>Ст. назначения</th>
                <th class="c-term">Терминал</th><th>Груз</th>
                <th class="c-dt">Срок доставки</th><th class="c-d">Просрочка</th>
              </tr></thead>
              <tbody>
                @for (g of groups(); track g.key) {
                  <tr class="grp" (click)="toggle(g.key)">
                    <td class="c-inv num">
                      <span nz-icon [nzType]="isOpen(g.key) ? 'down' : 'right'" class="tw"></span>
                      {{ g.key || 'Без накладной' }}
                    </td>
                    <td class="c num">{{ g.vagon_count }}</td>
                    <td class="ell" [title]="g.gruzotpr">{{ g.gruzotpr || '—' }}</td>
                    <td class="ell" [title]="g.station_nach">{{ g.station_nach || '—' }}</td>
                    <td class="ell" [title]="g.stan_nazn">{{ g.stan_nazn || '—' }}</td>
                    <td class="c">{{ g.gruzpol_s || '—' }}</td>
                    <td class="ell" [title]="g.cargo_s">{{ g.cargo_s || '—' }}</td>
                    <td class="c">{{ fmtDate(g.date_dostav) }}</td>
                    <td class="c num"><b class="warn">{{ g.max_delay }} сут</b></td>
                  </tr>
                  @if (isOpen(g.key)) {
                    @for (v of g.vagons; track v.id) {
                      <tr class="vag">
                        <td class="c-inv"></td>
                        <td class="c num">
                          @if (v.id) {
                            <a class="lnk" (click)="openTrail(v)" nz-tooltip nzTooltipTitle="История движения вагона">{{ v.vagon }}</a>
                          } @else { {{ v.vagon }} }
                        </td>
                        <td class="c num">{{ v.index || '—' }}</td>
                        <td class="ell" [title]="v.station_oper">{{ v.station_oper || '—' }}</td>
                        <td class="ell">{{ v.oper_s || '—' }} {{ fmtDT(v.time_op) }}</td>
                        <td class="c">{{ v.naznach || '—' }}</td>
                        <td class="ell" [title]="v.cargo_s">{{ v.cargo_s || '—' }}</td>
                        <td class="c">{{ fmtDate(v.date_dostav) }}</td>
                        <td class="c num"><span class="warn">{{ v.delay }} сут</span></td>
                      </tr>
                    }
                  }
                } @empty {
                  <tr><td colspan="9" class="empty">
                    @if (loading()) { Загрузка… } @else { Вагонов с истекшим сроком доставки нет }
                  </td></tr>
                }
              </tbody>
            </table>
          </div>
        </nz-spin>
        <p class="hint">Строка — накладная (единица претензии по ст. 97 УЖТ), клик — вагоны; у развёрнутых вагонов показаны станция и время последней операции. Прибывшие вагоны здесь не показываются — их просрочка попадает в «Отчёт для претензии».</p>
      </ng-container>
    </nz-modal>

    @if (showReport()) {
      <app-overdue-report-modal (closed)="showReport.set(false)" />
    }
    @if (trailFor(); as tf) {
      <app-vagon-trail-modal [vagonId]="tf.id" [vagon]="tf.vagon" (closed)="trailFor.set(null)" />
    }
  `,
  styles: [`
    .ttl { cursor: move; user-select: none; }
    .ttl .sub { color: var(--color-text-muted); font-weight: 400; font-size: var(--font-size-sm); }
    .bar { display: flex; align-items: center; gap: var(--space-sm); margin-bottom: var(--space-sm); }
    .spacer { flex: 1 1 auto; }
    .dp-tbl th { white-space: nowrap; }
    .grp { cursor: pointer; }
    .grp:hover td { background: var(--color-bg-hover); }
    .vag td { background: var(--color-bg-subtle); }
    .tw { font-size: 10px; color: var(--color-text-muted); margin-right: 4px; }
    .c { text-align: center; white-space: nowrap; }
    .num { font-variant-numeric: tabular-nums; }
    .ell { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 220px; }
    .c-inv { width: 140px; } .c-n { width: 70px; } .c-term { width: 86px; }
    .c-dt { width: 108px; } .c-d { width: 96px; }
    .warn { color: var(--color-danger-text); }
    .lnk { color: var(--color-primary-active); text-decoration: underline; cursor: pointer; }
    .empty { text-align: center; color: var(--color-text-secondary); padding: var(--space-md); }
    .hint { margin: var(--space-xs) 0 0; color: var(--color-text-muted); font-size: var(--font-size-sm); }
  `],
})
export class OverdueModalComponent implements OnInit {
  private readonly api = inject(OverdueApiService);
  private readonly msg = inject(NzMessageService);

  readonly closed = output<void>();

  readonly loading = signal(false);
  readonly groups = signal<OverdueGroup[]>([]);
  readonly open = signal<Set<string>>(new Set());
  readonly showReport = signal(false);
  readonly trailFor = signal<{ id: string; vagon: string } | null>(null);

  readonly vagonCount = computed(() =>
    this.groups().reduce((s, g) => s + g.vagon_count, 0));

  ngOnInit(): void {
    void this.load();
  }

  async load(): Promise<void> {
    this.loading.set(true);
    try {
      this.groups.set(await this.api.getGroups());
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

  openTrail(v: OverdueVagon): void {
    this.trailFor.set({ id: v.id, vagon: v.vagon });
  }

  /** «2026-08-08T00:00:00» → «08.08.2026»; пусто → «—». */
  fmtDate(ts: string | null): string {
    if (!ts || ts.length < 10) return '—';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)}.${ts.slice(0, 4)}`;
  }

  /** «2026-07-24T08:05:00» → «24.07 08:05»; пусто → пустая строка. */
  fmtDT(ts: string | null): string {
    if (!ts || ts.length < 16) return '';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)} ${ts.slice(11, 16)}`;
  }

  /** Экспорт текущего списка (снимок, повагонно) в Excel на клиенте. */
  async exportExcel(): Promise<void> {
    const groups = this.groups();
    if (!groups.length) return;
    try {
      const XLSX = await import('xlsx-js-style');
      const wb = XLSX.utils.book_new();
      const rows: (string | number)[][] = [
        ['Накладная', 'Вагон', 'Индекс', 'Грузоотправитель', 'Ст. отправления', 'Ст. назначения',
         'Назначение', 'Груз', 'Ст. операции', 'Операция', 'Срок доставки', 'Просрочка, сут'],
      ];
      for (const g of groups) {
        for (const v of g.vagons) {
          rows.push([
            g.key || 'Без накладной', v.vagon, v.index, g.gruzotpr, g.station_nach, g.stan_nazn,
            v.naznach, v.cargo_s, v.station_oper, `${v.oper_s} ${this.fmtDT(v.time_op)}`.trim(),
            this.fmtDate(v.date_dostav), v.delay,
          ]);
        }
      }
      const ws = XLSX.utils.aoa_to_sheet(rows);
      ws['!cols'] = [15, 11, 15, 20, 20, 18, 11, 20, 20, 18, 13, 12].map((wch) => ({ wch }));
      XLSX.utils.book_append_sheet(wb, ws, 'Просрочка доставки');
      XLSX.writeFile(wb, `просрочка_доставки_снимок_${this.fmtDate(new Date().toISOString())}.xlsx`);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    }
  }
}
