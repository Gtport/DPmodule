import { Component, OnDestroy, OnInit, computed, inject } from '@angular/core';
import { OperativkaApiService, OperativkaRow } from './operativka-api.service';

/** Итоговая строка по всем терминалам (суммы счётчиков). */
interface OperativkaTotals {
  pogr_yesterday: number;
  prib_yesterday: number;
  vigr_yesterday: number;
  pogr_today: number;
  prib_today: number;
  vigr_today: number;
  not_unloaded: number;
}

/**
 * Карточка «Погрузка · Прибытие · Выгрузка»: суточные счётчики по терминалам
 * за вчерашние и текущие ЖД-сутки (вехи vagon_history: погружено в адрес
 * терминала — date_nach_d, без перегрузов, как в отчёте «Погрузка»; прибыло —
 * date_prib_d; выгружено — date_vigr_d) + «не выгружено» (текущий статус 10
 * из снимка). Терминалы идут группами станций, как колонки страницы.
 *
 * Во всю ширину колонки «Оперативка» (решение владельца 11.08.2026 — прежде
 * стояла в полширины, и прибытие с выгрузкой теснились в одной ячейке «П/В»):
 * каждый показатель — своя колонка, сутки разделены вертикальной границей,
 * сегодняшние значения выделены жирнее вчерашних, внизу строка «Итого».
 * Данные и минутное автообновление — в общем OperativkaApiService (тот же
 * ответ кормит карточку «Без плана в подходе»).
 */
@Component({
  selector: 'app-operativka-card',
  template: `
    <div class="card">
      <div class="head">
        <b>Погрузка · Прибытие · Выгрузка</b>
        @if (api.stale.stale()) {
          <span class="dp-stale"
                title="Автообновление не проходит — показаны последние полученные данные">{{ api.stale.label() }}</span>
        }
      </div>
      <table class="mini">
        <thead>
          <tr>
            <th rowspan="2" class="t-col">Терминал</th>
            <th colspan="3" class="day">Вчера {{ dm(api.data()?.yesterday) }}</th>
            <th colspan="3" class="day">Сегодня {{ dm(api.data()?.today) }}</th>
            <th rowspan="2" class="nu day" title="Прибыли, ещё не выгружены (статус 10)">Не выгр.</th>
          </tr>
          <tr>
            <th class="sub day" title="Погружено в адрес терминала (без перегрузов)">Погр</th>
            <th class="sub" title="Прибыло">Приб</th>
            <th class="sub" title="Выгружено">Выгр</th>
            <th class="sub day" title="Погружено в адрес терминала (без перегрузов)">Погр</th>
            <th class="sub" title="Прибыло">Приб</th>
            <th class="sub" title="Выгружено">Выгр</th>
          </tr>
        </thead>
        <tbody>
          @for (r of api.data()?.rows ?? []; track r.terminal) {
            <tr>
              <td class="t-col">{{ r.terminal }}</td>
              <td class="c yd day">{{ n(r.pogr_yesterday) }}</td>
              <td class="c yd">{{ n(r.prib_yesterday) }}</td>
              <td class="c yd">{{ n(r.vigr_yesterday) }}</td>
              <td class="c td-v day">{{ n(r.pogr_today) }}</td>
              <td class="c td-v">{{ n(r.prib_today) }}</td>
              <td class="c td-v">{{ n(r.vigr_today) }}</td>
              <td class="c nu-val day">{{ n(r.not_unloaded) }}</td>
            </tr>
          } @empty {
            <tr><td colspan="8" class="empty">{{ api.loading() ? 'Загрузка…' : 'Нет терминалов' }}</td></tr>
          }
        </tbody>
        @if (totals(); as t) {
          <tfoot>
            <tr class="total">
              <td class="t-col">Итого</td>
              <td class="c yd day">{{ n(t.pogr_yesterday) }}</td>
              <td class="c yd">{{ n(t.prib_yesterday) }}</td>
              <td class="c yd">{{ n(t.vigr_yesterday) }}</td>
              <td class="c td-v day">{{ n(t.pogr_today) }}</td>
              <td class="c td-v">{{ n(t.prib_today) }}</td>
              <td class="c td-v">{{ n(t.vigr_today) }}</td>
              <td class="c nu-val day">{{ n(t.not_unloaded) }}</td>
            </tr>
          </tfoot>
        }
      </table>
    </div>
  `,
  styles: [`
    .card { background: var(--color-bg-surface); border-radius: var(--radius-card);
            box-shadow: var(--shadow-card); padding: var(--space-sm) var(--space-md) var(--space-md); }
    .head { display: flex; align-items: center; gap: var(--space-sm); margin-bottom: var(--space-xs); }
    .mini { width: 100%; border-collapse: collapse; font-size: var(--font-size-sm); }
    .mini th { background: var(--color-bg-subtle); font-weight: 600; padding: 3px 6px;
               border: 1px solid var(--color-border-light); text-align: center; }
    .mini td { padding: 3px 8px; border: 1px solid var(--color-border-light); }
    .sub { font-weight: 500; color: var(--color-text-secondary); }
    /* Граница между сутками заметнее межколоночной — блоки читаются без вникания. */
    .day { border-left: 1px solid var(--color-border); }
    .t-col { text-align: left; }
    .c { text-align: center; font-variant-numeric: tabular-nums; }
    .yd { color: var(--color-text-secondary); }   /* вчера — фон для сравнения */
    .td-v { font-weight: 500; }                   /* сегодня — рабочие цифры смены */
    .nu-val { font-weight: 600; }
    .total td { border-top: 2px solid var(--color-border); background: var(--color-bg-subtle);
                font-weight: 600; }
    .total .yd { color: var(--color-text); }
    .empty { text-align: center; color: var(--color-text-secondary); padding: var(--space-sm); }
  `],
})
export class OperativkaCardComponent implements OnInit, OnDestroy {
  readonly api = inject(OperativkaApiService);

  /** «Итого» по всем терминалам; при одном терминале строка лишняя — не показываем. */
  readonly totals = computed<OperativkaTotals | null>(() => {
    const rows = this.api.data()?.rows ?? [];
    if (rows.length < 2) return null;
    const t: OperativkaTotals = {
      pogr_yesterday: 0, prib_yesterday: 0, vigr_yesterday: 0,
      pogr_today: 0, prib_today: 0, vigr_today: 0, not_unloaded: 0,
    };
    for (const r of rows as OperativkaRow[]) {
      t.pogr_yesterday += r.pogr_yesterday || 0;
      t.prib_yesterday += r.prib_yesterday || 0;
      t.vigr_yesterday += r.vigr_yesterday || 0;
      t.pogr_today += r.pogr_today || 0;
      t.prib_today += r.prib_today || 0;
      t.vigr_today += r.vigr_today || 0;
      t.not_unloaded += r.not_unloaded || 0;
    }
    return t;
  });

  ngOnInit(): void { this.api.attach(); }
  ngOnDestroy(): void { this.api.detach(); }

  /** Совместимость с прежним вызовом со страницы (после пересборки снимка). */
  load(): Promise<void> { return this.api.load(); }

  /** Ноль — прочерком: глаз ищет ненулевую работу, решётка нулей ему мешает. */
  n(v: number): string {
    return v ? String(v) : '—';
  }

  /** дд.мм из yyyy-MM-dd. */
  dm(d: string | null | undefined): string {
    return d && d.length >= 10 ? `${d.slice(8, 10)}.${d.slice(5, 7)}` : '';
  }
}
