import { Component, OnInit, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { NzTableModule } from 'ng-zorro-antd/table';
import { NzRadioModule } from 'ng-zorro-antd/radio';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { MissingApiService, MissingVagon } from './missing-api.service';

/**
 * Экран «Пропавшие вагоны»: записи-8 из таблицы кандидатов (status9) — вагоны,
 * исчезнувшие из выгрузки в незавершённом рейсе (штатно выбывшие 6/10/12 сюда
 * не попадают). Показывается последняя известная позиция; записи старше TTL
 * (missing8_ttl_days) снимаются автоочисткой. Свежепропавшие — сверху.
 */
@Component({
  selector: 'app-missing',
  imports: [FormsModule, NzTableModule, NzRadioModule, NzTagModule, NzButtonModule, NzIconModule, NzInputModule],
  template: `
    <div class="page">
      <div class="bar">
        <nz-radio-group [(ngModel)]="terminal" nzButtonStyle="solid" nzSize="small">
          <label nz-radio-button nzValue="">Все</label>
          @for (t of terminals(); track t) {
            <label nz-radio-button [nzValue]="t">{{ t }}</label>
          }
        </nz-radio-group>
        <input
          nz-input
          nzSize="small"
          class="search"
          placeholder="№ вагона"
          [ngModel]="search()"
          (ngModelChange)="search.set($event)"
        />
        <span class="spacer"></span>
        <span class="count">пропавших: {{ filtered().length }}</span>
        <button nz-button nzSize="small" (click)="load()">
          <span nz-icon nzType="reload"></span>
        </button>
      </div>

      <p class="hint">
        Вагоны, исчезнувшие из выгрузки до завершения рейса (прибывшие, выгруженные и
        уехавшие порожними сюда не попадают). Показана последняя известная позиция.
        Записи старше {{ ttlDays }} суток снимаются автоматически. Времена — МСК.
      </p>

      <nz-table
        #tbl
        [nzData]="filtered()"
        [nzLoading]="loading()"
        nzSize="small"
        [nzShowPagination]="filtered().length > 100"
        [nzPageSize]="100"
        nzTableLayout="fixed"
      >
        <thead>
          <tr>
            <th nzWidth="90px">Вагон</th>
            <th nzWidth="130px">Индекс</th>
            <th>Станция операции</th>
            <th nzWidth="140px">Операция</th>
            <th nzWidth="105px">Время операции</th>
            <th nzWidth="70px">Терминал</th>
            <th nzWidth="140px">Груз</th>
            <th nzWidth="60px" class="c">Вес</th>
            <th nzWidth="105px">Пропал</th>
            <th nzWidth="60px" class="c">Дней</th>
          </tr>
        </thead>
        <tbody>
          @for (r of tbl.data; track r.vagon) {
            <tr [class.stale]="r.days_missing >= 3">
              <td class="num">{{ r.vagon }}</td>
              <td class="idx" [title]="r.index">{{ r.index || '—' }}</td>
              <td class="small" [title]="station(r)">{{ station(r) }}</td>
              <td class="small" [title]="r.oper_s">{{ r.oper_s || '—' }}</td>
              <td class="c">{{ fmt(r.time_op) }}</td>
              <td>
                @if (r.naznach) {
                  <nz-tag class="term">{{ r.naznach }}</nz-tag>
                } @else {
                  —
                }
              </td>
              <td class="small" [title]="r.cargo_s">{{ r.cargo_s || 'порожний' }}</td>
              <td class="c">{{ r.ves ? r.ves.toFixed(1) : '—' }}</td>
              <td class="c">{{ fmt(r.missing_since) }}</td>
              <td class="c days">{{ r.days_missing }}</td>
            </tr>
          }
        </tbody>
      </nz-table>
    </div>
  `,
  styles: [`
    .page { display: flex; flex-direction: column; gap: var(--space-sm); }
    .bar { display: flex; align-items: center; gap: var(--space-sm); }
    /* Таблица — белая «парящая» карточка на сером фоне (стиль gtport). */
    nz-table { display: block; background: var(--color-bg-surface); border-radius: var(--radius-card);
               box-shadow: var(--shadow-card); overflow: hidden; }
    .search { width: 140px; }
    .spacer { flex: 1 1 auto; }
    .count { color: var(--color-text-secondary); font-size: var(--font-size-sm); }
    .hint { color: var(--color-text-secondary); font-size: var(--font-size-sm); margin: 0; }
    .num { font-variant-numeric: tabular-nums; }
    .idx { font-variant-numeric: tabular-nums; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .small { color: var(--color-text-secondary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .c { text-align: center; font-variant-numeric: tabular-nums; }
    .days { font-weight: 600; }
    .term { margin: 0; }
    /* Давно пропавшие (3+ суток) — жёлтая подсветка строки. */
    tr.stale > td { background: var(--color-warning-bg, #fffbe6); }
    :host ::ng-deep .ant-table-tbody > tr > td { padding: 4px 8px; }
    :host ::ng-deep .ant-table-thead > tr > th { padding: 6px 8px; }
  `],
})
export class MissingComponent implements OnInit {
  private readonly api = inject(MissingApiService);
  /** Уведомления — всплывающие тосты с автоуборкой (договорённость), не баннеры в теле. */
  private readonly msg = inject(NzMessageService);

  readonly rows = signal<MissingVagon[]>([]);
  readonly loading = signal(false);
  readonly terminal = signal<string>('');
  readonly search = signal<string>('');

  /** Срок автоочистки (client_settings.extra.status.missing8_ttl_days) — для подсказки. */
  readonly ttlDays = 7;

  readonly terminals = computed(() =>
    [...new Set(this.rows().map((r) => r.naznach).filter(Boolean))].sort(),
  );
  readonly filtered = computed(() => {
    const t = this.terminal();
    const q = this.search().trim();
    return this.rows().filter(
      (r) => (!t || r.naznach === t) && (!q || r.vagon.includes(q)),
    );
  });

  ngOnInit(): void {
    void this.load();
  }

  async load(): Promise<void> {
    this.loading.set(true);
    try {
      this.rows.set(await this.api.getMissing());
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.loading.set(false);
    }
  }

  /** «Станция (дорога)» из последней известной позиции. */
  station(r: MissingVagon): string {
    if (!r.station_oper) return '—';
    return r.doroga_oper ? `${r.station_oper} (${r.doroga_oper})` : r.station_oper;
  }

  /** «2026-07-15T08:49:00» → «15.07 08:49»; пусто → «—». */
  fmt(ts: string | null): string {
    if (!ts || ts.length < 16) return '—';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)} ${ts.slice(11, 16)}`;
  }
}
