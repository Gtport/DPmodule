import { Component, computed, inject, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { DragDropModule } from '@angular/cdk/drag-drop';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzRadioModule } from 'ng-zorro-antd/radio';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import {
  GtForecastApiService, GtFreeSlot, GtOverride, GtSimulateRequest, GtTrain,
} from './gt-forecast-api.service';

/**
 * Диалог what-if правки поезда (перенос gtport TrainEditDialog):
 * Бросить / Восстановить / На нитку / Переместить универсальный груз.
 * Предпросмотр — тот же POST simulate с правкой-кандидатом: сервер отвечает
 * за долю секунды, диалог показывает новые прибытие и отклонение до применения.
 */
@Component({
  selector: 'app-gt-train-edit',
  imports: [FormsModule, DragDropModule, NzButtonModule, NzModalModule, NzRadioModule, NzSpinModule],
  template: `
    <nz-modal [nzVisible]="true" [nzTitle]="ttl" [nzFooter]="null" nzWidth="460px"
              (nzOnCancel)="closed.emit()">
      <ng-template #ttl>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          Правка поезда {{ train().index }}
        </div>
      </ng-template>
      <ng-container *nzModalContent>
        <div class="info">
          <span>Станция: <b>{{ train().station_oper }}</b></span>
          <span>Вагонов: <b>{{ train().vagon_count }}</b></span>
          <span>Расчёт: <b>{{ jd(train().rasch_jd) }}</b></span>
          <span>Прогноз: <b>{{ jd(train().prog_jd) }}</b></span>
          @if (train().delay_hours) { <span>Задержка: <b>{{ train().delay_hours }}ч</b></span> }
          @if (train().status === '5') { <span class="chip thrown">БРОШЕН</span> }
          @if (train().plan_jd) { <span class="chip plan">В ПЛАНЕ</span> }
          @if (hasUniversal()) { <span class="chip univ">УНИВЕРС</span> }
        </div>

        <nz-radio-group [ngModel]="mode()" (ngModelChange)="setMode($event)" class="modes">
          <label nz-radio-button nzValue="throw">🛑 Бросить</label>
          @if (train().status === '5') {
            <label nz-radio-button nzValue="restore">▶ Восстановить</label>
          }
          @if (slot()) {
            <label nz-radio-button nzValue="assign">📌 На нитку</label>
          }
          @if (hasUniversal() && moveTargets().length > 0) {
            <label nz-radio-button nzValue="move">↔ Переместить</label>
          }
        </nz-radio-group>

        @switch (mode()) {
          @case ('throw') {
            <label class="fld">Суток простоя:
              <input type="number" min="1" max="30" [ngModel]="days()" (ngModelChange)="setDays($event)" />
            </label>
          }
          @case ('restore') {
            <label class="fld">Остаточная задержка, ч (0 — немедленно):
              <input type="number" min="0" max="720" [ngModel]="hours()" (ngModelChange)="setHours($event)" />
            </label>
          }
          @case ('assign') {
            <div class="fld">Нитка: <b>{{ jd(slot()!.jd) }} ЖД</b></div>
          }
          @case ('move') {
            <label class="fld">Терминал:
              <nz-radio-group [ngModel]="moveTo()" (ngModelChange)="setMoveTo($event)">
                @for (t of moveTargets(); track t) {
                  <label nz-radio [nzValue]="t">{{ t }}</label>
                }
              </nz-radio-group>
            </label>
          }
        }

        <div class="preview">
          @if (previewing()) {
            <nz-spin nzSimple nzSize="small" />
          } @else if (preview(); as p) {
            <div>Новый прогноз: <b>{{ jd(p.progJd) }}</b>
              @if (p.mistake !== null) { · откл. <b>{{ p.mistake.toFixed(1) }}</b> }
            </div>
            <div class="note">* пересчитывается вся очередь станции</div>
          } @else if (previewError()) {
            <div class="err">{{ previewError() }}</div>
          }
        </div>

        <div class="btns">
          <button nz-button (click)="closed.emit()">Отмена</button>
          <button nz-button nzType="primary" (click)="doApply()" [disabled]="previewing()">Применить</button>
        </div>
      </ng-container>
    </nz-modal>
  `,
  styles: [`
    .ttl { cursor: move; }
    .info { display: flex; flex-wrap: wrap; gap: 4px 12px; font-size: 12px;
            margin-bottom: var(--space-sm); }
    .chip { border-radius: 3px; padding: 0 6px; font-weight: 600; font-size: 11px; }
    .chip.thrown { background: #ffebee; color: #c62828; }
    .chip.plan { background: #e8f5e9; color: #2e7d32; }
    .chip.univ { background: #ede7f6; color: #6a1b9a; }
    .modes { margin-bottom: var(--space-sm); }
    .fld { display: block; font-size: 12px; margin-bottom: var(--space-sm); }
    .fld input { width: 70px; margin-left: 6px; border: 1px solid var(--color-border, #d9d9d9);
                 border-radius: 4px; padding: 2px 6px; }
    .preview { min-height: 40px; font-size: 12px; background: var(--color-bg-muted, #fafafa);
               border-radius: 4px; padding: 6px 10px; margin-bottom: var(--space-sm); }
    .preview .note { color: #888; font-size: 11px; }
    .preview .err { color: #c62828; }
    .btns { display: flex; justify-content: flex-end; gap: var(--space-sm); }
  `],
})
export class GtTrainEditComponent {
  private readonly api = inject(GtForecastApiService);

  readonly train = input.required<GtTrain>();
  /** Выбранная в таблице свободная нитка (для режима «На нитку»). */
  readonly slot = input<GtFreeSlot | null>(null);
  /** Терминалы режима, куда можно переместить универсальный груз. */
  readonly terminals = input<string[]>([]);
  /** Базовый запрос сеанса (правки + скорости) — предпросмотр добавляет кандидата. */
  readonly baseRequest = input.required<GtSimulateRequest>();

  readonly apply = output<GtOverride>();
  readonly closed = output<void>();

  readonly mode = signal<'throw' | 'restore' | 'assign' | 'move'>('throw');
  readonly days = signal(1);
  readonly hours = signal(0);
  readonly moveTo = signal('');
  readonly preview = signal<{ progJd: string | null; mistake: number | null } | null>(null);
  readonly previewing = signal(false);
  readonly previewError = signal('');

  private previewTimer: ReturnType<typeof setTimeout> | null = null;

  readonly hasUniversal = computed(() => this.train().sub_groups.some((sg) => sg.is_universal));
  readonly moveTargets = computed(() => {
    const cur = new Set(this.train().sub_groups.filter((sg) => sg.is_universal).map((sg) => sg.naznach));
    return this.terminals().filter((t) => !cur.has(t));
  });

  ngOnInit(): void {
    if (this.train().status === '5') this.mode.set('restore');
    this.moveTo.set(this.moveTargets()[0] ?? '');
    this.schedulePreview();
  }

  setMode(m: 'throw' | 'restore' | 'assign' | 'move'): void {
    this.mode.set(m);
    this.schedulePreview();
  }
  setDays(v: number | null): void {
    if (v && v >= 1) { this.days.set(v); this.schedulePreview(); }
  }
  setHours(v: number | null): void {
    if (v !== null && v >= 0) { this.hours.set(v); this.schedulePreview(); }
  }
  setMoveTo(t: string): void {
    this.moveTo.set(t);
    this.schedulePreview();
  }

  /** Правка-кандидат из текущих полей диалога. */
  candidate(): GtOverride | null {
    const index = this.train().index;
    switch (this.mode()) {
      case 'throw': return { index, action: 'throw', delay_days: this.days() };
      case 'restore': return { index, action: 'restore', delay_hours: this.hours() };
      case 'assign': {
        const s = this.slot();
        return s ? { index, action: 'assign', slot: s.msk } : null;
      }
      case 'move': return this.moveTo() ? { index, action: 'move', move_to: this.moveTo() } : null;
    }
  }

  /** Предпросмотр с задержкой набора: тот же simulate с кандидатом. */
  private schedulePreview(): void {
    if (this.previewTimer) clearTimeout(this.previewTimer);
    this.previewTimer = setTimeout(() => void this.runPreview(), 350);
  }

  private async runPreview(): Promise<void> {
    const cand = this.candidate();
    if (!cand) { this.preview.set(null); return; }
    this.previewing.set(true);
    this.previewError.set('');
    try {
      const base = this.baseRequest();
      const res = await this.api.simulate({ ...base, overrides: [...base.overrides, cand] });
      const t = res.trains.find((x) => x.index === cand.index);
      this.preview.set(t ? { progJd: t.prog_jd, mistake: t.mistake } : null);
    } catch (e) {
      this.preview.set(null);
      this.previewError.set(e instanceof Error ? e.message : 'Не удалось посчитать предпросмотр');
    } finally {
      this.previewing.set(false);
    }
  }

  doApply(): void {
    const cand = this.candidate();
    if (cand) this.apply.emit(cand);
  }

  jd(iso: string | null | undefined): string {
    if (!iso) return '—';
    return `${iso.slice(8, 10)}.${iso.slice(5, 7)} ${iso.slice(11, 16)}`;
  }
}
