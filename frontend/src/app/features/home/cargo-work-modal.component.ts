import { Component, OnInit, computed, inject, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { DragDropModule } from '@angular/cdk/drag-drop';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzPopconfirmModule } from 'ng-zorro-antd/popconfirm';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { yesterdayMsk } from '../../shared/msk-date';
import { ArrivalsApiService, TerminalTarget } from './arrivals-api.service';
import {
  CargoWorkApiService, CargoWorkDay, CargoWorkLine, CargoWorkLoad, CargoWorkManual,
} from './cargo-work-api.service';

/**
 * Модалка «Грузовая работа» (перенос одноимённой страницы gtport): суточный
 * учётный лист терминала — что осталось с прошлых суток, что прибыло, что
 * выгружено и насколько это близко к тому, что порт МОГ переработать.
 *
 * Отличия от gtport, видимые диспетчеру:
 *  · терминалы и колонки груза приходят из справочника — таблица строится по
 *    ответу, а не по зашитым «АТ/УТ/ГУТ» и «уголь/металл/чугун»;
 *  · «Выгрузка станция» больше не вводится руками — это НАША цифра из вех
 *    истории, поэтому «перепоказ» показывает расхождение АСУ с фактом порта;
 *  · есть «Пересчитать»: история дополняется (подтверждение прибытий, sticky-10,
 *    «Обновить справочники»), и вчерашние цифры иначе остались бы старыми;
 *  · остаток/эффективность/перепоказ считает сервер — на клиенте формул нет.
 *
 * Окно перемещается за заголовок (cdkDrag), как остальные модалки проекта.
 */
@Component({
  selector: 'app-cargo-work-modal',
  imports: [
    FormsModule, DragDropModule, NzButtonModule, NzIconModule, NzInputModule,
    NzModalModule, NzPopconfirmModule, NzSpinModule, NzTooltipModule,
  ],
  template: `
    <nz-modal [nzVisible]="true" [nzTitle]="title" [nzFooter]="null" nzWidth="900px"
              [nzMask]="false" nzWrapClassName="cargo-work-wrap" (nzOnCancel)="closed.emit()">
      <ng-template #title>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          Грузовая работа
        </div>
      </ng-template>

      <ng-container *nzModalContent>
        <div class="bar">
          @for (t of terminals(); track t.name) {
            <button class="term" type="button"
                    [class.on]="t.name === terminal()"
                    [style.background]="t.name === terminal() ? (t.color || '#eee') : null"
                    (click)="pickTerminal(t.name)">{{ t.name }}</button>
          }
          <span class="spacer"></span>
          <span class="lbl">Дата</span>
          <input class="date" type="date" [ngModel]="date()" (ngModelChange)="pickDate($event)" />
          <button nz-button nzSize="small" (click)="resetDate()">Вчера</button>
        </div>

        <nz-spin [nzSpinning]="loading()">
          @if (day(); as d) {
            <table class="grid">
              <thead>
                <tr>
                  <th class="head" [style.background]="d.color || '#f5f5f5'">Показатель</th>
                  @for (ln of d.lines; track ln.cargo_key) {
                    <th class="head num" [style.background]="d.color || '#f5f5f5'"
                        nz-tooltip [nzTooltipTitle]="ln.pc ? ln.pc + ' ваг/сут' : 'способность не задана'">
                      {{ ln.label }}
                    </th>
                  }
                </tr>
              </thead>
              <tbody>
                <tr>
                  <td class="lbl-cell">Ост на 18 факт/станция</td>
                  @for (ln of d.lines; track ln.cargo_key) {
                    <td class="num">{{ pair(ln.ost_18, ln.ost_st) }}</td>
                  }
                </tr>
                <tr>
                  <td class="lbl-cell">Прибыло</td>
                  @for (ln of d.lines; track ln.cargo_key) { <td class="num">{{ ln.prib }}</td> }
                </tr>
                <tr>
                  <td class="lbl-cell">Обр полез/полн</td>
                  @for (ln of d.lines; track ln.cargo_key) {
                    <td class="num">{{ pair(ln.useful_formation, ln.total_formation) }}</td>
                  }
                </tr>
                <tr>
                  <td class="lbl-cell">План выгрузки</td>
                  @for (ln of d.lines; track ln.cargo_key) {
                    <td class="num">
                      @if (editing()) {
                        <input class="cell" type="number" min="0"
                               [ngModel]="draft(ln.cargo_key).plan"
                               (ngModelChange)="setField(ln.cargo_key, 'plan', $event)" />
                      } @else { {{ ln.plan }} }
                    </td>
                  }
                </tr>
                <tr>
                  <td class="lbl-cell">Выгрузка факт/станция</td>
                  @for (ln of d.lines; track ln.cargo_key) {
                    <td class="num">
                      @if (editing()) {
                        <span class="two">
                          <input class="cell" type="number" min="0"
                                 [ngModel]="draft(ln.cargo_key).vigr_fact"
                                 (ngModelChange)="setField(ln.cargo_key, 'vigr_fact', $event)" />
                          <span class="sep">/</span>
                          <span class="ro" nz-tooltip
                                nzTooltipTitle="Выгружено по данным АСУ — считается из истории вагонов">
                            {{ ln.vigr_stan }}
                          </span>
                        </span>
                      } @else { {{ pair(ln.vigr_fact, ln.vigr_stan) }} }
                    </td>
                  }
                </tr>
                <tr>
                  <td class="lbl-cell">Остаток</td>
                  @for (ln of d.lines; track ln.cargo_key) { <td class="num">{{ ln.ost }}</td> }
                </tr>
                <tr>
                  <td class="lbl-cell">Эффективность</td>
                  @for (ln of d.lines; track ln.cargo_key) {
                    <td class="num">{{ ln.effectiv }}%</td>
                  }
                </tr>
                <tr>
                  <td class="lbl-cell">Перепоказ</td>
                  @for (ln of d.lines; track ln.cargo_key) {
                    <td class="num" [class.warn]="ln.perepokaz !== 0">{{ ln.perepokaz }}</td>
                  }
                </tr>
                <tr>
                  <td class="lbl-cell">Простой порта</td>
                  @for (ln of d.lines; track ln.cargo_key) {
                    <td class="num">{{ ln.downtime || '0:00' }}</td>
                  }
                </tr>
                <tr>
                  <td class="lbl-cell">Комментарий</td>
                  @for (ln of d.lines; track ln.cargo_key) {
                    <td class="num">
                      @if (editing()) {
                        <input nz-input nzSize="small" class="prim"
                               [ngModel]="draft(ln.cargo_key).prim"
                               (ngModelChange)="setField(ln.cargo_key, 'prim', $event)" />
                      } @else { {{ ln.prim || '—' }} }
                    </td>
                  }
                </tr>
              </tbody>
            </table>

            @if (d.load.length) {
              <table class="grid load">
                <thead>
                  <tr>
                    <th class="head" [style.background]="d.color || '#f5f5f5'">Погрузка</th>
                    <th class="head num" [style.background]="d.color || '#f5f5f5'">Погружено</th>
                    <th class="head num" [style.background]="d.color || '#f5f5f5'">План</th>
                    <th class="head num" [style.background]="d.color || '#f5f5f5'">Остаток</th>
                  </tr>
                </thead>
                <tbody>
                  @for (row of d.load; track row.cargo_key) {
                    <tr>
                      <td class="lbl-cell">{{ row.label }}</td>
                      @for (f of loadFields; track f) {
                        <td class="num">
                          @if (editing()) {
                            <input class="cell" type="number" min="0"
                                   [ngModel]="loadDraft(row.cargo_key)[f]"
                                   (ngModelChange)="setLoadField(row.cargo_key, f, $event)" />
                          } @else { {{ row[f] }} }
                        </td>
                      }
                    </tr>
                  }
                </tbody>
              </table>
            }
          } @else if (!loading()) {
            <p class="mut">Нет данных за выбранные сутки.</p>
          }
        </nz-spin>

        <div class="acts">
          @if (editing()) {
            <button nz-button nzType="primary" nzSize="small" [nzLoading]="saving()"
                    (click)="save()">Сохранить</button>
            <button nz-button nzSize="small" (click)="cancel()">Отмена</button>
          } @else {
            <button nz-button nzType="primary" nzSize="small" (click)="edit()">Редактировать</button>
            <button nz-button nzSize="small" nz-tooltip
                    nzTooltipTitle="Пересобрать прибытие, выгрузку по станции и аналитику из истории; введённое вручную сохранится"
                    [nzLoading]="recalcing()" (click)="recalc()">Пересчитать</button>
            <button nz-button nzSize="small" nzDanger nz-popconfirm
                    nzPopconfirmTitle="Удалить учёт за эти сутки по терминалу?"
                    (nzOnConfirm)="remove()">Удалить</button>
          }
          <span class="spacer"></span>
          <span class="hint">Серые строки считает система; вручную вводятся план, факт выгрузки и комментарий.</span>
        </div>
      </ng-container>
    </nz-modal>
  `,
  styles: [`
    .ttl { cursor: move; }
    .bar { display: flex; align-items: center; gap: var(--space-xs); margin-bottom: var(--space-sm);
           flex-wrap: wrap; }
    .spacer { flex: 1 1 auto; }
    .lbl { color: var(--color-text-secondary); font-size: var(--font-size-sm); }
    .date { font-size: var(--font-size-sm); padding: 2px 6px; border: 1px solid var(--color-border);
            border-radius: var(--radius-sm); background: var(--color-bg-surface); color: inherit; }
    .term { border: 1px solid var(--color-border); background: none; color: inherit; cursor: pointer;
            border-radius: var(--radius-sm); padding: 2px 10px; font-size: var(--font-size-sm); }
    .term.on { font-weight: 600; border-color: var(--color-text-secondary); }

    .grid { width: 100%; border-collapse: collapse; font-size: var(--font-size-sm); }
    .grid.load { margin-top: var(--space-md); }
    .grid th, .grid td { border: 1px solid var(--color-border); padding: 3px 8px; }
    .head { font-weight: 600; text-align: left; }
    .num { text-align: center; font-variant-numeric: tabular-nums; }
    .lbl-cell { font-weight: 500; white-space: nowrap; }
    .warn { color: var(--color-danger); font-weight: 600; }
    .cell { width: 64px; text-align: center; font-size: var(--font-size-sm); padding: 1px 4px;
            border: 1px solid var(--color-border); border-radius: var(--radius-sm);
            background: var(--color-bg-surface); color: inherit; }
    .two { display: inline-flex; align-items: center; gap: 4px; justify-content: center; }
    .sep { color: var(--color-text-muted); }
    .ro { color: var(--color-text-secondary); min-width: 28px; display: inline-block; }
    .prim { width: 100%; }

    .acts { display: flex; align-items: center; gap: var(--space-xs); margin-top: var(--space-md); }
    .hint { color: var(--color-text-muted); font-size: var(--font-size-xs); }
    .mut { color: var(--color-text-secondary); }
  `],
})
export class CargoWorkModalComponent implements OnInit {
  private readonly api = inject(CargoWorkApiService);
  private readonly arrivalsApi = inject(ArrivalsApiService);
  private readonly msg = inject(NzMessageService);

  readonly closed = output<void>();

  readonly terminals = signal<TerminalTarget[]>([]);
  readonly terminal = signal('');
  readonly date = signal(yesterdayISO());
  readonly day = signal<CargoWorkDay | null>(null);

  readonly loading = signal(false);
  readonly saving = signal(false);
  readonly recalcing = signal(false);
  readonly editing = signal(false);

  /** Черновик правок — только ручные поля; авто-слой фронт не трогает. */
  private readonly lineDraft = signal<Record<string, LineDraft>>({});
  private readonly loadDrafts = signal<Record<string, LoadDraft>>({});

  readonly loadFields: (keyof CargoWorkLoad & ('load_fact' | 'plan' | 'ost'))[] =
    ['load_fact', 'plan', 'ost'];

  async ngOnInit(): Promise<void> {
    try {
      const terminals = await this.arrivalsApi.getTerminals();
      this.terminals.set(terminals);
      if (terminals.length) {
        this.terminal.set(terminals[0].name);
        await this.load();
      }
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    }
  }

  async load(): Promise<void> {
    if (!this.terminal()) return;
    this.loading.set(true);
    try {
      this.day.set(await this.api.getDay(this.date(), this.terminal()));
      this.editing.set(false);
    } catch (err) {
      this.day.set(null);
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.loading.set(false);
    }
  }

  pickTerminal(name: string): void {
    if (name === this.terminal()) return;
    this.terminal.set(name);
    void this.load();
  }

  pickDate(value: string): void {
    if (!value) return;
    this.date.set(value);
    void this.load();
  }

  resetDate(): void {
    this.pickDate(yesterdayISO());
  }

  /** «Факт/станция» и «полез/полн» — как в gtport: через дробь, если есть вторая цифра. */
  pair(first: number, second: number): string {
    return second !== 0 ? `${first}/${second}` : String(first);
  }

  edit(): void {
    const d = this.day();
    if (!d) return;
    const lines: Record<string, LineDraft> = {};
    for (const ln of d.lines) {
      lines[ln.cargo_key] = { plan: ln.plan, vigr_fact: ln.vigr_fact, prim: ln.prim };
    }
    const load: Record<string, LoadDraft> = {};
    for (const row of d.load) {
      load[row.cargo_key] = { load_fact: row.load_fact, plan: row.plan, ost: row.ost };
    }
    this.lineDraft.set(lines);
    this.loadDrafts.set(load);
    this.editing.set(true);
  }

  cancel(): void {
    this.editing.set(false);
  }

  draft(key: string): LineDraft {
    return this.lineDraft()[key] ?? { plan: 0, vigr_fact: 0, prim: '' };
  }

  loadDraft(key: string): LoadDraft {
    return this.loadDrafts()[key] ?? { load_fact: 0, plan: 0, ost: 0 };
  }

  setField(key: string, field: 'plan' | 'vigr_fact' | 'prim', value: unknown): void {
    const cur = { ...this.draft(key) };
    if (field === 'prim') cur.prim = String(value ?? '');
    else cur[field] = toInt(value);
    this.lineDraft.set({ ...this.lineDraft(), [key]: cur });
  }

  setLoadField(key: string, field: 'load_fact' | 'plan' | 'ost', value: unknown): void {
    const cur = { ...this.loadDraft(key), [field]: toInt(value) };
    this.loadDrafts.set({ ...this.loadDrafts(), [key]: cur });
  }

  async save(): Promise<void> {
    this.saving.set(true);
    try {
      const manual: CargoWorkManual = { lines: this.lineDraft(), load: this.loadDrafts() };
      this.day.set(await this.api.save(this.date(), this.terminal(), manual));
      this.editing.set(false);
      this.msg.success('Учёт сохранён');
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.saving.set(false);
    }
  }

  async recalc(): Promise<void> {
    this.recalcing.set(true);
    try {
      this.day.set(await this.api.recalc(this.date(), this.terminal()));
      this.msg.success('Пересчитано по данным истории');
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.recalcing.set(false);
    }
  }

  async remove(): Promise<void> {
    try {
      await this.api.remove(this.date(), this.terminal());
      this.msg.success('Учёт за сутки удалён');
      await this.load();
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    }
  }
}

interface LineDraft { plan: number; vigr_fact: number; prim: string }
interface LoadDraft { load_fact: number; plan: number; ost: number }

/** Пустое поле — это ноль, а не NaN (иначе сервер получит null). */
function toInt(value: unknown): number {
  const n = Number(value);
  return Number.isFinite(n) ? Math.trunc(n) : 0;
}

/** Учётный лист закрывают на следующий день — по умолчанию открываем вчера
 *  (строго по Москве: старый вариант через toISOString съезжал на границе суток). */
function yesterdayISO(): string {
  return yesterdayMsk();
}
