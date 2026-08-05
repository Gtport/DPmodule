import { Component, OnInit, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzRadioModule } from 'ng-zorro-antd/radio';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../../core/api/api-error';
import {
  GtForecastApiService,
  GtContext,
  GtFreeSlot,
  GtOverride,
  GtSimulateRequest,
  GtSimulateResponse,
  GtStation,
  GtTrain,
} from './gt-forecast-api.service';
import { GtGanttComponent } from './gantt-chart.component';
import { GtTrainTableComponent } from './train-table.component';
import { GtTrainEditComponent } from './train-edit-dialog.component';

/**
 * Вкладка «Прогноз прибытия/выгрузки» (перенос страницы «Прогноз GT» gtport).
 * Слева — таблицы очереди поездов терминалов режима, справа — диаграммы Ганта
 * симуляции выгрузки по потокам.
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
  imports: [FormsModule, NzButtonModule, NzIconModule, NzRadioModule, NzSpinModule,
    GtGanttComponent, GtTrainTableComponent, GtTrainEditComponent],
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
        <button nz-button nzSize="small" [nzType]="useNorm() ? 'primary' : 'default'"
                title="Считать по нормативной скорости вместо плановой"
                (click)="toggleNorm()">
          {{ useNorm() ? 'Норма' : 'План' }}
        </button>
        <span class="spacer"></span>
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
        <button nz-button nzSize="small" title="Сбросить правки сеанса (скорости и поезда)"
                (click)="resetOverrides()" [disabled]="!hasOverrides()">
          <span nz-icon nzType="clear"></span>
        </button>
        <button nz-button nzSize="small" title="Обновить" (click)="reload()" [disabled]="loading()">
          <span nz-icon nzType="reload"></span>
        </button>
      </div>

      @if (loading() && !data()) {
        <div class="empty"><nz-spin nzSimple /></div>
      } @else if (data(); as d) {
        <div class="cols">
          <div class="left">
            @for (t of terminalBlocks(); track t.name) {
              <app-gt-train-table
                [title]="t.name" [color]="t.color" [trains]="t.trains"
                [remainder]="t.remainder" [waitByIndex]="t.waitByIndex" [black]="black()"
                [slots]="showSlots() ? d.free_slots : []"
                [selectedSlot]="selectedSlot()"
                (slotSelect)="selectedSlot.set($event)"
                (editTrain)="editTrain.set($event)" />
            }
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
              <app-gt-gantt [flow]="f" (speedChange)="onSpeed(f.terminal, f.cargo_key, $event)" />
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
    .left { flex: 0 0 38%; display: flex; flex-direction: column; gap: var(--space-sm);
            min-width: 320px; }
    .right { flex: 1 1 auto; display: flex; flex-direction: column; gap: var(--space-sm);
             min-width: 0; }
    .right.dim { opacity: 0.6; }
    .empty { display: flex; justify-content: center; padding: var(--space-xl);
             color: #888; background: var(--color-bg-surface); border-radius: var(--radius-card); }
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

  readonly hasOverrides = computed(() =>
    Object.keys(this.overrides()).length > 0 || this.trainOverrides().length > 0);

  /** Терминалы текущего режима (для «Переместить»). */
  readonly modeTerminals = computed(() => {
    const st = this.stations().find((s) => s.code === this.station());
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
    const d = this.data();
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
    this.station.set(code);
    this.resetSession();
    void this.simulate();
  }

  setStartDate(v: string): void {
    if (!v) return;
    this.startDate.set(v);
    this.resetSession();
    void this.simulate();
  }

  setDays(v: number | null): void {
    if (!v || v < 1 || v > 14) return;
    this.days.set(v);
    void this.simulate();
  }

  toggleNorm(): void {
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

/** Локальная дата YYYY-MM-DD (стартовые расчётные сутки по умолчанию). */
function localDate(): string {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}
