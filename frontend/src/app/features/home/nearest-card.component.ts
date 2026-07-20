import { Component, OnDestroy, OnInit, computed, inject, input, signal } from '@angular/core';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { TerminalTarget } from './arrivals-api.service';
import { NearestApiService, NearestTrain } from './nearest-api.service';
import { NearestModalComponent } from './nearest-modal.component';

/**
 * Компактный блок «Ближайшие поезда» половины станции (перенос gtport
 * TrainsMini «Подход»): ВСЕ нитки плана (has_plan; решение владельца —
 * миниатюра показывает план целиком, бесплановый прогноз — в модалке по
 * переключателю). Автообновление раз в минуту, «умное» (без перерисовки);
 * разворот — перемещаемая модалка с действиями.
 */
@Component({
  selector: 'app-nearest-card',
  imports: [NzButtonModule, NzIconModule, NzTooltipModule, NearestModalComponent],
  template: `
    <div class="card">
      <div class="head">
        <b>Ближайшие поезда</b>
        <span class="spacer"></span>
        <button nz-button nzType="text" nzSize="small" nz-tooltip
                nzTooltipTitle="Все подходящие поезда (полная таблица)" (click)="expanded.set(true)">
          <span nz-icon nzType="expand-alt"></span>
        </button>
      </div>
      <div class="mini-wrap">
        <table class="mini">
          <thead>
            <tr><th class="w-dt">Прибытие</th><th class="w-idx">Индекс</th><th>Состав</th></tr>
          </thead>
          <tbody>
            @for (t of planTrains(); track t.key) {
              <tr>
                <td class="c plan">{{ fmtDT(t.time_jd) }}</td>
                <td class="c num" [class.danger]="t.broshen">{{ t.index || '—' }}</td>
                <td class="sost ell" [title]="sostav(t)">{{ sostav(t) }}</td>
              </tr>
            } @empty {
              <tr><td colspan="3" class="empty">{{ loading() ? 'Загрузка…' : 'Нет плановых ниток в подходе' }}</td></tr>
            }
          </tbody>
        </table>
      </div>
    </div>

    @if (expanded()) {
      <app-nearest-modal [station]="station()" [terminals]="terminals()"
                         (closed)="expanded.set(false)" />
    }
  `,
  styles: [`
    .card { background: var(--color-bg-surface); border-radius: var(--radius-card);
            box-shadow: var(--shadow-card); padding: var(--space-sm) var(--space-md) var(--space-md); }
    .head { display: flex; align-items: center; gap: var(--space-sm); margin-bottom: var(--space-xs); }
    .spacer { flex: 1 1 auto; }
    /* Все нитки плана; при длинном плане — внутренний скролл, шапка липкая. */
    .mini-wrap { max-height: 44vh; overflow: auto; }
    .mini { width: 100%; border-collapse: collapse; font-size: var(--font-size-sm); }
    .mini th { background: var(--color-bg-subtle); font-weight: 600; padding: 3px 8px;
               border: 1px solid var(--color-border-light); position: sticky; top: 0; z-index: 1; }
    .mini td { padding: 3px 8px; border: 1px solid var(--color-border-light); }
    .c { text-align: center; white-space: nowrap; }
    .num { font-variant-numeric: tabular-nums; font-weight: 500; }
    .plan { color: var(--color-success); font-weight: 600; } /* время из нитки плана */
    .danger { color: var(--color-danger); } /* в составе брошенные */
    .w-dt { width: 92px; }
    .w-idx { width: 118px; }
    .sost { max-width: 0; }
    .ell { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .empty { text-align: center; color: var(--color-text-secondary); padding: var(--space-sm); }
  `],
})
export class NearestCardComponent implements OnInit, OnDestroy {
  private readonly api = inject(NearestApiService);
  private readonly msg = inject(NzMessageService);

  readonly station = input.required<string>();
  readonly terminals = input.required<TerminalTarget[]>();

  readonly loading = signal(false);
  readonly trains = signal<NearestTrain[]>([]);
  readonly expanded = signal(false);
  private timer: ReturnType<typeof setInterval> | null = null;

  /** Миниатюра показывает ВСЕ нитки плана (has_plan); бесплановый прогноз — в модалке. */
  readonly planTrains = computed(() => this.trains().filter((t) => t.has_plan));

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
      this.trains.set(await this.api.getNearest(this.terminals().map((t) => t.name)));
    } catch (err) {
      if (initial) this.msg.error(apiErrorMessage(err));
    } finally {
      if (initial) this.loading.set(false);
    }
  }

  /** дд.мм чч:мм (компакт — без года). */
  fmtDT(ts: string | null): string {
    if (!ts || ts.length < 16) return '—';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)} ${ts.slice(11, 16)}`;
  }

  sostav(t: NearestTrain): string {
    return t.sub_groups.map((sg) => sg.display).join(' · ') || '—';
  }
}
