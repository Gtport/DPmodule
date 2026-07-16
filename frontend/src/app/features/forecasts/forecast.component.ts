import { Component, OnInit, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { NzTableModule } from 'ng-zorro-antd/table';
import { NzRadioModule } from 'ng-zorro-antd/radio';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { apiErrorMessage } from '../../core/api/api-error';
import { ForecastApiService, ForecastTrain } from './forecast-api.service';

/**
 * Экран «Прогнозы» (тестовый): поезда с прогнозными полями Stage 3/4 из RAM-снимка.
 * Фильтр по терминалу, зелёная подсветка плановых (нитка задана планом). Столбцы:
 * терминал · индекс · состав («кол-во, sms_2/причал → площадка, вес») · станция
 * назначения · расчёт МСК · прогноз МСК · прогноз ЖД · mistake.
 */
@Component({
  selector: 'app-forecast',
  imports: [FormsModule, NzTableModule, NzRadioModule, NzTagModule, NzButtonModule, NzIconModule],
  template: `
    <div class="page">
      <div class="bar">
        <nz-radio-group [(ngModel)]="terminal" nzButtonStyle="solid" nzSize="small">
          <label nz-radio-button nzValue="">Все</label>
          @for (t of terminals(); track t) {
            <label nz-radio-button [nzValue]="t">{{ t }}</label>
          }
        </nz-radio-group>
        <span class="spacer"></span>
        <span class="count">поездов: {{ filtered().length }}</span>
        <button nz-button nzSize="small" (click)="load()">
          <span nz-icon nzType="reload"></span>
        </button>
      </div>

      <p class="hint">
        Зелёным — поезда с нитками из плана подвода (прогноз = план). Остальные — беспланные,
        разложены по ниткам причала. Времена — МСК.
      </p>

      <nz-table
        #tbl
        [nzData]="filtered()"
        [nzLoading]="loading()"
        nzSize="small"
        [nzShowPagination]="false"
        [nzFrontPagination]="false"
        nzTableLayout="fixed"
      >
        <thead>
          <tr>
            <th nzWidth="70px">Терминал</th>
            <th nzWidth="130px">Индекс</th>
            <th nzWidth="230px">Состав</th>
            <th>Станция назначения</th>
            <th nzWidth="105px">Расчёт МСК</th>
            <th nzWidth="105px">Прогноз МСК</th>
            <th nzWidth="105px">Прогноз ЖД</th>
            <th nzWidth="80px" class="c">Mistake</th>
          </tr>
        </thead>
        <tbody>
          @for (r of tbl.data; track r.id_disl) {
            <tr [class.plan]="r.has_plan">
              <td><nz-tag class="term">{{ r.naznach }}</nz-tag></td>
              <td class="idx" [title]="r.id_disl">{{ r.index || '—' }}</td>
              <td class="sostav">{{ sostav(r) }}</td>
              <td class="small" [title]="r.stan_nazn">{{ r.stan_nazn || '—' }}</td>
              <td class="c">{{ fmt(r.rasch_msk) }}</td>
              <td class="c prog">{{ fmt(r.prog_msk) }}</td>
              <td class="c">{{ fmt(r.prog_jd) }}</td>
              <td class="c">{{ mistake(r.mistake) }}</td>
            </tr>
          }
        </tbody>
      </nz-table>

      @if (error()) {
        <p class="err">{{ error() }}</p>
      }
    </div>
  `,
  styles: [`
    .page { display: flex; flex-direction: column; gap: var(--space-sm); }
    .bar { display: flex; align-items: center; gap: var(--space-sm); }
    .spacer { flex: 1 1 auto; }
    .count { color: var(--color-text-secondary); font-size: var(--font-size-sm); }
    .hint { color: var(--color-text-secondary); font-size: var(--font-size-sm); margin: 0; }
    .idx { font-variant-numeric: tabular-nums; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .small { color: var(--color-text-secondary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .sostav { font-size: var(--font-size-sm); }
    .c { text-align: center; font-variant-numeric: tabular-nums; }
    .prog { font-weight: 600; }
    .term { margin: 0; }
    /* Плановые поезда — зелёная подсветка строки. */
    tr.plan > td { background: var(--color-success-bg, #f6ffed); }
    .err { color: var(--color-error, #cf1322); font-size: var(--font-size-sm); }
    :host ::ng-deep .ant-table-tbody > tr > td { padding: 4px 8px; }
    :host ::ng-deep .ant-table-thead > tr > th { padding: 6px 8px; }
  `],
})
export class ForecastComponent implements OnInit {
  private readonly api = inject(ForecastApiService);

  readonly rows = signal<ForecastTrain[]>([]);
  readonly loading = signal(false);
  readonly error = signal<string | null>(null);
  readonly terminal = signal<string>('');

  readonly terminals = computed(() =>
    [...new Set(this.rows().map((r) => r.naznach))].sort(),
  );
  readonly filtered = computed(() => {
    const t = this.terminal();
    return t ? this.rows().filter((r) => r.naznach === t) : this.rows();
  });

  ngOnInit(): void {
    void this.load();
  }

  async load(): Promise<void> {
    this.loading.set(true);
    this.error.set(null);
    try {
      this.rows.set(await this.api.getForecast());
    } catch (err) {
      this.error.set(apiErrorMessage(err));
    } finally {
      this.loading.set(false);
    }
  }

  /** Состав: «кол-во ваг sms_2 причал → площадка вес» (стрелка — перестановка назначения). */
  sostav(r: ForecastTrain): string {
    const marka = [r.sms_2, r.gruzpol_s].filter(Boolean).join(' ');
    const route = r.naznach ? `${marka} → ${r.naznach}` : marka;
    const ves = r.ves ? ` ${Math.round(r.ves)}т` : '';
    return `${r.vagon_count} ваг${route ? ' ' + route : ''}${ves}`;
  }

  /** «2026-07-15T08:49:00» → «15.07 08:49»; пусто → «—». */
  fmt(ts: string | null): string {
    if (!ts || ts.length < 16) return '—';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)} ${ts.slice(11, 16)}`;
  }

  /** Mistake (дни) → «+1.3» / «−0.4»; пусто/0 → «—». */
  mistake(m: number | null): string {
    if (m == null || Math.abs(m) < 0.05) return '—';
    return (m > 0 ? '+' : '') + m.toFixed(1);
  }
}
