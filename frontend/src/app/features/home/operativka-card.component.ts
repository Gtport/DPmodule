import { Component, OnDestroy, OnInit, inject } from '@angular/core';
import { OperativkaApiService } from './operativka-api.service';

/**
 * Карточка «Прибытие/выгрузка»: прибыло/выгружено по терминалам за вчерашние и
 * текущие ЖД-сутки (вехи vagon_history) + «не выгружено» (текущий статус 10 из
 * снимка). Терминалы идут группами станций, как колонки страницы.
 *
 * Узкая (в половину колонки «Оперативка», под карточкой «Информация» — решение
 * владельца), поэтому колонка терминала без заголовка, а прибытие и выгрузка
 * живут в одной ячейке «П/В». Данные и минутное автообновление — в общем
 * OperativkaApiService (тот же ответ кормит карточку «Без плана в подходе»).
 */
@Component({
  selector: 'app-operativka-card',
  template: `
    <div class="card">
      <table class="mini">
        <thead>
          <tr>
            <th rowspan="2" class="t-col"></th>
            <th>Вчера {{ dm(api.data()?.yesterday) }}</th>
            <th>Сегодня {{ dm(api.data()?.today) }}</th>
            <th rowspan="2" class="nu" title="Прибыли, ещё не выгружены (статус 10)">Не выгр.</th>
          </tr>
          <tr>
            <th class="pv" title="Прибыло / выгружено">П/В</th>
            <th class="pv" title="Прибыло / выгружено">П/В</th>
          </tr>
        </thead>
        <tbody>
          @for (r of api.data()?.rows ?? []; track r.terminal) {
            <tr>
              <td class="t-col">{{ r.terminal }}</td>
              <td class="c">{{ pv(r.prib_yesterday, r.vigr_yesterday) }}</td>
              <td class="c">{{ pv(r.prib_today, r.vigr_today) }}</td>
              <td class="c nu-val">{{ r.not_unloaded || '—' }}</td>
            </tr>
          } @empty {
            <tr><td colspan="4" class="empty">{{ api.loading() ? 'Загрузка…' : 'Нет терминалов' }}</td></tr>
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
    .pv { font-weight: 500; color: var(--color-text-secondary); }
    .nu { max-width: 64px; }
    .nu-val { font-weight: 600; }
    .empty { text-align: center; color: var(--color-text-secondary); padding: var(--space-sm); }
  `],
})
export class OperativkaCardComponent implements OnInit, OnDestroy {
  readonly api = inject(OperativkaApiService);

  ngOnInit(): void { this.api.attach(); }
  ngOnDestroy(): void { this.api.detach(); }

  /** Совместимость с прежним вызовом со страницы (после пересборки снимка). */
  load(): Promise<void> { return this.api.load(); }

  /** «Прибыло/выгружено» в одну ячейку; пустые сутки — прочерк, не «—/—». */
  pv(prib: number, vigr: number): string {
    return prib || vigr ? `${prib || 0}/${vigr || 0}` : '—';
  }

  /** дд.мм из yyyy-MM-dd. */
  dm(d: string | null | undefined): string {
    return d && d.length >= 10 ? `${d.slice(8, 10)}.${d.slice(5, 7)}` : '';
  }
}
