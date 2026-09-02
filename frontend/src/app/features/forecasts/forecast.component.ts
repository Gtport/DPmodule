import { Component, OnInit, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { NzRadioModule } from 'ng-zorro-antd/radio';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { ArrivalsApiService, TerminalTarget } from '../home/arrivals-api.service';
import {
  ForecastApiService,
  ForecastBoardDTO,
  ForecastLine,
} from './forecast-api.service';
import { loadXlsx } from '../../shared/xlsx';

/**
 * Экран «Новый прогноз» (перенос gtport PrognozNew).
 *
 * Строка таблицы — подгруппа поезда (индекс × станция отправления × дата
 * погрузки), разложенная по колонкам-датам (прогноз прибытия prog_jd; у уже
 * прибывших — факт). Пять итоговых строк на каждую дату: «В пути» (прибывает за
 * день), «ВСЕГО» (остаток + прибывает, накопительно), «Выгрузка»
 * (min(ВСЕГО, норма)), «Остаток» (переходит на следующий день), «Все едет»
 * (контроль без ручных смещений).
 *
 * Отличия от эталона — только источники (расхардкоживание): подтаблицы и нормы
 * выгрузки — из справочника port_cargo_line (в gtport — switch по портам и
 * gt_port_speeds), «Остаток на 18:00» — вчерашний остаток «Грузовой работы»
 * (в gtport — таблицы vigr_*), сводный режим — станция с несколькими
 * терминалами (в gtport — захардкоженный «Мыс Астафьева»).
 *
 * Смещения выгрузки (стрелки) и правки нормы — только в текущем сеансе,
 * в БД не пишутся (как в gtport).
 */

/** Ручное смещение выгрузки строки: исходная и новая колонка-дата (ДД.ММ). */
interface ShiftInfo {
  originalDate: string;
  shiftedDate: string;
}

/** Строка таблицы: слитая подгруппа (прогноз или прибывший). */
interface BoardRow {
  id: string; // стабильный ключ: таблица|индекс|станция отправления|дата погрузки
  trainIndex: string;
  trainStationOper: string; // станция операции; у прибывших — «Прибыл»
  trainDate: string; // полная дата (prog_jd / date_prib) для сортировки
  trainDateColumn: string; // ДД.ММ — колонка раскладки
  trainColor: string;
  trainStatus: number | null;
  isHistory: boolean;
  stationNach: string;
  dateNach: string;
  vagonCount: number;
}

/** Подтаблица: линия выгрузки терминала (или весь терминал одной таблицей). */
interface BoardTable {
  id: string; // терминал|cargo_key — он же ключ нормы выгрузки
  title: string;
  terminal: string;
  cargoKey: string;
  rows: BoardRow[];
  initialRemainder: number; // «Остаток на 18:00» прошлых суток
}

/** Итоговые строки таблицы по колонкам-датам (порядок = dateColumns). */
interface SummaryValues {
  total: number[];
  unload: number[];
  remainder: number[];
  originalTotal: number[];
  inTransit: number[];
}

/** Пункт переключателя: терминал или сводка станции (несколько терминалов). */
interface FilterOption {
  key: string;
  label: string;
  terminals: string[];
  summary: boolean;
}

/** Ключ слияния строк — как в gtport: индекс|станция отправления|дата погрузки. */
const rowKey = (index: string, stationNach: string, dateNach: string | null): string =>
  `${index}|${stationNach}|${dateNach ?? ''}`;

@Component({
  selector: 'app-forecast',
  imports: [FormsModule, NzRadioModule, NzButtonModule, NzIconModule, NzSpinModule],
  template: `
    <div class="page">
      <div class="bar">
        <span class="ttl">Новый прогноз</span>
        <nz-radio-group [ngModel]="filter()" (ngModelChange)="setFilter($event)" nzSize="small">
          @for (o of options(); track o.key) {
            <label nz-radio [nzValue]="o.key">{{ o.label }}</label>
          }
        </nz-radio-group>
        <span class="spacer"></span>
        <button nz-button nzSize="small" title="Обновить данные" (click)="reload()" [disabled]="loading()">
          <span nz-icon nzType="reload"></span>
        </button>
        <button nz-button nzSize="small" title="Экспортировать в Excel"
                (click)="exportExcel()" [disabled]="tables().length === 0">
          <span nz-icon nzType="download"></span>
        </button>
      </div>

      <div class="stats">
        <span>
          @if (board(); as b) {
            Прогноз: <b>{{ prognozCount() }}</b> | Прибыло: <b>{{ arrivedCount() }}</b>
            @if (!isSummary() && activeTable(); as t) {
              | Остаток на 18:00: <b>{{ t.initialRemainder }}</b>
            }
          } @else {
            Нет данных для отображения
          }
        </span>
        <span class="range">
          @if (dateColumns().length > 0) {
            Диапазон дат: {{ dateColumns()[0] }} - {{ dateColumns()[dateColumns().length - 1] }}
          }
        </span>
      </div>

      @if (!isSummary() && tables().length > 1) {
        <div class="subtabs">
          @for (t of tables(); track t.id; let i = $index) {
            <button [class.on]="i === activeTab()" (click)="activeTab.set(i)">
              {{ t.title }} ({{ t.rows.length }})
            </button>
          }
        </div>
      }

      <div class="scroll">
        @if (loading()) {
          <div class="empty"><nz-spin nzSimple /></div>
        } @else if (tables().length === 0) {
          <div class="empty">По выбранному фильтру нет данных</div>
        } @else {
          @for (t of visibleTables(); track t.id) {
            @if (isSummary()) {
              <div class="tbl-head">
                <b>{{ t.title }}</b>
                <span>Остаток на 18:00: <b>{{ t.initialRemainder }}</b> · Подгрупп: {{ t.rows.length }}</span>
              </div>
            }
            <table class="board">
              <thead>
                <tr>
                  <th>Индекс</th>
                  <th>Станция</th>
                  <th>Дата</th>
                  <th>Станция отправления</th>
                  <th>Дата погрузки</th>
                  <th class="num">Кол-во</th>
                  <th class="shift-col">Смещение</th>
                  @for (d of dateColumns(); track d) {
                    <th class="date">{{ d }}</th>
                  }
                </tr>
              </thead>
              <tbody>
                @for (r of t.rows; track r.id) {
                  <tr [class.arrived]="r.isHistory">
                    <td [style.color]="r.trainColor" class="idx">{{ r.trainIndex }}</td>
                    <td [style.color]="rowColor(r)">{{ r.trainStationOper }}</td>
                    <td [style.color]="rowColor(r)">{{ fmtFull(r.trainDate) }}</td>
                    <td [style.color]="rowColor(r)">{{ r.stationNach }}</td>
                    <td [style.color]="rowColor(r)">{{ fmtFull(r.dateNach) }}</td>
                    <td class="num" [style.color]="rowColor(r)">{{ r.vagonCount }}</td>
                    <td class="shift-col">
                      <button class="arr" (click)="shiftLeft(r)" [disabled]="!canShiftLeft(r)">‹</button>
                      <button class="arr" (click)="shiftRight(r)" [disabled]="!canShiftRight(r)">›</button>
                      @if (shifts()[r.id]; as s) {
                        <button class="arr reset" title="Сбросить смещение" (click)="resetShift(r.id)">✕</button>
                        <span class="shift-note">{{ s.originalDate }} → {{ s.shiftedDate }}</span>
                      }
                    </td>
                    @for (d of dateColumns(); track d) {
                      <td class="date" [class.hl]="highlightCell(r, d)"
                          [class.act]="actualDate(r) === d" [style.color]="rowColor(r)">
                        {{ actualDate(r) === d ? r.vagonCount : '' }}
                      </td>
                    }
                  </tr>
                }
                @if (summaryFor(t); as s) {
                  <tr class="sum bold gray">
                    <td [attr.colspan]="7">ВСЕГО</td>
                    @for (v of s.total; track $index) {
                      <td class="date" [style.background]="heat(v, speedOf(t))">{{ v }}</td>
                    }
                  </tr>
                  <tr class="sum">
                    <td [attr.colspan]="7" class="speed-cell">
                      Выгрузка
                      <input type="number" min="0" [value]="speedOf(t)"
                             (input)="setSpeed(t.id, $event)" />
                    </td>
                    @for (v of s.unload; track $index) {
                      <td class="date">{{ v }}</td>
                    }
                  </tr>
                  <tr class="sum ost">
                    <td [attr.colspan]="7">Остаток</td>
                    @for (v of s.remainder; track $index) {
                      <td class="date">{{ v }}</td>
                    }
                  </tr>
                  <tr class="sum bold">
                    <td [attr.colspan]="7">Все едет</td>
                    @for (v of s.originalTotal; track $index) {
                      <td class="date" [style.background]="heat(v, speedOf(t))">{{ v }}</td>
                    }
                  </tr>
                  <tr class="sum bold">
                    <td [attr.colspan]="7">В пути</td>
                    @for (v of s.inTransit; track $index) {
                      <td class="date">{{ v }}</td>
                    }
                  </tr>
                }
              </tbody>
            </table>
          }
        }
      </div>

      <div class="foot">
        @if (tables().length > 0) {
          @if (isSummary()) {
            Таблиц: {{ tables().length }} · Всего подгрупп: {{ totalRows() }}
          } @else {
            Подгрупп в таблице: {{ activeTable()?.rows?.length ?? 0 }}
          }
        } @else {
          Нет данных
        }
      </div>
    </div>
  `,
  styles: [`
    .page { display: flex; flex-direction: column; gap: var(--space-sm); height: 100%; min-height: 0; }
    .bar { display: flex; align-items: center; gap: var(--space-sm); flex-wrap: wrap; }
    .ttl { font-weight: 600; }
    .spacer { flex: 1 1 auto; }
    .stats { display: flex; justify-content: space-between; align-items: center;
             font-size: var(--font-size-sm); color: var(--color-text-secondary);
             background: var(--color-bg-surface); border-radius: var(--radius-card);
             padding: 2px 10px; box-shadow: var(--shadow-card); }
    .subtabs { display: flex; gap: 4px; }
    .subtabs button { border: 1px solid #d9d9d9; background: var(--color-bg-surface);
                      border-radius: 4px 4px 0 0; padding: 2px 10px; font-size: var(--font-size-sm);
                      cursor: pointer; }
    .subtabs button.on { background: #1677ff; color: #fff; border-color: #1677ff; }
    .scroll { flex: 1 1 auto; min-height: 0; overflow: auto; background: var(--color-bg-surface);
              border-radius: var(--radius-card); box-shadow: var(--shadow-card); }
    .empty { padding: 40px; text-align: center; color: var(--color-text-secondary); }
    .tbl-head { display: flex; gap: 12px; align-items: center; padding: 4px 10px;
                background: #eef2f7; border-top: 2px solid #90a4c4; border-bottom: 1px solid #cfd8e3;
                position: sticky; left: 0; font-size: var(--font-size-sm); }
    table.board { border-collapse: collapse; width: 100%; font-size: 12px; margin-bottom: 14px; }
    table.board th { position: sticky; top: 0; z-index: 1; background: #fafafa;
                     border-bottom: 1px solid #e0e0e0; padding: 4px 6px; text-align: left;
                     white-space: nowrap; font-weight: 600; }
    table.board td { padding: 1px 6px; border-bottom: 1px solid #f0f0f0; white-space: nowrap; }
    td.idx { font-weight: 500; font-variant-numeric: tabular-nums; }
    th.num, td.num { text-align: center; }
    th.date, td.date { text-align: center; min-width: 46px; font-variant-numeric: tabular-nums; }
    td.date.act { background: #e8f5e8; font-weight: 600; }
    td.date.hl { background: #fff9c4; }
    tr.arrived td { background: #f8f8f8; opacity: 0.85; }
    .shift-col { white-space: nowrap; }
    button.arr { border: none; background: none; cursor: pointer; padding: 0 3px;
                 font-size: 13px; line-height: 1; color: var(--color-text-secondary); }
    button.arr:disabled { opacity: 0.3; cursor: default; }
    button.arr.reset { color: #ff4d4f; font-size: 11px; }
    .shift-note { font-size: 10px; color: #666; margin-left: 2px; }
    tr.sum td { border-top: 1px solid #e0e0e0; padding: 3px 6px; }
    tr.sum.bold td { font-weight: 600; }
    tr.sum.gray td:first-child { background: #f0f0f0; }
    tr.sum.ost td { background: #fafafa; font-weight: 500; }
    .speed-cell input { width: 60px; font-size: 12px; padding: 0 4px;
                        border: 1px solid #d9d9d9; border-radius: 4px; margin-left: 6px; }
    .foot { font-size: var(--font-size-sm); color: var(--color-text-secondary); padding: 0 4px; }
  `],
})
export class ForecastComponent implements OnInit {
  private readonly api = inject(ForecastApiService);
  private readonly arrivalsApi = inject(ArrivalsApiService);
  /** Уведомления — всплывающие тосты с автоуборкой (договорённость), не баннеры. */
  private readonly msg = inject(NzMessageService);

  readonly board = signal<ForecastBoardDTO | null>(null);
  readonly targets = signal<TerminalTarget[]>([]);
  readonly loading = signal(false);
  readonly filter = signal<string>('');
  readonly activeTab = signal(0);
  /** Смещения выгрузки — только в сеансе (ключ — id строки). */
  readonly shifts = signal<Record<string, ShiftInfo>>({});
  /** Нормы выгрузки: старт — pc линии, правки живут до перезагрузки страницы. */
  readonly speeds = signal<Record<string, number>>({});

  /** Пункты переключателя: терминалы + сводки станций с ≥2 терминалами. */
  readonly options = computed<FilterOption[]>(() => {
    const ts = this.targets();
    const out: FilterOption[] = ts.map((t) => ({
      key: t.name, label: t.name, terminals: [t.name], summary: false,
    }));
    const byStation = new Map<string, TerminalTarget[]>();
    for (const t of ts) {
      if (!t.station) continue;
      byStation.set(t.station, [...(byStation.get(t.station) ?? []), t]);
    }
    for (const [station, terms] of byStation) {
      if (terms.length < 2) continue;
      out.push({
        key: `st:${station}`,
        label: this.stationTitle(station),
        terminals: terms.map((t) => t.name),
        summary: true,
      });
    }
    return out;
  });

  readonly isSummary = computed(() =>
    this.options().find((o) => o.key === this.filter())?.summary ?? false,
  );

  private readonly selectedTerminals = computed<string[]>(() =>
    this.options().find((o) => o.key === this.filter())?.terminals ?? [],
  );

  /** Подтаблицы выбранного пункта: линии выгрузки терминалов + слитые строки. */
  readonly tables = computed<BoardTable[]>(() => {
    const b = this.board();
    if (!b) return [];
    const out: BoardTable[] = [];
    for (const term of this.selectedTerminals()) {
      const lines = b.lines.filter((l) => l.terminal === term);
      const single = lines.length === 1 && lines[0].cargo_key === '';
      const used = new Set<string>();
      for (const line of lines) {
        const match = single ? null : line.cargo_key;
        const rows = this.buildRows(b, term, line.cargo_key, (cg) => single || cg === match);
        if (match !== null) used.add(match);
        if (rows.length > 0 || lines.length === 1) {
          out.push(this.makeTable(term, line, rows, lines.length > 1));
        }
      }
      if (!single) {
        // Группы груза, не описанные линиями терминала: gtport их молча терял —
        // здесь показываем отдельной таблицей «Прочее» (норма 0, остаток 0).
        const rows = this.buildRows(b, term, '~', (cg) => !used.has(cg));
        if (rows.length > 0) {
          out.push({
            id: `${term}|~`, title: `${term} Прочее`, terminal: term,
            cargoKey: '~', rows, initialRemainder: 0,
          });
        }
      }
    }
    return out;
  });

  readonly visibleTables = computed<BoardTable[]>(() => {
    const ts = this.tables();
    if (this.isSummary()) return ts;
    const i = Math.min(this.activeTab(), ts.length - 1);
    return i >= 0 ? [ts[i]] : [];
  });

  readonly activeTable = computed<BoardTable | null>(() => this.visibleTables()[0] ?? null);

  readonly totalRows = computed(() => this.tables().reduce((s, t) => s + t.rows.length, 0));

  /** Сплошной диапазон дат ДД.ММ (min..max, без пропусков — иначе выгрузка за
   *  «пустой» день не начислялась бы и остаток завышался; правило gtport). */
  readonly dateColumns = computed<string[]>(() => {
    let min = '';
    let max = '';
    for (const t of this.tables()) {
      for (const r of t.rows) {
        const iso = r.trainDate.slice(0, 10);
        if (iso.length < 10) continue;
        if (!min || iso < min) min = iso;
        if (!max || iso > max) max = iso;
      }
    }
    if (!min) return [];
    const out: string[] = [];
    const cur = new Date(`${min}T00:00:00`);
    const end = new Date(`${max}T00:00:00`);
    while (cur <= end) {
      out.push(`${String(cur.getDate()).padStart(2, '0')}.${String(cur.getMonth() + 1).padStart(2, '0')}`);
      cur.setDate(cur.getDate() + 1);
    }
    return out;
  });

  /** «Прогноз: N» — поезда в подходе с вагонами на выбранные терминалы. */
  readonly prognozCount = computed(() => {
    const b = this.board();
    if (!b) return 0;
    const sel = new Set(this.selectedTerminals());
    return b.groups.filter((g) => g.sub_groups.some((sg) => sel.has(sg.naznach))).length;
  });

  /** «Прибыло: N» — прибывшие за сутки поезда выбранных терминалов. */
  readonly arrivedCount = computed(() => {
    const b = this.board();
    if (!b) return 0;
    const sel = new Set(this.selectedTerminals());
    return b.arrived.filter((g) => g.sub_groups.some((sg) => sel.has(sg.naznach))).length;
  });

  ngOnInit(): void {
    void this.load(true);
  }

  async load(first: boolean): Promise<void> {
    this.loading.set(true);
    try {
      const [targets, board] = await Promise.all([
        first ? this.arrivalsApi.getTerminals() : Promise.resolve(this.targets()),
        this.api.getBoard(),
      ]);
      this.targets.set(targets);
      this.board.set(board);
      // Нормы: новые ключи — из pc линии; ручные правки сеанса не затираем
      // (правило gtport: правка живёт до перезагрузки страницы).
      const speeds = { ...this.speeds() };
      for (const l of board.lines) {
        const key = this.lineKey(l.terminal, l.cargo_key);
        if (!(key in speeds)) speeds[key] = l.pc;
      }
      this.speeds.set(speeds);
      if (first && !this.filter() && this.options().length > 0) {
        this.filter.set(this.options()[0].key);
      }
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.loading.set(false);
    }
  }

  reload(): void {
    this.shifts.set({});
    void this.load(false);
  }

  setFilter(key: string): void {
    this.filter.set(key);
    this.activeTab.set(0);
    this.shifts.set({});
  }

  // ── Сборка строк ───────────────────────────────────────────────────────────

  /** Слияние прогнозных и прибывших подгрупп терминала в строки таблицы. */
  private buildRows(
    b: ForecastBoardDTO,
    terminal: string,
    tableKey: string,
    takes: (cargoGroup: string) => boolean,
  ): BoardRow[] {
    const map = new Map<string, BoardRow>();
    for (const g of b.groups) {
      for (const sg of g.sub_groups) {
        if (sg.naznach !== terminal || !takes(sg.cargo_group)) continue;
        const key = rowKey(g.index, sg.station_nach, sg.date_nach);
        const found = map.get(key);
        if (found) {
          found.vagonCount += sg.vagon_count;
          continue;
        }
        map.set(key, {
          id: `${terminal}|${tableKey}|${key}`,
          trainIndex: g.index,
          trainStationOper: g.station_oper,
          trainDate: g.prog_jd ?? '',
          trainDateColumn: this.fmtColumn(g.prog_jd),
          trainColor: sg.color || '#000000',
          trainStatus: g.status,
          isHistory: false,
          stationNach: sg.station_nach,
          dateNach: sg.date_nach ?? '',
          vagonCount: sg.vagon_count,
        });
      }
    }
    for (const g of b.arrived) {
      for (const sg of g.sub_groups) {
        if (sg.naznach !== terminal || !takes(sg.cargo_group)) continue;
        const key = rowKey(g.index_pp, sg.station_nach, sg.date_nach);
        const found = map.get(key);
        if (found) {
          found.vagonCount += sg.vagon_count;
          continue;
        }
        map.set(key, {
          id: `${terminal}|${tableKey}|${key}`,
          trainIndex: g.index_pp,
          trainStationOper: 'Прибыл',
          trainDate: g.date_prib ?? '',
          trainDateColumn: this.fmtColumn(g.date_prib),
          trainColor: sg.color || '#000000',
          trainStatus: null,
          isHistory: true,
          stationNach: sg.station_nach,
          dateNach: sg.date_nach ?? '',
          vagonCount: sg.vagon_count,
        });
      }
    }
    const rows = [...map.values()];
    rows.sort((a, b2) => {
      if (!a.trainDate) return 1;
      if (!b2.trainDate) return -1;
      return this.railwayTime(a.trainDate).getTime() - this.railwayTime(b2.trainDate).getTime();
    });
    return rows;
  }

  private makeTable(terminal: string, line: ForecastLine, rows: BoardRow[], multi: boolean): BoardTable {
    return {
      id: this.lineKey(terminal, line.cargo_key),
      title: multi && line.label ? `${terminal} ${line.label}` : terminal,
      terminal,
      cargoKey: line.cargo_key,
      rows,
      initialRemainder: line.ost,
    };
  }

  private lineKey(terminal: string, cargoKey: string): string {
    return `${terminal}|${cargoKey}`;
  }

  /** Имя сводки: станция как есть (метка пункта «все терминалы станции»). */
  private stationTitle(station: string): string {
    return station
      .toLowerCase()
      .replace(/(^|[\s-])([а-яёa-z])/g, (m) => m.toUpperCase());
  }

  // ── Итоги (перенос updateSummaryRows из gtport) ────────────────────────────

  summaryFor(t: BoardTable): SummaryValues {
    const dates = this.dateColumns();
    const shifts = this.shifts();
    const speed = this.speedOf(t);
    const s: SummaryValues = { total: [], unload: [], remainder: [], originalTotal: [], inTransit: [] };
    let runningRemainder = t.initialRemainder;
    let runningOriginal = t.initialRemainder;
    for (const date of dates) {
      const daily = t.rows
        .filter((r) => (shifts[r.id]?.shiftedDate || r.trainDateColumn) === date)
        .reduce((sum, r) => sum + r.vagonCount, 0);
      const originalDaily = t.rows
        .filter((r) => r.trainDateColumn === date)
        .reduce((sum, r) => sum + r.vagonCount, 0);

      const total = runningRemainder + daily;
      const originalTotal = runningOriginal + originalDaily;
      const unloaded = Math.min(total, speed);
      const remainder = total - unloaded;

      s.inTransit.push(daily);
      s.total.push(total);
      s.originalTotal.push(originalTotal);
      s.unload.push(unloaded);
      s.remainder.push(remainder);

      runningRemainder = remainder;
      runningOriginal = originalTotal - unloaded;
    }
    return s;
  }

  speedOf(t: BoardTable): number {
    return this.speeds()[t.id] ?? 0;
  }

  setSpeed(tableId: string, ev: Event): void {
    const v = parseInt((ev.target as HTMLInputElement).value, 10);
    if (!isNaN(v) && v >= 0) {
      this.speeds.set({ ...this.speeds(), [tableId]: v });
    }
  }

  /** Цвет «ВСЕГО»/«Все едет»: зелёный в норме, дальше — по проценту превышения. */
  heat(value: number, speed: number): string {
    if (value <= speed) return '#e8f5e8';
    if (speed <= 0) return '#ffcdd2';
    const excess = ((value - speed) / speed) * 100;
    if (excess <= 25) return '#fff9c4';
    if (excess <= 50) return '#ffe0b2';
    return '#ffcdd2';
  }

  // ── Смещения выгрузки ──────────────────────────────────────────────────────

  actualDate(r: BoardRow): string {
    return this.shifts()[r.id]?.shiftedDate || r.trainDateColumn;
  }

  canShiftLeft(r: BoardRow): boolean {
    const dates = this.dateColumns();
    const i = dates.indexOf(this.actualDate(r));
    return i > 0 && i - 1 >= dates.indexOf(r.trainDateColumn);
  }

  canShiftRight(r: BoardRow): boolean {
    const dates = this.dateColumns();
    const i = dates.indexOf(this.actualDate(r));
    return i >= 0 && i < dates.length - 1;
  }

  shiftLeft(r: BoardRow): void {
    const dates = this.dateColumns();
    const i = dates.indexOf(this.actualDate(r));
    if (i <= 0) return;
    if (i - 1 < dates.indexOf(r.trainDateColumn)) {
      // Нельзя выгрузить раньше, чем поезд прибыл.
      this.msg.warning('Нельзя смещать раньше даты прибытия');
      return;
    }
    this.shifts.set({
      ...this.shifts(),
      [r.id]: { originalDate: r.trainDateColumn, shiftedDate: dates[i - 1] },
    });
  }

  shiftRight(r: BoardRow): void {
    const dates = this.dateColumns();
    const i = dates.indexOf(this.actualDate(r));
    if (i < 0 || i >= dates.length - 1) return;
    this.shifts.set({
      ...this.shifts(),
      [r.id]: { originalDate: r.trainDateColumn, shiftedDate: dates[i + 1] },
    });
  }

  resetShift(rowId: string): void {
    const next = { ...this.shifts() };
    delete next[rowId];
    this.shifts.set(next);
  }

  /** Жёлтая подсветка пути смещения: от исходной колонки до новой. */
  highlightCell(r: BoardRow, date: string): boolean {
    const shift = this.shifts()[r.id];
    if (!shift) return false;
    const dates = this.dateColumns();
    const a = dates.indexOf(r.trainDateColumn);
    const b = dates.indexOf(shift.shiftedDate);
    const i = dates.indexOf(date);
    return i >= Math.min(a, b) && i <= Math.max(a, b);
  }

  // ── Форматирование ─────────────────────────────────────────────────────────

  /** Брошенный поезд (статус 5) — красным; прочее — цвет подгруппы. */
  rowColor(r: BoardRow): string {
    return r.trainStatus === 5 && !r.isHistory ? '#ff4d4f' : r.trainColor;
  }

  /** «2026-07-28…» → «28.07» (колонка раскладки). */
  private fmtColumn(ts: string | null): string {
    if (!ts || ts.length < 10) return '';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)}`;
  }

  /** «2026-07-28…» → «28.07.2026». */
  fmtFull(ts: string): string {
    if (!ts || ts.length < 10) return '';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)}.${ts.slice(0, 4)}`;
  }

  /** ЖД-время для сортировки: час ≥ 18 → −18ч, иначе +6ч (бизнес-правило). */
  private railwayTime(ts: string): Date {
    const y = Number(ts.slice(0, 4));
    const mo = Number(ts.slice(5, 7));
    const d = Number(ts.slice(8, 10));
    const h = Number(ts.slice(11, 13)) || 0;
    const mi = Number(ts.slice(14, 16)) || 0;
    const date = new Date(y, mo - 1, d, h, mi, 0);
    date.setHours(h >= 18 ? h - 18 : h + 6, mi, 0);
    return date;
  }

  // ── Экспорт в Excel (перенос exportToExcel из gtport) ──────────────────────

  async exportExcel(): Promise<void> {
    const tables = this.tables();
    const dates = this.dateColumns();
    if (tables.length === 0) return;
    const singleSheet = this.isSummary();
    try {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const XLSX: any = await loadXlsx();
      const wb = XLSX.utils.book_new();
      const numCols = 6 + dates.length;
      const colWidths = [
        { wch: 20 }, { wch: 28 }, { wch: 12 }, { wch: 27 }, { wch: 12 }, { wch: 8 },
        ...dates.map(() => ({ wch: 8 })),
      ];
      const border = { style: 'thin', color: { rgb: '000000' } };
      const heatRgb = (value: number, speed: number): string => {
        if (value <= speed) return 'C8E6C9';
        if (speed <= 0) return 'FFCDD2';
        const excess = ((value - speed) / speed) * 100;
        if (excess <= 25) return 'FFF9C4';
        if (excess <= 50) return 'FFE0B2';
        return 'FFCDD2';
      };

      // Тело таблицы: заголовок, строки, 5 итоговых строк (порядок gtport).
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const buildAoa = (t: BoardTable): { aoa: any[][]; s: SummaryValues } => {
        const s = this.summaryFor(t);
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const aoa: any[][] = [
          ['Индекс', 'Станция', 'Дата', 'Станция отправления', 'Дата погрузки', 'Кол-во', ...dates],
        ];
        for (const r of t.rows) {
          const actual = this.actualDate(r);
          aoa.push([
            r.trainIndex, r.trainStationOper, this.fmtFull(r.trainDate),
            r.stationNach, this.fmtFull(r.dateNach), r.vagonCount,
            ...dates.map((d) => (d === actual ? r.vagonCount : '')),
          ]);
        }
        aoa.push(['ВСЕГО', '', '', '', '', '', ...s.total]);
        aoa.push(['Выгрузка', '', '', '', '', '', ...s.unload]);
        aoa.push(['Остаток', '', '', '', '', '', ...s.remainder]);
        aoa.push(['Все едет', '', '', '', '', '', ...s.originalTotal]);
        aoa.push(['В пути', '', '', '', '', '', ...s.inTransit]);
        return { aoa, s };
      };

      // Стили блока таблицы (позиции — относительно headerRow, поэтому работают
      // и при стопке таблиц на одном листе).
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const styleBlock = (ws: any, t: BoardTable, headerRow: number, aoa: any[][]): void => {
        const L = aoa.length;
        const speed = this.speedOf(t);
        for (let rLocal = 0; rLocal < L; rLocal++) {
          const R = headerRow + rLocal;
          for (let C = 0; C < numCols; C++) {
            const ref = XLSX.utils.encode_cell({ r: R, c: C });
            if (!ws[ref]) continue;
            ws[ref].s = {
              alignment: { horizontal: 'center', vertical: 'center' },
              border: { top: border, bottom: border, left: border, right: border },
            };
            if (rLocal === 0) {
              ws[ref].s.font = { bold: true };
              ws[ref].s.fill = { patternType: 'solid', fgColor: { rgb: 'E0E0E0' } };
            } else if (rLocal >= L - 5) {
              ws[ref].s.font = { bold: true };
              const isHeatRow = rLocal === L - 5 || rLocal === L - 2; // ВСЕГО / Все едет
              if (isHeatRow && C >= 6 && typeof ws[ref].v === 'number') {
                ws[ref].s.fill = { patternType: 'solid', fgColor: { rgb: heatRgb(ws[ref].v, speed) } };
              }
            } else {
              const row = t.rows[rLocal - 1];
              if (row) {
                const shift = this.shifts()[row.id];
                if (shift && C >= 6 && this.highlightCell(row, dates[C - 6])) {
                  ws[ref].s.fill = { patternType: 'solid', fgColor: { rgb: 'FFF9C4' } };
                }
                const red = row.trainStatus === 5 && !row.isHistory && C !== 0;
                ws[ref].s.font = {
                  color: { rgb: red ? 'FF4D4F' : row.trainColor.replace('#', '') },
                };
              }
            }
            if (C >= 6 && rLocal > 0 && typeof ws[ref].v === 'number') {
              ws[ref].t = 'n';
              ws[ref].z = '0';
            }
          }
        }
      };

      let sheetName = 'Новый прогноз';
      if (singleSheet) {
        // Сводка станции: все таблицы на одном листе + строки «в пути» по
        // терминалам с разбивкой и общий итог (правило gtport «Мыс Астафьева»).
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const bigAoa: any[][] = [];
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const blocks: { t: BoardTable; headerRow: number; aoa: any[][]; titleRow: number }[] = [];
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const merges: any[] = [];
        const perTerm = new Map<string, number[]>();
        const grand = dates.map(() => 0);
        const tablesPerTerm = new Map<string, number>();
        for (const t of tables) {
          tablesPerTerm.set(t.terminal, (tablesPerTerm.get(t.terminal) ?? 0) + 1);
        }

        for (const t of tables) {
          const { aoa, s } = buildAoa(t);
          const acc = perTerm.get(t.terminal) ?? dates.map(() => 0);
          s.inTransit.forEach((v, i) => { acc[i] += v; grand[i] += v; });
          perTerm.set(t.terminal, acc);

          const titleRow = bigAoa.length;
          const row = new Array(numCols).fill('');
          row[0] = `${t.title} — остаток на 18:00: ${t.initialRemainder}`;
          bigAoa.push(row);
          merges.push({ s: { r: titleRow, c: 0 }, e: { r: titleRow, c: numCols - 1 } });
          const headerRow = bigAoa.length;
          aoa.forEach((r) => bigAoa.push(r));
          blocks.push({ t, headerRow, aoa, titleRow });
        }

        const grandRows: number[] = [];
        for (const [term, acc] of perTerm) {
          if ((tablesPerTerm.get(term) ?? 0) < 2) continue;
          grandRows.push(bigAoa.length);
          bigAoa.push([`ВСЕГО (вагонов в пути ${term})`, '', '', '', '', '', ...acc]);
        }
        grandRows.push(bigAoa.length);
        bigAoa.push(['ВСЕГО (вагонов в пути общее)', '', '', '', '', '', ...grand]);
        grandRows.forEach((r) => merges.push({ s: { r, c: 0 }, e: { r, c: 5 } }));

        const ws = XLSX.utils.aoa_to_sheet(bigAoa);
        ws['!cols'] = colWidths;
        ws['!merges'] = merges;
        for (const bl of blocks) {
          styleBlock(ws, bl.t, bl.headerRow, bl.aoa);
          const ref = XLSX.utils.encode_cell({ r: bl.titleRow, c: 0 });
          if (ws[ref]) {
            ws[ref].s = {
              font: { bold: true, sz: 12 },
              alignment: { horizontal: 'left', vertical: 'center' },
              fill: { patternType: 'solid', fgColor: { rgb: 'DCE6F1' } },
            };
          }
        }
        for (const r of grandRows) {
          for (let C = 0; C < numCols; C++) {
            const ref = XLSX.utils.encode_cell({ r, c: C });
            if (!ws[ref]) continue;
            ws[ref].s = {
              font: { bold: true },
              alignment: { horizontal: C === 0 ? 'left' : 'center', vertical: 'center' },
              fill: { patternType: 'solid', fgColor: { rgb: 'BBDEFB' } },
              border: { top: border, bottom: border, left: border, right: border },
            };
            if (C >= 6 && typeof ws[ref].v === 'number') {
              ws[ref].t = 'n';
              ws[ref].z = '0';
            }
          }
        }
        sheetName = this.options().find((o) => o.key === this.filter())?.label || sheetName;
        XLSX.utils.book_append_sheet(wb, ws, sheetName.substring(0, 31));
      } else {
        for (const t of tables) {
          const { aoa } = buildAoa(t);
          const ws = XLSX.utils.aoa_to_sheet(aoa);
          ws['!cols'] = colWidths;
          styleBlock(ws, t, 0, aoa);
          XLSX.utils.book_append_sheet(wb, ws, t.title.substring(0, 31));
        }
      }

      const now = new Date();
      const stamp = `${String(now.getDate()).padStart(2, '0')}.${String(now.getMonth() + 1).padStart(2, '0')}.${String(now.getFullYear()).slice(-2)}`;
      const base = singleSheet ? sheetName.toLowerCase().replace(/\s+/g, '_') : 'новый_прогноз';
      XLSX.writeFile(wb, `${base}_${stamp}.xlsx`);
    } catch (err) {
      console.error('Ошибка экспорта:', err);
      this.msg.error('Ошибка при экспорте в Excel');
    }
  }
}
