import { Component, computed, input, signal } from '@angular/core';
import { GtTrain } from './gt-forecast-api.service';

/**
 * Таблица очереди поездов терминала (перенос gtport TrainTable, этап
 * просмотра). Колонки: Поезд (индекс + состав по подгруппам с цветом клиента),
 * Прибытие (prog_jd), Путь (осталось в пути), Опережение (mistake — риск
 * броска/срыва нитки), Ожид (ожидание начала выгрузки из симуляции).
 *
 * Шапка: остатки терминала, счётчики поездов/брошенных, кликабельные бейджи
 * риска ⚠/!/~ — клик подсвечивает строки этого уровня.
 */

type RiskLevel = 'critical' | 'warning' | 'watch';

interface Risk {
  level: RiskLevel;
  reason: string;
}

interface Row {
  train: GtTrain;
  risk: Risk | null;
  arrival: string;
  arrivalColor: string;
  arrivalBold: boolean;
  path: string;
  mistake: string;
  mistakeColor: string;
  wait: string;
  waitColor: string;
  waitBold: boolean;
}

const RISK_COLOR: Record<RiskLevel, string> = {
  critical: '#c62828',
  warning: '#e65100',
  watch: '#f9a825',
};

@Component({
  selector: 'app-gt-train-table',
  template: `
    <div class="tt">
      <div class="head" [style.background]="black() ? '#f5f5f5' : (color() || '#f5f5f5')">
        <span class="name">{{ title() }}</span>
        <span class="ost">{{ remainder() }}</span>
        <span class="cnt">{{ rows().length }} поезд.</span>
        @if (thrownCount() > 0) {
          <span class="thrown">✕{{ thrownCount() }} бр.</span>
        }
        @for (lvl of levels; track lvl) {
          @if (riskCount()[lvl] > 0) {
            <button class="risk-badge" [class.on]="riskFilter() === lvl"
                    [style.color]="riskColor[lvl]"
                    [title]="riskHint[lvl]"
                    (click)="toggleFilter(lvl)">
              {{ riskMark[lvl] }} {{ riskCount()[lvl] }}
            </button>
          }
        }
      </div>
      <div class="dp-tbl-wrap">
        <table class="dp-tbl">
          <thead>
            <tr>
              <th>Поезд</th>
              <th title="Прибытие (ЖД)">Прибытие</th>
              <th title="Осталось в пути (ч/сут)">Путь</th>
              <th title="Опережение нитки (mistake): + опережает слот (придержат/бросок), − не успевает на нитку (план слетит)">Опер.</th>
              <th title="Ожидание начала выгрузки (ч)">Ожид</th>
            </tr>
          </thead>
          <tbody>
            @for (r of rows(); track r.train.index + r.train.station_oper) {
              <tr [class.arrived]="r.train.is_arrived"
                  [style.background]="rowBg(r)">
                <td class="c-train">
                  <div class="idx">
                    @if (r.risk) {
                      <span class="dot" [style.background]="riskColor[r.risk.level]"
                            [title]="r.risk.reason"></span>
                    }
                    <b [title]="r.train.index">{{ shortIndex(r.train.index) }}</b>
                    <span class="vc">{{ r.train.vagon_count }}в</span>
                  </div>
                  <div class="compose">
                    @for (sg of r.train.sub_groups; track sg.key) {
                      <span [style.color]="black() ? '#111' : (sg.color || '#111')"
                            [title]="sg.station_nach + ' → ' + sg.naznach + (sg.cargo_group ? ' · ' + sg.cargo_group : '') + (sg.is_universal ? ' · универсальный' : '')">
                        {{ sg.station_nach }}&thinsp;×{{ sg.vagon_count }}{{ sg.is_universal ? '·У' : '' }}
                      </span>
                    }
                  </div>
                </td>
                <td [style.color]="r.arrivalColor" [class.b]="r.arrivalBold">{{ r.arrival }}</td>
                <td>{{ r.path }}</td>
                <td [style.color]="r.mistakeColor" class="b">{{ r.mistake }}</td>
                <td [style.color]="r.waitColor" [class.b]="r.waitBold">{{ r.wait }}</td>
              </tr>
            } @empty {
              <tr><td colspan="5" class="empty">Нет поездов</td></tr>
            }
          </tbody>
        </table>
      </div>
    </div>
  `,
  styles: [`
    .tt { background: var(--color-bg-surface); border-radius: var(--radius-card);
          box-shadow: var(--shadow-card); overflow: hidden; }
    .head { display: flex; align-items: center; gap: var(--space-sm);
            padding: 2px var(--space-sm); font-size: 12px; flex-wrap: wrap; }
    .head .name { font-weight: 600; }
    .head .ost { color: #333; }
    .head .cnt { margin-left: auto; color: #555; }
    .thrown { color: #c62828; font-weight: 600; }
    .risk-badge { border: 1px solid transparent; background: transparent; border-radius: 3px;
                  font-size: 12px; font-weight: 600; cursor: pointer; padding: 0 4px; }
    .risk-badge.on { border-color: currentColor; background: rgba(0,0,0,0.04); }
    .dp-tbl-wrap { max-height: 70vh; overflow: auto; }
    .dp-tbl td, .dp-tbl th { font-size: 11px; padding: 2px 4px; text-align: center; }
    .c-train { text-align: left; }
    .idx { display: flex; align-items: center; gap: 4px; }
    .vc { color: #777; }
    .dot { width: 8px; height: 8px; border-radius: 50%; display: inline-block; flex: none; }
    .compose { display: flex; flex-wrap: wrap; gap: 0 6px; font-size: 10px; }
    .b { font-weight: 600; }
    .arrived { opacity: 0.85; }
    .empty { color: #888; padding: var(--space-md); }
  `],
})
export class GtTrainTableComponent {
  readonly title = input.required<string>();
  readonly color = input<string>('');
  /** Поезда терминала (отфильтрованы родителем), в хронологии прибытия. */
  readonly trains = input.required<GtTrain[]>();
  /** Текст остатков в шапке («остаток: N» / «остаток У:n М:n Ч:n»). */
  readonly remainder = input<string>('');
  /** Ожидание выгрузки по индексу поезда (минуты, из операций симуляции). */
  readonly waitByIndex = input<Record<string, number>>({});
  readonly black = input<boolean>(false);

  readonly riskFilter = signal<RiskLevel | null>(null);

  readonly levels: RiskLevel[] = ['critical', 'warning', 'watch'];
  readonly riskColor = RISK_COLOR;
  readonly riskMark: Record<RiskLevel, string> = { critical: '⚠', warning: '!', watch: '~' };
  readonly riskHint: Record<RiskLevel, string> = {
    critical: 'Критично: план слетит или поезд придержат',
    warning: 'Внимание: план оптимистичен либо вероятно бросание',
    watch: 'Следить: опережение нитки',
  };

  readonly rows = computed<Row[]>(() =>
    this.trains().map((t) => {
      const waitMin = this.waitByIndex()[t.index] ?? 0;
      const waitH = waitMin / 60;
      return {
        train: t,
        risk: risk(t),
        arrival: fmtJd(t.prog_jd),
        arrivalColor: t.is_arrived ? '#1565c0' : t.status === '5' ? '#c62828' : t.plan_jd ? '#2e7d32' : '',
        arrivalBold: !!t.plan_jd || t.status === '5',
        path: fmtPath(t),
        mistake: fmtMistake(t.mistake),
        mistakeColor: mistakeColor(t.mistake),
        wait: waitMin > 0 ? waitH.toFixed(1) + 'ч' : '',
        waitColor: waitH > 12 ? '#c62828' : waitH > 6 ? '#e65100' : waitMin > 0 ? '#2e7d32' : '',
        waitBold: waitH > 6,
      };
    })
  );

  readonly thrownCount = computed(() => this.trains().filter((t) => t.status === '5').length);

  readonly riskCount = computed<Record<RiskLevel, number>>(() => {
    const c: Record<RiskLevel, number> = { critical: 0, warning: 0, watch: 0 };
    for (const r of this.rows()) if (r.risk) c[r.risk.level]++;
    return c;
  });

  toggleFilter(lvl: RiskLevel): void {
    this.riskFilter.set(this.riskFilter() === lvl ? null : lvl);
  }

  rowBg(r: Row): string {
    const f = this.riskFilter();
    if (!f || r.risk?.level !== f) return '';
    return { critical: 'rgba(198,40,40,0.10)', warning: 'rgba(230,81,0,0.10)', watch: 'rgba(249,168,37,0.12)' }[f];
  }

  shortIndex = shortIndex;
}

/** Индикатор риска поезда (перенос gtport getRiskIndicator). */
function risk(t: GtTrain): Risk | null {
  if (t.is_arrived) return null;
  if (!/^\d{4}-\d{3}-\d{4}$/.test(t.index)) return null; // Б/И и с.ф. не оцениваем
  const inPlan = !!t.plan_jd;
  const thrown = t.status === '5';
  const mistake = t.mistake ?? 0;
  const togo = t.to_go ?? 0;

  if (inPlan && thrown) return { level: 'warning', reason: 'Нитка под угрозой срыва: поезд брошен' };
  if (inPlan) {
    if (mistake <= -1) {
      return { level: 'critical', reason: `ПЛАН СЛЕТИТ: не успевает на нитку на ${(-mistake).toFixed(1)} сут` };
    }
    if (mistake < -0.3) return { level: 'warning', reason: 'План оптимистичен: расчёт позже нитки' };
    return null;
  }
  if (mistake > 1) {
    if (togo <= 24) return { level: 'critical', reason: 'Опережает нитку у порта — придержат или бросят' };
    const ratio = togo > 0 ? mistake / (togo / 24) : Infinity;
    if (ratio > 0.5) return { level: 'warning', reason: 'Опережение велико — вероятно бросание в пути' };
    return { level: 'watch', reason: 'Опережает нитку — следить' };
  }
  return null;
}

function shortIndex(index: string): string {
  if (!index) return '???';
  if (index.includes('с.ф.')) return 'с.ф.';
  if (index.includes('Б/И')) return index;
  if (index.length >= 8) return index.substring(5, 8);
  return index;
}

function fmtJd(iso: string | null): string {
  if (!iso) return '—';
  return `${iso.slice(8, 10)}.${iso.slice(5, 7)} ${iso.slice(11, 16)}`;
}

function fmtPath(t: GtTrain): string {
  if (t.is_arrived) return '—';
  const togo = t.to_go;
  if (togo == null) return '—';
  return togo < 24 ? `${Math.round(togo)}ч` : (togo / 24).toFixed(1);
}

function fmtMistake(m: number | null): string {
  if (m == null || m === 0) return '';
  const sign = m > 0 ? '+' : '−';
  const abs = Math.abs(m);
  return abs < 1 ? `${sign}${Math.round(abs * 24)}ч` : `${sign}${abs.toFixed(1)}`;
}

function mistakeColor(m: number | null): string {
  if (m == null) return '';
  if (m < -0.3) return '#c62828';
  if (m < 0) return '#e65100';
  if (m >= 3) return '#c62828';
  if (m >= 1) return '#e65100';
  return '#2e7d32';
}
