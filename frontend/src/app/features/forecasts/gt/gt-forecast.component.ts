import { Component, OnInit, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzRadioModule } from 'ng-zorro-antd/radio';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { NzSwitchModule } from 'ng-zorro-antd/switch';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../../core/api/api-error';
import {
  GtForecastApiService,
  GtContext,
  GtFreeSlot,
  GtOverride,
  GtSimulateRequest,
  GtSimulateResponse,
  GtSnapshotFull,
  GtSnapshotMeta,
  GtStation,
  GtTrain,
} from './gt-forecast-api.service';
import { GtGanttComponent } from './gantt-chart.component';
import { GtTrainTableComponent } from './train-table.component';
import { GtTrainEditComponent } from './train-edit-dialog.component';
import { GtSnapshotsComponent } from './snapshots-dialog.component';
import { GtSpeedsComponent } from './speeds-dialog.component';

/**
 * Вкладка «Прогноз прибытия/выгрузки» (перенос страницы «Прогноз GT» gtport).
 * Слева — таблицы очереди поездов терминалов режима РЯДОМ (как эталон:
 * АЭ | ГУТ-2, строка = поезд), справа — диаграммы Ганта симуляции выгрузки
 * по потокам.
 *
 * Вся математика — на сервере (POST simulate): смена режима/даты/горизонта/
 * скорости суток и what-if правки поездов → пересчёт запросом. Правки
 * (скорости и поезда) живут в сеансе и в БД не пишутся — как в gtport.
 * Журнал действий пишется на каждую применённую правку (prev/new
 * прибытие и отклонение) и очищается сбросом сеанса.
 */

/** Запись журнала what-if сеанса (эталон gtport JournalEntry). */
interface JournalEntry {
  time: string;
  index: string;
  action: string;
  naznach: string;
  delayHours: number;
  prevProgJd: string | null;
  newProgJd: string | null;
  prevMistake: number | null;
  newMistake: number | null;
}

const ACTION_LABEL: Record<string, string> = {
  throw: 'Бросить',
  restore: 'Восстановить',
  assign: 'На нитку',
  move: 'Переместить',
};

@Component({
  selector: 'app-gt-forecast',
  imports: [FormsModule, NzButtonModule, NzIconModule, NzRadioModule, NzSpinModule, NzSwitchModule,
    GtGanttComponent, GtTrainTableComponent, GtTrainEditComponent, GtSnapshotsComponent,
    GtSpeedsComponent],
  template: `
    <div class="page">
      <div class="bar">
        <span class="ttl">Прогноз прибытия/выгрузки</span>
        <nz-radio-group [ngModel]="station()" (ngModelChange)="setStation($event)" nzSize="small">
          @for (s of stations(); track s.code) {
            <label nz-radio-button [nzValue]="s.code">{{ stationLabel(s) }}</label>
          }
        </nz-radio-group>
        <input class="date" type="date" [ngModel]="startDate()" (ngModelChange)="setStartDate($event)" />
        <label class="days">дней:
          <input type="number" min="1" max="14" [ngModel]="days()" (ngModelChange)="setDays($event)" />
        </label>
        <nz-switch [ngModel]="useNorm()" (ngModelChange)="toggleNorm()" nzSize="small"
                   nzCheckedChildren="Норма" nzUnCheckedChildren="План"
                   [nzDisabled]="archived()"
                   title="Считать по нормативной скорости вместо плановой"></nz-switch>
        <span class="spacer"></span>
        <button nz-button nzSize="small" title="Настройки скоростей выгрузки (план и норма линий)"
                (click)="speedsOpen.set(true)" [disabled]="archived() || !currentStation()">
          <span nz-icon nzType="setting"></span>
        </button>
        <button nz-button nzSize="small" [nzType]="showSlots() ? 'primary' : 'default'"
                title="Показать свободные нитки" (click)="toggleSlots()">
          🟢
        </button>
        <button nz-button nzSize="small" [nzType]="journalOpen() ? 'primary' : 'default'"
                title="Журнал правок сеанса" (click)="journalOpen.set(!journalOpen())">
          <span nz-icon nzType="history"></span>
          @if (journal().length > 0) { <span class="jcount">{{ journal().length }}</span> }
        </button>
        <button nz-button nzSize="small" [nzType]="black() ? 'primary' : 'default'"
                title="Скрыть окраску клиентов" (click)="black.set(!black())">
          <span nz-icon nzType="bg-colors"></span>
        </button>
        <button nz-button nzSize="small" title="Сохранённые прогнозы и CSV-аналитика"
                (click)="snapshotsOpen.set(true)" [disabled]="archived()">
          <span nz-icon nzType="book"></span>
        </button>
        <button nz-button nzSize="small" title="Экспорт в Excel" (click)="exportExcel()"
                [disabled]="!viewData()">
          <span nz-icon nzType="download"></span>
        </button>
        <button nz-button nzSize="small" title="Сбросить правки сеанса (скорости и поезда)"
                (click)="resetOverrides()" [disabled]="!hasOverrides() || archived()">
          <span nz-icon nzType="clear"></span>
        </button>
        <button nz-button nzSize="small" title="Обновить" (click)="reload()"
                [disabled]="loading() || archived()">
          <span nz-icon nzType="reload"></span>
        </button>
      </div>

      @if (archive(); as a) {
        <div class="archive-banner">
          📂 Архив прогноза на <b>{{ fmtDate(a.plan_date) }}</b>
          (расчёт с {{ fmtDate(a.start_date) }} · {{ a.days_count }} дн. ·
          сохранил {{ a.saved_by || '—' }}) — только просмотр, данные зафиксированы
          <button nz-button nzSize="small" (click)="closeArchive()">Вернуться к текущему</button>
        </div>
      }

      @if (loading() && !data()) {
        <div class="empty"><nz-spin nzSimple /></div>
      } @else if (viewData(); as d) {
        <div class="cols">
          <div class="left">
            <div class="tables">
              @for (t of terminalBlocks(); track t.name) {
                <app-gt-train-table
                  [title]="t.name" [color]="t.color" [trains]="t.trains"
                  [remainder]="t.remainder" [waitByIndex]="t.waitByIndex" [black]="black()"
                  [compact]="terminalBlocks().length > 1"
                  [slots]="showSlots() ? d.free_slots : []"
                  [selectedSlot]="selectedSlot()"
                  [selectedTrain]="selectedTrain()"
                  (slotSelect)="selectedSlot.set($event)"
                  (trainSelect)="toggleTrainSelect($event)"
                  (editTrain)="onEditTrain($event)" />
              }
            </div>
            @if (journalOpen()) {
              <div class="journal">
                <div class="jhead">Журнал правок ({{ journal().length }})</div>
                @for (j of journal(); track $index) {
                  <div class="jrow">
                    <span class="jtime">{{ j.time }}</span>
                    <b>{{ j.index }}</b>
                    <span>{{ actionLabel(j.action) }}</span>
                    @if (j.delayHours) { <span>{{ j.delayHours }}ч</span> }
                    <span class="jshift">{{ fmtJd(j.prevProgJd) }} → {{ fmtJd(j.newProgJd) }}</span>
                    @if (j.prevMistake !== null || j.newMistake !== null) {
                      <span class="jm">откл. {{ fmtM(j.prevMistake) }} → {{ fmtM(j.newMistake) }}</span>
                    }
                  </div>
                } @empty {
                  <div class="jrow muted">Правок не было</div>
                }
              </div>
            }
          </div>
          <div class="right" [class.dim]="loading()">
            @for (f of d.flows; track f.terminal + f.cargo_key) {
              <app-gt-gantt [flow]="f" [selectedTrain]="selectedTrain()"
                            (speedChange)="onSpeed(f.terminal, f.cargo_key, $event)"
                            (trainSelect)="toggleTrainSelect($event)"
                            (trainContext)="onGanttContext($event)" />
            }
          </div>
        </div>
      } @else {
        <div class="empty">Нет данных</div>
      }

      @if (editTrain(); as t) {
        <app-gt-train-edit
          [train]="t"
          [slot]="selectedSlot()"
          [terminals]="modeTerminals()"
          [baseRequest]="baseRequest()"
          (apply)="applyOverride($event)"
          (closed)="editTrain.set(null)" />
      }

      @if (snapshotsOpen()) {
        <app-gt-snapshots
          [request]="baseRequest()"
          [journal]="journal()"
          [overridesCount]="trainOverrides().length"
          (openSnap)="openSnapshot($event)"
          (closed)="snapshotsOpen.set(false)" />
      }

      @if (speedsOpen() && currentStation(); as st) {
        <app-gt-speeds [station]="st"
          (saved)="onSpeedsSaved()"
          (closed)="speedsOpen.set(false)" />
      }
    </div>
  `,
  styles: [`
    .page { display: flex; flex-direction: column; gap: var(--space-sm); }
    .bar { display: flex; align-items: center; gap: var(--space-sm); flex-wrap: wrap;
           background: var(--color-bg-surface); border-radius: var(--radius-card);
           box-shadow: var(--shadow-card); padding: var(--space-xs) var(--space-md); }
    .ttl { font-weight: 600; }
    .spacer { flex: 1; }
    .date { border: 1px solid var(--color-border, #d9d9d9); border-radius: 4px;
            padding: 1px 6px; font-size: 12px; }
    .days { font-size: 12px; color: #555; }
    .days input { width: 44px; border: 1px solid var(--color-border, #d9d9d9);
                  border-radius: 4px; padding: 1px 4px; font-size: 12px; }
    .jcount { font-size: 11px; font-weight: 600; margin-left: 2px; }
    .cols { display: flex; gap: var(--space-sm); align-items: flex-start; }
    /* Эталон gtport: таблицы терминалов РЯДОМ, левая панель ~53% ширины. */
    .left { flex: 0 0 53%; display: flex; flex-direction: column; gap: var(--space-sm);
            min-width: 480px; }
    .tables { display: flex; gap: var(--space-sm); align-items: flex-start; }
    .tables > * { flex: 1 1 0; min-width: 0; }
    .right { flex: 1 1 auto; display: flex; flex-direction: column; gap: var(--space-sm);
             min-width: 0; overflow-x: auto; }
    .right.dim { opacity: 0.6; }
    .empty { display: flex; justify-content: center; padding: var(--space-xl);
             color: #888; background: var(--color-bg-surface); border-radius: var(--radius-card); }
    .archive-banner { display: flex; align-items: center; gap: var(--space-sm); flex-wrap: wrap;
             background: #fff8e1; border: 1px solid #ffe082; border-radius: var(--radius-card);
             padding: var(--space-xs) var(--space-md); font-size: 12px; }
    .journal { background: var(--color-bg-surface); border-radius: var(--radius-card);
               box-shadow: var(--shadow-card); padding: var(--space-xs) var(--space-sm);
               font-size: 11px; max-height: 30vh; overflow: auto; }
    .jhead { font-weight: 600; margin-bottom: 4px; }
    .jrow { display: flex; flex-wrap: wrap; gap: 0 8px; padding: 1px 0;
            border-bottom: 1px dashed var(--color-border, #eee); }
    .jrow.muted { color: #999; border: none; }
    .jtime { color: #999; }
    .jshift { color: #1565c0; }
    .jm { color: #777; }
  `],
})
export class GtForecastComponent implements OnInit {
  private readonly api = inject(GtForecastApiService);
  private readonly msg = inject(NzMessageService);

  readonly stations = signal<GtStation[]>([]);
  readonly station = signal<string>('');
  readonly startDate = signal<string>(localDate());
  readonly days = signal<number>(10);
  readonly useNorm = signal<boolean>(false);
  readonly black = signal<boolean>(false);
  readonly loading = signal<boolean>(false);
  readonly data = signal<GtSimulateResponse | null>(null);
  /** «терминал|груз» → дата → ваг/сут: правки скоростей текущего сеанса. */
  readonly overrides = signal<Record<string, Record<string, number>>>({});
  /** What-if правки поездов текущего сеанса (весь список уходит в каждый simulate). */
  readonly trainOverrides = signal<GtOverride[]>([]);
  readonly journal = signal<JournalEntry[]>([]);
  readonly journalOpen = signal(false);
  readonly showSlots = signal(false);
  readonly selectedSlot = signal<GtFreeSlot | null>(null);
  readonly editTrain = signal<GtTrain | null>(null);
  readonly snapshotsOpen = signal(false);
  readonly speedsOpen = signal(false);
  /** Выделенный поезд — синхронно в таблицах и на диаграммах (клик, toggle). */
  readonly selectedTrain = signal<string | null>(null);
  /** Открытый архивный план: подменяет данные экрана, правки блокируются. */
  readonly archive = signal<GtSnapshotFull | null>(null);

  readonly archived = computed(() => this.archive() !== null);

  /** Данные экрана: архив (read-only) либо живой пересчёт. */
  readonly viewData = computed<GtSimulateResponse | null>(() => {
    const a = this.archive();
    if (a) {
      return { trains: a.trains, flows: a.flows, free_slots: a.free_slots ?? [], max_train_wagons: 0 };
    }
    return this.data();
  });

  readonly hasOverrides = computed(() =>
    Object.keys(this.overrides()).length > 0 || this.trainOverrides().length > 0);

  /** Станция текущего режима (для диалогов настроек/перемещения). */
  readonly currentStation = computed(() =>
    this.stations().find((s) => s.code === this.station()) ?? null);

  /** Терминалы текущего режима (для «Переместить»). */
  readonly modeTerminals = computed(() => {
    const st = this.currentStation();
    return st ? st.terminals.map((t) => t.name) : [];
  });

  /** Базовый запрос сеанса — его же использует предпросмотр диалога. */
  readonly baseRequest = computed<GtSimulateRequest>(() => ({
    station: this.station(),
    start_date: this.startDate(),
    days: this.days(),
    use_norm: this.useNorm(),
    speed_overrides: this.overrides(),
    overrides: this.trainOverrides(),
  }));

  /** Блоки таблиц: терминал режима → его поезда, остатки и ожидания. */
  readonly terminalBlocks = computed(() => {
    const d = this.viewData();
    const st = this.stations().find((s) => s.code === this.station());
    if (!d || !st) return [];
    return st.terminals.map((term) => {
      const trains = d.trains.filter((t) =>
        t.sub_groups.some((sg) => sg.naznach === term.name));
      const flows = d.flows.filter((f) => f.terminal === term.name);
      const remainder = flows.length > 1
        ? 'остаток ' + flows.map((f) => `${f.cargo_key ? f.cargo_key[0] : ''}:${f.initial_remainder}`).join(' ')
        : flows.length === 1 ? `остаток: ${flows[0].initial_remainder}` : '';
      const waitByIndex: Record<string, number> = {};
      for (const f of flows) {
        for (const day of f.days) {
          for (const op of day.operations) {
            if (op.wait_min > 0 && op.train_index) {
              waitByIndex[op.train_index] = (waitByIndex[op.train_index] ?? 0) + op.wait_min;
            }
          }
        }
      }
      return { name: term.name, color: term.color, trains, remainder, waitByIndex };
    });
  });

  async ngOnInit(): Promise<void> {
    try {
      const ctx: GtContext = await this.api.getContext();
      this.stations.set(ctx.stations);
      if (ctx.stations.length > 0) {
        this.station.set(ctx.stations[0].code);
        await this.simulate();
      }
    } catch (e) {
      this.msg.error(apiErrorMessage(e));
    }
  }

  stationLabel(s: GtStation): string {
    return s.terminals.map((t) => t.name).join(' + ');
  }

  setStation(code: string): void {
    if (this.archived()) return;
    this.station.set(code);
    this.resetSession();
    void this.simulate();
  }

  setStartDate(v: string): void {
    if (!v || this.archived()) return;
    this.startDate.set(v);
    this.resetSession();
    void this.simulate();
  }

  setDays(v: number | null): void {
    if (!v || v < 1 || v > 14 || this.archived()) return;
    this.days.set(v);
    void this.simulate();
  }

  toggleNorm(): void {
    if (this.archived()) return;
    // Как gtport GlobalSpeedToggle: переключение затирает ручные правки скоростей.
    this.useNorm.set(!this.useNorm());
    this.overrides.set({});
    void this.simulate();
  }

  toggleSlots(): void {
    this.showSlots.set(!this.showSlots());
    if (!this.showSlots()) this.selectedSlot.set(null);
  }

  resetOverrides(): void {
    this.resetSession();
    void this.simulate();
  }

  private resetSession(): void {
    this.overrides.set({});
    this.trainOverrides.set([]);
    this.journal.set([]);
    this.selectedSlot.set(null);
    this.editTrain.set(null);
  }

  reload(): void {
    void this.simulate();
  }

  onSpeed(terminal: string, cargoKey: string, e: { date: string; value: number }): void {
    if (this.archived()) return;
    const key = `${terminal}|${cargoKey}`;
    const next = { ...this.overrides() };
    next[key] = { ...(next[key] ?? {}), [e.date]: e.value };
    this.overrides.set(next);
    void this.simulate();
  }

  /** Применение what-if правки из диалога: правка → пересчёт → запись в журнал. */
  async applyOverride(ov: GtOverride): Promise<void> {
    const before = this.data()?.trains.find((t) => t.index === ov.index);
    this.trainOverrides.set([...this.trainOverrides(), ov]);
    this.editTrain.set(null);
    this.selectedSlot.set(null);
    await this.simulate();
    const after = this.data()?.trains.find((t) => t.index === ov.index);
    const now = new Date();
    this.journal.set([...this.journal(), {
      time: `${String(now.getHours()).padStart(2, '0')}:${String(now.getMinutes()).padStart(2, '0')}`,
      index: ov.index,
      action: ov.action,
      naznach: before?.sub_groups[0]?.naznach ?? '',
      delayHours: ov.action === 'throw' ? (ov.delay_days ?? 0) * 24 : (ov.delay_hours ?? 0),
      prevProgJd: before?.prog_jd ?? null,
      newProgJd: after?.prog_jd ?? null,
      prevMistake: before?.mistake ?? null,
      newMistake: after?.mistake ?? null,
    }]);
    this.journalOpen.set(true);
  }

  onEditTrain(t: GtTrain): void {
    if (!this.archived()) this.editTrain.set(t);
  }

  toggleTrainSelect(index: string): void {
    this.selectedTrain.set(this.selectedTrain() === index ? null : index);
  }

  /** ПКМ по блоку диаграммы — диалог правки поезда по индексу. */
  onGanttContext(index: string): void {
    if (this.archived()) return;
    const t = this.viewData()?.trains.find((x) => x.index === index);
    if (t && !t.is_arrived) this.editTrain.set(t);
  }

  /** Скорости линий сохранены: перечитать context (линии) и пересчитать. */
  async onSpeedsSaved(): Promise<void> {
    this.speedsOpen.set(false);
    try {
      const ctx = await this.api.getContext();
      this.stations.set(ctx.stations);
    } catch (e) {
      this.msg.error(apiErrorMessage(e));
    }
    void this.simulate();
  }

  /** Открыть архивный план read-only (данные зафиксированы при сохранении). */
  async openSnapshot(meta: GtSnapshotMeta): Promise<void> {
    try {
      const full = await this.api.getSnapshot(meta.plan_date, meta.station);
      this.snapshotsOpen.set(false);
      this.selectedSlot.set(null);
      this.editTrain.set(null);
      this.archive.set(full);
    } catch (e) {
      this.msg.error(apiErrorMessage(e));
    }
  }

  closeArchive(): void {
    this.archive.set(null);
  }

  /** Экспорт в Excel: поезда, выгрузка по суткам, журнал (xlsx-js-style лениво). */
  async exportExcel(): Promise<void> {
    const d = this.viewData();
    if (!d) return;
    const XLSX = await import('xlsx-js-style');
    const wb = XLSX.utils.book_new();
    const headStyle = { font: { bold: true } };
    const head = (cells: string[]) => cells.map((v) => ({ v, s: headStyle }));

    const trainsRows: unknown[][] = [head([
      'Терминал', 'Поезд', 'Вагонов', 'Статус', 'Прибытие ЖД', 'Путь', 'Откл., сут', 'Задержка, ч', 'Ожид, ч',
    ])];
    for (const block of this.terminalBlocks()) {
      for (const t of block.trains) {
        const waitMin = block.waitByIndex[t.index] ?? 0;
        trainsRows.push([
          block.name, t.index, t.vagon_count,
          t.is_arrived ? 'прибыл' : t.status === '5' ? 'брошен' : t.plan_jd ? 'в плане' : 'в пути',
          this.fmtJd(t.prog_jd),
          t.is_arrived || t.to_go == null ? '' : (t.to_go / 24).toFixed(1),
          t.mistake == null ? '' : t.mistake.toFixed(2),
          t.delay_hours || '',
          waitMin > 0 ? (waitMin / 60).toFixed(1) : '',
        ]);
      }
    }
    const wsTrains = XLSX.utils.aoa_to_sheet(trainsRows);
    wsTrains['!cols'] = [{ wch: 10 }, { wch: 16 }, { wch: 9 }, { wch: 9 }, { wch: 13 }, { wch: 7 }, { wch: 10 }, { wch: 11 }, { wch: 8 }];
    XLSX.utils.book_append_sheet(wb, wsTrains, 'Поезда');

    const ganttRows: unknown[][] = [head([
      'Терминал', 'Груз', 'Сутки', 'План', 'Норма', 'Вход', 'Приб', 'Полн', 'Полез', 'Выгр', 'Ост', 'Простой',
    ])];
    for (const f of d.flows) {
      for (const day of f.days) {
        ganttRows.push([
          f.terminal, f.cargo_key, `${day.date.slice(8, 10)}.${day.date.slice(5, 7)}`,
          day.plan_speed, day.norm_speed, day.incoming_total, day.arrival,
          day.total_formation, day.useful_formation, day.unloaded, day.remaining,
          day.total_wait_min > 0 ? hm(day.total_wait_min) : '',
        ]);
      }
      ganttRows.push([]);
    }
    const wsGantt = XLSX.utils.aoa_to_sheet(ganttRows);
    wsGantt['!cols'] = [{ wch: 10 }, { wch: 9 }, { wch: 7 }, ...Array(8).fill({ wch: 7 }), { wch: 9 }];
    XLSX.utils.book_append_sheet(wb, wsGantt, 'Выгрузка по суткам');

    const journalRows: unknown[][] = [head([
      'Время', 'Поезд', 'Действие', 'Порт', 'Задержка, ч', 'Было (ЖД)', 'Стало (ЖД)', 'Откл. было', 'Откл. стало',
    ])];
    for (const j of this.journal()) {
      journalRows.push([
        j.time, j.index, this.actionLabel(j.action), j.naznach, j.delayHours || '',
        this.fmtJd(j.prevProgJd), this.fmtJd(j.newProgJd),
        this.fmtM(j.prevMistake), this.fmtM(j.newMistake),
      ]);
    }
    const wsJournal = XLSX.utils.aoa_to_sheet(journalRows);
    wsJournal['!cols'] = [{ wch: 7 }, { wch: 16 }, { wch: 13 }, { wch: 8 }, { wch: 11 }, { wch: 13 }, { wch: 13 }, { wch: 10 }, { wch: 10 }];
    XLSX.utils.book_append_sheet(wb, wsJournal, 'Журнал');

    const st = this.stations().find((s) => s.code === this.station());
    const label = st ? st.terminals.map((t) => t.name).join('+') : this.station();
    const date = this.archive()?.plan_date ?? this.startDate();
    XLSX.writeFile(wb, `gt_prognoz_${label}_${date}.xlsx`);
  }

  fmtDate(iso: string): string {
    return `${iso.slice(8, 10)}.${iso.slice(5, 7)}.${iso.slice(0, 4)}`;
  }

  actionLabel(a: string): string {
    return ACTION_LABEL[a] ?? a;
  }

  fmtJd(iso: string | null): string {
    if (!iso) return '—';
    return `${iso.slice(8, 10)}.${iso.slice(5, 7)} ${iso.slice(11, 16)}`;
  }

  fmtM(m: number | null): string {
    return m === null ? '—' : m.toFixed(1);
  }

  private async simulate(): Promise<void> {
    if (!this.station()) return;
    this.loading.set(true);
    try {
      this.data.set(await this.api.simulate(this.baseRequest()));
    } catch (e) {
      this.msg.error(apiErrorMessage(e));
    } finally {
      this.loading.set(false);
    }
  }
}

/** Минуты → «ЧЧ:ММ» (колонка простоя в Excel). */
function hm(minutes: number): string {
  const m = Math.round(minutes);
  return `${String(Math.floor(m / 60)).padStart(2, '0')}:${String(m % 60).padStart(2, '0')}`;
}

/** Локальная дата YYYY-MM-DD (стартовые расчётные сутки по умолчанию). */
function localDate(): string {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}
