import { Component, OnDestroy, OnInit, inject, signal } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzMessageService } from 'ng-zorro-antd/message';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../../environments/environment';
import { apiErrorMessage } from '../../core/api/api-error';

/** Поезд «бесплановых в подходе» (сигнал: движется без плана ближе порога). */
export interface UnplannedTrain {
  index: string;
  station_oper: string;
  rasst: number | null;
  vagon_count: number;
  sostav: string[];
  vagons: string[];
}

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
  unplanned: UnplannedTrain[];
}

/**
 * Карточка «Оперативка»: прибыло/выгружено по терминалам за вчерашние и текущие
 * ЖД-сутки (вехи vagon_history) + «не выгружено» (текущий статус 10 из снимка).
 * Терминалы сгруппированы по станциям (порядок — как колонки страницы);
 * автообновление раз в минуту, «умное» (без перерисовки).
 */
@Component({
  selector: 'app-operativka-card',
  imports: [NzButtonModule, NzTooltipModule],
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
          <!-- Подзаголовки станций убраны (решение владельца): порядок строк тот же,
               терминалы идут группами станций, но без строк-разделителей. -->
          @for (r of data()?.rows ?? []; track r.terminal) {
            <tr>
              <td class="t-col">{{ r.terminal }}</td>
              <td class="c">{{ r.prib_yesterday || '—' }}</td>
              <td class="c">{{ r.vigr_yesterday || '—' }}</td>
              <td class="c">{{ r.prib_today || '—' }}</td>
              <td class="c">{{ r.vigr_today || '—' }}</td>
              <td class="c nu-val">{{ r.not_unloaded || '—' }}</td>
            </tr>
          } @empty {
            <tr><td colspan="6" class="empty">{{ loading() ? 'Загрузка…' : 'Нет терминалов' }}</td></tr>
          }
        </tbody>
      </table>

      <!-- Бесплановые в подходе: сменили станцию без плана ближе порога.
           Живут до «Скрыть» (или пока не получат план / не прибудут). -->
      @if (data()?.unplanned?.length) {
        <div class="unpl">
          <div class="unpl-title"><b>Без плана в подходе ({{ data()!.unplanned.length }})</b>
            <span class="hint">двигаются, плана нет</span></div>
          @for (u of data()!.unplanned; track u.index) {
            <div class="unpl-row">
              <span class="num b">{{ u.index || '—' }}</span>
              <span class="mut">({{ u.vagon_count }})</span>
              <span class="unpl-sost ell" [title]="u.sostav.join(' · ')">{{ u.sostav.join(' · ') }}</span>
              <span class="mut nowrap">{{ u.station_oper }}@if (u.rasst != null) { · {{ u.rasst }} км }</span>
              <button nz-button nzSize="small" nz-tooltip
                      nzTooltipTitle="Скрыть (появится снова при следующей смене станции без плана)"
                      (click)="dismiss(u)">Скрыть</button>
            </div>
          }
        </div>
      }
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
    .empty { text-align: center; color: var(--color-text-secondary); padding: var(--space-sm); }
    /* Бесплановые в подходе — жёлтая секция-сигнал. */
    .unpl { background: var(--color-warning-bg); border: 1px solid var(--color-warning);
            border-radius: var(--radius-md); padding: var(--space-xs) var(--space-sm);
            margin-top: var(--space-sm); display: flex; flex-direction: column; gap: 2px; }
    .unpl-title { display: flex; align-items: center; gap: var(--space-sm); font-size: var(--font-size-sm); }
    .unpl-row { display: flex; align-items: center; gap: var(--space-sm); font-size: var(--font-size-sm);
                padding: 2px 0; min-width: 0; }
    .b { font-weight: 600; }
    .num { font-variant-numeric: tabular-nums; }
    /* Данные — основным цветом (как в «Прибывших»/«Ближайших»), серый только для пояснения. */
    .mut { color: inherit; }
    .hint { color: var(--color-text-secondary); }
    .nowrap { white-space: nowrap; }
    .unpl-sost { flex: 1 1 auto; min-width: 0; }
    .ell { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  `],
})
export class OperativkaCardComponent implements OnInit, OnDestroy {
  private readonly http = inject(HttpClient);
  private readonly msg = inject(NzMessageService);
  private readonly url = `${environment.apiBaseUrl}/v1/dislocation/operativka`;

  readonly loading = signal(false);
  readonly data = signal<Operativka | null>(null);
  private timer: ReturnType<typeof setInterval> | null = null;

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

  /** «Скрыть» бесплановых (указание оператора). */
  async dismiss(u: UnplannedTrain): Promise<void> {
    try {
      await firstValueFrom(this.http.post(`${this.url}/unplanned/dismiss`, { vagons: u.vagons }));
      this.msg.info(`Скрыто: поезд ${u.index || '—'} (${u.vagon_count} ваг.).`);
      await this.load();
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    }
  }

  /** дд.мм из yyyy-MM-dd. */
  dm(d: string | null | undefined): string {
    return d && d.length >= 10 ? `${d.slice(8, 10)}.${d.slice(5, 7)}` : '';
  }
}
