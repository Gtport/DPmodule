import { Component, computed, input, output, signal } from '@angular/core';
import { GtFreeSlot, GtTrain } from './gt-forecast-api.service';

/**
 * Таблица очереди поездов терминала (перенос gtport TrainTable). Колонки:
 * Поезд (индекс · состав одной строкой, как эталон), Прибытие (prog_jd),
 * Путь (осталось в пути), Опережение (mistake — риск броска/срыва нитки),
 * Ожид (ожидание начала выгрузки из симуляции).
 *
 * Вся информация о поезде — в ОДНОЙ строке (таблицы двух терминалов стоят
 * рядом). Состав — подгруппы СВОЕГО терминала «станция (Nв)» через «·», лишнее
 * срезается многоточием; в компакт-режиме сборный поезд (подгруппы с разными
 * index_main) схлопывается в «сборный (Nв)» курсивом — эталон compact gtport.
 * Полный состав — в тултипе строки.
 *
 * Шапка: остатки терминала, счётчики поездов/брошенных, кликабельные бейджи
 * риска ⚠/!/~ — клик подсвечивает строки этого уровня.
 *
 * Этап 2: ПКМ по строке — диалог what-if правки; строки свободных ниток 🟢
 * встраиваются в хронологию, клик по нитке выделяет её и подсвечивает
 * подходящие поезда (rasch ≤ слот + 3ч — эталон SLOT_TOLERANCE_H).
 */

type RiskLevel = 'critical' | 'warning' | 'watch';

interface Risk {
  level: RiskLevel;
  reason: string;
}

interface Row {
  train: GtTrain;
  risk: Risk | null;
  /** Состав одной строкой: «станция (Nв) · …» либо «сборный (Nв)». */
  compose: string;
  /** Сборный в компакте — курсив (эталон compact+isMixed gtport). */
  mixed: boolean;
  /** Цвет клиента первой подгруппы терминала (эталон sg[0].color). */
  composeColor: string;
  /** Полный тултип строки: индекс + подгруппы с маршрутами. */
  tip: string;
  arrival: string;
  arrivalColor: string;
  arrivalBold: boolean;
  path: string;
  mistake: string;
  mistakeColor: string;
  wait: string;
  waitColor: string;
  waitBold: boolean;
  fitsSlot: boolean;
}

/** Строка таблицы: поезд либо свободная нитка, в общей хронологии ЖД. */
interface Line {
  jdKey: string;
  row?: Row;
  slot?: GtFreeSlot;
}

const RISK_COLOR: Record<RiskLevel, string> = {
  critical: '#c62828',
  warning: '#e65100',
  watch: '#f9a825',
};

/** Допуск подбора поезда на нитку, часов (эталон SLOT_TOLERANCE_H). */
const SLOT_TOLERANCE_H = 3;

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
            @for (ln of lines(); track ln.jdKey + (ln.row?.train?.index ?? 'slot')) {
              @if (ln.slot; as s) {
                <tr class="slot-row" [class.sel]="selectedSlot()?.msk === s.msk"
                    (click)="onSlotClick(s)"
                    title="Свободная нитка — клик для подбора поездов">
                  <td class="c-train slot-cell">🟢 свободная нитка
                    @if (selectedSlot()?.msk === s.msk && fitCount() > 0) {
                      <span class="fit">({{ fitCount() }} подх.)</span>
                    }
                  </td>
                  <td class="slot-cell b">{{ fmtJdStr(s.jd) }}</td>
                  <td class="slot-cell">—</td>
                  <td class="slot-cell">—</td>
                  <td class="slot-cell">—</td>
                </tr>
              } @else if (ln.row; as r) {
                <tr [class.arrived]="r.train.is_arrived"
                    class="train-row"
                    [style.background]="rowBg(r)"
                    (click)="trainSelect.emit(r.train.index)"
                    (contextmenu)="onContext($event, r.train)">
                  <td class="c-train">
                    <div class="line" [title]="r.tip + (r.risk ? '\n⚠ ' + r.risk.reason : '')">
                      @if (r.risk) {
                        <span class="dot" [style.background]="riskColor[r.risk.level]"></span>
                      }
                      <b>{{ shortIndex(r.train.index) }}</b>
                      @if (r.train.delay_hours && !r.train.is_arrived) {
                        <span class="delay">+{{ r.train.delay_hours }}ч</span>
                      }
                      <span class="compose" [class.mixed]="r.mixed"
                            [style.color]="r.composeColor">{{ r.compose }}</span>
                    </div>
                  </td>
                  <td [style.color]="r.arrivalColor" [class.b]="r.arrivalBold">{{ r.arrival }}</td>
                  <td>{{ r.path }}</td>
                  <td [style.color]="r.mistakeColor" class="b">{{ r.mistake }}</td>
                  <td [style.color]="r.waitColor" [class.b]="r.waitBold">{{ r.wait }}</td>
                </tr>
              }
            } @empty {
              <tr><td colspan="5" class="empty">Нет поездов</td></tr>
            }
          </tbody>
        </table>
      </div>
      <div class="hint">клик — выделить поезд · клик по 🟢 нитке — подбор поездов · ПКМ — правка</div>
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
    .dp-tbl-wrap { max-height: 66vh; overflow: auto; }
    .dp-tbl { table-layout: fixed; width: 100%; }
    .dp-tbl td, .dp-tbl th { font-size: 11px; padding: 2px 4px; text-align: center;
                             white-space: nowrap; }
    .dp-tbl th:nth-child(2) { width: 70px; }
    .dp-tbl th:nth-child(3) { width: 34px; }
    .dp-tbl th:nth-child(4) { width: 44px; }
    .dp-tbl th:nth-child(5) { width: 40px; }
    .c-train { text-align: left; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
    .line { display: flex; align-items: center; gap: 4px; white-space: nowrap; overflow: hidden; }
    .delay { color: #c62828; font-size: 10px; font-weight: 600; flex: none; }
    .dot { width: 8px; height: 8px; border-radius: 50%; display: inline-block; flex: none; }
    .compose { font-size: 10px; overflow: hidden; text-overflow: ellipsis; }
    .compose.mixed { font-style: italic; }
    .b { font-weight: 600; }
    .arrived { opacity: 0.85; }
    .empty { color: #888; padding: var(--space-md); }
    .train-row { cursor: pointer; }
    .slot-row { cursor: pointer; }
    .slot-cell { color: #2e7d32; background: rgba(46,125,50,0.05); }
    .slot-row.sel .slot-cell { background: rgba(46,125,50,0.18); font-weight: 600; }
    .fit { color: #1b5e20; }
    .hint { font-size: 10px; color: #999; padding: 2px var(--space-sm); }
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
  /** Компакт (две таблицы рядом): сборный состав схлопнут в «сборный (Nв)». */
  readonly compact = input<boolean>(false);
  /** Свободные нитки станции (пустой список = показ выключен). */
  readonly slots = input<GtFreeSlot[]>([]);
  readonly selectedSlot = input<GtFreeSlot | null>(null);

  /** Выделенный поезд (синхронно с диаграммой). */
  readonly selectedTrain = input<string | null>(null);

  readonly slotSelect = output<GtFreeSlot | null>();
  readonly editTrain = output<GtTrain>();
  readonly trainSelect = output<string>();

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
      // Подгруппы СВОЕГО терминала (поезд может везти и на соседний).
      const own = t.sub_groups.filter((sg) => sg.naznach === this.title());
      const groups = own.length > 0 ? own : t.sub_groups;
      const count = groups.reduce((s, g) => s + g.vagon_count, 0);
      const mains = new Set(groups.map((g) => g.index_main).filter(Boolean));
      const mixed = groups.length > 1 && mains.size > 1;
      const full = groups.map((g) => `${g.station_nach || '—'} (${g.vagon_count}в)`).join(' · ');
      return {
        train: t,
        risk: risk(t),
        compose: this.compact() && mixed ? `сборный (${count}в)` : full,
        mixed,
        composeColor: this.black() ? '#111' : (groups[0]?.color || '#111'),
        tip: [
          t.index,
          ...groups.map((g) =>
            `${g.station_nach || '—'} → ${g.naznach} · ${g.vagon_count}в` +
            (g.cargo_group ? ` · ${g.cargo_group}` : '') +
            (g.is_universal ? ' · универсальный' : '')),
        ].join('\n'),
        arrival: fmtJd(t.prog_jd),
        arrivalColor: t.is_arrived ? '#1565c0' : t.status === '5' ? '#c62828' : t.plan_jd ? '#2e7d32' : '',
        arrivalBold: !!t.plan_jd || t.status === '5',
        path: fmtPath(t),
        mistake: fmtMistake(t.mistake),
        mistakeColor: mistakeColor(t.mistake),
        wait: waitMin > 0 ? waitH.toFixed(1) + 'ч' : '',
        waitColor: waitH > 12 ? '#c62828' : waitH > 6 ? '#e65100' : waitMin > 0 ? '#2e7d32' : '',
        waitBold: waitH > 6,
        fitsSlot: this.fitsSelected(t),
      };
    })
  );

  /** Поезда и свободные нитки в единой ФИЗИЧЕСКОЙ хронологии: ключ — расчётная
   *  шкала (−18ч/+6ч), а не сырое ЖД-время. ЖД ≥ 18:00 — вечер предыдущих
   *  физических суток и идёт РАНЬШЕ утренних времён тех же ЖД-суток
   *  (замечание владельца 05.08.2026: сортировка по сырому ЖД врала). */
  readonly lines = computed<Line[]>(() => {
    const out: Line[] = this.rows().map((row) => ({ jdKey: calcKey(row.train.prog_jd), row }));
    for (const slot of this.slots()) {
      out.push({ jdKey: calcKey(slot.jd), slot });
    }
    out.sort((a, b) => a.jdKey.localeCompare(b.jdKey));
    return out;
  });

  readonly thrownCount = computed(() => this.trains().filter((t) => t.status === '5').length);

  readonly riskCount = computed<Record<RiskLevel, number>>(() => {
    const c: Record<RiskLevel, number> = { critical: 0, warning: 0, watch: 0 };
    for (const r of this.rows()) if (r.risk) c[r.risk.level]++;
    return c;
  });

  readonly fitCount = computed(() => this.rows().filter((r) => r.fitsSlot).length);

  toggleFilter(lvl: RiskLevel): void {
    this.riskFilter.set(this.riskFilter() === lvl ? null : lvl);
  }

  onSlotClick(s: GtFreeSlot): void {
    this.slotSelect.emit(this.selectedSlot()?.msk === s.msk ? null : s);
  }

  onContext(e: MouseEvent, t: GtTrain): void {
    e.preventDefault();
    if (!t.is_arrived) this.editTrain.emit(t);
  }

  /** Подходит ли поезд на выбранную нитку: rasch ≤ слот + 3ч, не в плане, не прибыл. */
  private fitsSelected(t: GtTrain): boolean {
    const s = this.selectedSlot();
    if (!s || t.is_arrived || t.plan_jd) return false;
    const rasch = t.rasch_msk ?? t.rasch_jd;
    if (!rasch) return false;
    return Date.parse(rasch + 'Z') <= Date.parse(s.msk + 'Z') + SLOT_TOLERANCE_H * 3600000;
  }

  rowBg(r: Row): string {
    if (r.train.index === this.selectedTrain()) return '#e3f2fd'; // выделение — как gtport
    if (r.fitsSlot) return 'rgba(46,125,50,0.15)';
    const f = this.riskFilter();
    if (!f || r.risk?.level !== f) return '';
    return { critical: 'rgba(198,40,40,0.10)', warning: 'rgba(230,81,0,0.10)', watch: 'rgba(249,168,37,0.12)' }[f];
  }

  shortIndex = shortIndex;
  fmtJdStr = fmtJd;
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

/** ЖД-время → ключ расчётной шкалы (час ≥ 18 → −18ч, иначе +6ч), ISO-строкой. */
function calcKey(jd: string | null): string {
  if (!jd) return '';
  const d = new Date(jd.slice(0, 19) + 'Z'); // naive как UTC — только для арифметики
  d.setUTCHours(d.getUTCHours() + (d.getUTCHours() >= 18 ? -18 : 6));
  return d.toISOString();
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
