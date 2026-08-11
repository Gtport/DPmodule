import { Component, OnInit, computed, inject, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { DragDropModule } from '@angular/cdk/drag-drop';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { todayMsk } from '../../shared/msk-date';
import { ArrivalsApiService } from './arrivals-api.service';
import { LongStandApiService, LongStandVagon } from './long-stand-api.service';
import { VagonTrailModalComponent } from './vagon-trail-modal.component';

/**
 * Перемещаемая модалка «Долгостой»: вагоны, стоящие на станции назначения
 * дольше порога (dest_stand_hours, дефолт 48 ч), статусы ≥ 10 — и ждущие
 * выгрузки, и уже выгруженные, но не убранные с путей.
 *
 * Раскладка — решение владельца 12.08.2026: ПЛОСКИЙ список без группировки и
 * сворачивания. Терминал — своя колонка, а принадлежность видна заливкой всей
 * строки фирменным цветом терминала из реестра `ports` (как в «Брошенных» и
 * «Просрочке доставки»). Отдельной подсветки самых долгих нет — порядок задаёт
 * сервер (терминал → длительность стоянки убыванием), и дольше всех стоящие и
 * так идут сверху своего терминала.
 *
 * Клик или ПКМ по вагону — «История движения вагона» (по id рейса).
 */
@Component({
  selector: 'app-long-stand-modal',
  imports: [
    FormsModule, DragDropModule, NzButtonModule, NzIconModule, NzInputModule,
    NzModalModule, NzTagModule, NzTooltipModule, VagonTrailModalComponent,
  ],
  template: `
    <nz-modal [nzVisible]="true" [nzTitle]="ttl" [nzFooter]="null" nzWidth="1340px"
              [nzMask]="false" (nzOnCancel)="closed.emit()">
      <ng-template #ttl>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          Долгостой — дольше {{ thresholdHours() }} ч ({{ total() }})
        </div>
      </ng-template>

      <ng-container *nzModalContent>
        <div class="bar">
          <span class="mut">
            Стоят на станции назначения дольше порога, считая с прибытия.
            «Гружён» — ждёт выгрузки, «выгружен» — не убран с путей.
          </span>
          <span class="spacer"></span>
          <input nz-input nzSize="small" class="search" placeholder="№ вагона"
                 [ngModel]="search()" (ngModelChange)="search.set($event)" />
          <button nz-button nzType="text" nzSize="small" (click)="exportExcel()"
                  [disabled]="!total()" nz-tooltip nzTooltipTitle="Экспорт в Excel">
            <span nz-icon nzType="download"></span>
          </button>
          <button nz-button nzType="text" nzSize="small" nz-tooltip nzTooltipTitle="Обновить"
                  (click)="load()">
            <span nz-icon nzType="reload"></span>
          </button>
        </div>

        <div class="dp-tbl-wrap">
          <table class="dp-tbl">
            <thead>
              <tr>
                <th class="c-term">Терминал</th>
                <th class="c-vag">Вагон</th>
                <th class="c-state">Состояние</th>
                <th class="c-idx">Индекс</th>
                <th>Станция дислокации</th>
                <th class="c-op">Операция</th>
                <th class="c-dt">Время оп.</th>
                <th class="c-nach">Ст. погрузки</th>
                <th class="c-otpr">Отправитель</th>
                <th class="c-cargo">Груз</th>
                <th class="c-ves">Вес</th>
                <th class="c-dt">Прибыл</th>
                <th class="c-stood">Стоит</th>
              </tr>
            </thead>
            <tbody>
              @for (r of rows(); track r.id) {
                <tr class="rw" [style.background]="rowBg(r)" (click)="trailRow.set(r)">
                  <td class="ell term" [title]="r.naznach">{{ r.naznach || 'Без терминала' }}</td>
                  <td class="num">{{ r.vagon }}</td>
                  <td class="c">
                    <nz-tag class="chip" [nzColor]="r.state === 'гружён' ? 'red' : 'default'">
                      {{ r.state || '—' }}
                    </nz-tag>
                  </td>
                  <td class="num idx" [title]="r.index">{{ r.index || '—' }}</td>
                  <td class="ell" [title]="station(r)">{{ station(r) }}</td>
                  <td class="ell" [title]="r.oper_s">{{ r.oper_s || '—' }}</td>
                  <td class="c">{{ fmt(r.time_op) }}</td>
                  <td class="ell" [title]="r.station_nach">{{ r.station_nach || '—' }}</td>
                  <td class="ell" [title]="r.gruzotpr">{{ r.gruzotpr || '—' }}</td>
                  <td class="ell" [title]="r.cargo_s">{{ r.cargo_s || 'порожний' }}</td>
                  <td class="c">{{ r.ves ? r.ves.toFixed(1) : '—' }}</td>
                  <td class="c">{{ fmt(r.since) }}</td>
                  <td class="c stood">{{ stood(r.hours) }}</td>
                </tr>
              } @empty {
                <tr><td colspan="13" class="empty">Долгостоя нет</td></tr>
              }
            </tbody>
          </table>
        </div>

        <p class="hint">
          Цвет строки — терминал, клик по вагону — история движения.
          Отсчёт от прибытия (не от простоя РЖД: подача его обнуляет). Времена — московские.
        </p>
      </ng-container>
    </nz-modal>

    @if (trailRow(); as r) {
      <app-vagon-trail-modal [vagonId]="r.id" [vagon]="r.vagon" (closed)="trailRow.set(null)" />
    }
  `,
  styles: [`
    .ttl { cursor: move; user-select: none; }
    .bar { display: flex; align-items: center; gap: var(--space-sm); margin-bottom: var(--space-sm);
           font-size: var(--font-size-sm); }
    .spacer { flex: 1 1 auto; }
    .search { width: 140px; }
    .mut { color: var(--color-text-muted); }
    .dp-tbl { table-layout: fixed; }
    /* Строка залита фирменным цветом терминала (реестр ports), как в «Брошенных». */
    .rw { cursor: pointer; }
    .rw:hover td { background: var(--color-bg-hover); }
    /* Ширины фиксированные; станция дислокации — единственная резиновая колонка. */
    .c-term { width: 96px; } .c-vag { width: 84px; } .c-state { width: 88px; }
    .c-idx { width: 108px; }
    .c-op { width: 104px; } .c-dt { width: 106px; } .c-nach { width: 136px; }
    .c-otpr { width: 148px; } .c-cargo { width: 112px; } .c-ves { width: 52px; }
    .c-stood { width: 74px; }
    .num { font-variant-numeric: tabular-nums; }
    .idx, .ell { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .c { text-align: center; font-variant-numeric: tabular-nums; }
    .term { font-weight: 600; }
    .stood { font-weight: 600; }
    .chip { margin: 0; }
    .empty { text-align: center; color: var(--color-text-secondary); padding: var(--space-md); }
    .hint { margin: var(--space-xs) 0 0; color: var(--color-text-muted); font-size: var(--font-size-sm); }
  `],
})
export class LongStandModalComponent implements OnInit {
  private readonly api = inject(LongStandApiService);
  private readonly arrivals = inject(ArrivalsApiService);
  private readonly msg = inject(NzMessageService);

  readonly closed = output<void>();

  readonly vagons = signal<LongStandVagon[]>([]);
  readonly thresholdHours = signal(48);
  readonly search = signal('');
  readonly trailRow = signal<LongStandVagon | null>(null);
  private readonly termColor = signal<Record<string, string>>({});

  /** Плоский список в порядке сервера: терминал → длительность стоянки убыванием. */
  readonly rows = computed(() => {
    const q = this.search().trim();
    return q ? this.vagons().filter((r) => r.vagon.includes(q)) : this.vagons();
  });

  readonly total = computed(() => this.rows().length);

  ngOnInit(): void {
    void this.load();
    void this.loadTerminals();
  }

  async load(): Promise<void> {
    try {
      const res = await this.api.getList();
      this.vagons.set(res?.vagons ?? []);
      if (res?.threshold_hours) this.thresholdHours.set(res.threshold_hours);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    }
  }

  private async loadTerminals(): Promise<void> {
    try {
      const cm: Record<string, string> = {};
      for (const t of await this.arrivals.getTerminals()) cm[t.name] = t.color;
      this.termColor.set(cm);
    } catch {
      /* цвета не критичны для показа таблицы */
    }
  }

  /** Заливка строки фирменным цветом терминала; терминала нет — без заливки. */
  rowBg(r: LongStandVagon): string | null {
    return this.termColor()[r.naznach] ?? null;
  }

  /** «Станция (дорога)» из последней известной позиции. */
  station(r: LongStandVagon): string {
    if (!r.station_oper) return '—';
    return r.doroga_oper ? `${r.station_oper} (${r.doroga_oper})` : r.station_oper;
  }

  /** Длительность стоянки: до двух суток в часах, дальше «Nс Nч» — так читается быстрее. */
  stood(hours: number): string {
    if (hours < 48) return `${hours} ч`;
    return `${Math.floor(hours / 24)}с ${hours % 24}ч`;
  }

  /** «2026-07-15T08:49:00» → «15.07.26 08:49»; пусто → «—». */
  fmt(ts: string | null): string {
    if (!ts || ts.length < 16) return '—';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)}.${ts.slice(2, 4)} ${ts.slice(11, 16)}`;
  }

  /** Экспорт видимого списка в Excel — теми же колонками, что на экране. */
  async exportExcel(): Promise<void> {
    const recs = this.rows();
    if (!recs.length) return;
    try {
      const XLSX = await import('xlsx-js-style');
      const wb = XLSX.utils.book_new();
      const sh = [
        ['Терминал', 'Вагон', 'Состояние', 'Индекс', 'Станция дислокации', 'Операция',
         'Время оп.', 'Ст. погрузки', 'Отправитель', 'Груз', 'Вес', 'Прибыл', 'Часов', 'Дней'],
        ...recs.map((r) => [
          r.naznach, r.vagon, r.state, r.index, this.station(r), r.oper_s,
          this.fmt(r.time_op), r.station_nach, r.gruzotpr, r.cargo_s || 'порожний',
          r.ves ? Math.round(r.ves * 10) / 10 : '', this.fmt(r.since), r.hours, r.days,
        ]),
      ];
      const ws = XLSX.utils.aoa_to_sheet(sh);
      ws['!cols'] = [9, 10, 11, 14, 28, 14, 15, 20, 22, 16, 7, 15, 7, 6].map((wch) => ({ wch }));
      XLSX.utils.book_append_sheet(wb, ws, 'Долгостой');
      XLSX.writeFile(wb, `Долгостой_${todayMsk()}.xlsx`);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    }
  }
}
