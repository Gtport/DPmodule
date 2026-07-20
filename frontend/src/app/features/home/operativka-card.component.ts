import { Component, OnDestroy, OnInit, computed, inject, signal } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { NzMessageService } from 'ng-zorro-antd/message';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../../environments/environment';
import { apiErrorMessage } from '../../core/api/api-error';

/** Строка «Оперативки»: терминал и суточные счётчики. */
export interface OperativkaRow {
  terminal: string;
  station: string;
  station_code: string;
  prib_yesterday: number;
  vigr_yesterday: number;
  prib_today: number;
  vigr_today: number;
  not_unloaded: number;
}

export interface Operativka {
  yesterday: string;
  today: string;
  rows: OperativkaRow[];
}

/**
 * Карточка «Оперативка»: прибыло/выгружено по терминалам за вчерашние и текущие
 * ЖД-сутки (вехи vagon_history) + «не выгружено» (текущий статус 10 из снимка).
 * Терминалы сгруппированы по станциям (порядок — как колонки страницы);
 * автообновление раз в минуту, «умное» (без перерисовки).
 */
@Component({
  selector: 'app-operativka-card',
  imports: [],
  template: `
    <div class="card">
      <table class="mini">
        <thead>
          <tr>
            <th rowspan="2" class="t-col">Терминал</th>
            <th colspan="2">Вчера {{ dm(data()?.yesterday) }}</th>
            <th colspan="2">Сегодня {{ dm(data()?.today) }}</th>
            <th rowspan="2" class="nu" title="Прибыли, ещё не выгружены (статус 10)">Не выгр.</th>
          </tr>
          <tr>
            <th>Приб.</th><th>Выгр.</th><th>Приб.</th><th>Выгр.</th>
          </tr>
        </thead>
        <tbody>
          @for (g of grouped(); track g.code) {
            <tr class="st-row"><td colspan="6">{{ g.station }}</td></tr>
            @for (r of g.rows; track r.terminal) {
              <tr>
                <td class="t-col">{{ r.terminal }}</td>
                <td class="c">{{ r.prib_yesterday || '—' }}</td>
                <td class="c">{{ r.vigr_yesterday || '—' }}</td>
                <td class="c">{{ r.prib_today || '—' }}</td>
                <td class="c">{{ r.vigr_today || '—' }}</td>
                <td class="c nu-val">{{ r.not_unloaded || '—' }}</td>
              </tr>
            }
          } @empty {
            <tr><td colspan="6" class="empty">{{ loading() ? 'Загрузка…' : 'Нет терминалов' }}</td></tr>
          }
        </tbody>
      </table>
    </div>
  `,
  styles: [`
    .card { background: var(--color-bg-surface); border-radius: var(--radius-card);
            box-shadow: var(--shadow-card); padding: var(--space-sm) var(--space-md) var(--space-md); }
    .mini { width: 100%; border-collapse: collapse; font-size: var(--font-size-sm); }
    .mini th { background: var(--color-bg-subtle); font-weight: 600; padding: 3px 6px;
               border: 1px solid var(--color-border-light); text-align: center; }
    .mini td { padding: 3px 8px; border: 1px solid var(--color-border-light); }
    .t-col { text-align: left; }
    .c { text-align: center; font-variant-numeric: tabular-nums; }
    .nu { max-width: 64px; }
    .nu-val { font-weight: 600; }
    .st-row td { background: var(--color-bg-subtle); font-weight: 600; padding: 2px 8px; }
    .empty { text-align: center; color: var(--color-text-secondary); padding: var(--space-sm); }
  `],
})
export class OperativkaCardComponent implements OnInit, OnDestroy {
  private readonly http = inject(HttpClient);
  private readonly msg = inject(NzMessageService);
  private readonly url = `${environment.apiBaseUrl}/v1/dislocation/operativka`;

  readonly loading = signal(false);
  readonly data = signal<Operativka | null>(null);
  private timer: ReturnType<typeof setInterval> | null = null;

  /** Группировка строк по станциям (порядок с бэка сохранён). */
  readonly grouped = computed(() => {
    const out: { code: string; station: string; rows: OperativkaRow[] }[] = [];
    for (const r of this.data()?.rows ?? []) {
      const last = out[out.length - 1];
      if (last && last.code === r.station_code) last.rows.push(r);
      else out.push({ code: r.station_code, station: this.title(r.station), rows: [r] });
    }
    return out;
  });

  ngOnInit(): void {
    void this.load(true);
    this.timer = setInterval(() => void this.load(), 60_000);
  }

  ngOnDestroy(): void {
    if (this.timer) clearInterval(this.timer);
  }

  async load(initial = false): Promise<void> {
    if (initial) this.loading.set(true);
    try {
      this.data.set(await firstValueFrom(this.http.get<Operativka>(this.url)));
    } catch (err) {
      if (initial) this.msg.error(apiErrorMessage(err));
    } finally {
      if (initial) this.loading.set(false);
    }
  }

  /** «МЫС АСТАФЬЕВА» → «Мыс Астафьева». */
  private title(name: string): string {
    return name.toLowerCase().replace(/(^|[\s-])\p{L}/gu, (m) => m.toUpperCase());
  }

  /** дд.мм из yyyy-MM-dd. */
  dm(d: string | null | undefined): string {
    return d && d.length >= 10 ? `${d.slice(8, 10)}.${d.slice(5, 7)}` : '';
  }
}
