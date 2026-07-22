import { Component, OnDestroy, OnInit, inject } from '@angular/core';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { OperativkaApiService, UnplannedTrain } from './operativka-api.service';

/**
 * Карточка «Без плана в подходе» — жёлтая секция-сигнал во всю ширину колонки
 * «Оперативка» (решение владельца: отдельная карточка, а не хвост таблицы
 * прибытия/выгрузки). Поезд попадает сюда, если на сравнении снимков сменил
 * станцию, плана нет, а до терминала ближе порога unplanned_move_km. Живёт до
 * «Скрыть» — или пока не получит план / не прибудет.
 *
 * Данные — из общего OperativkaApiService (один запрос на две карточки).
 */
@Component({
  selector: 'app-unplanned-card',
  imports: [NzButtonModule, NzTooltipModule],
  template: `
    @if (trains().length) {
      <div class="card unpl">
        <div class="unpl-title"><b>Без плана в подходе ({{ trains().length }})</b>
          <span class="hint">двигаются, плана нет</span></div>
        @for (u of trains(); track u.index) {
          <div class="unpl-row">
            <span class="num b">{{ u.index || '—' }}</span>
            <span class="mut">({{ u.vagon_count }})</span>
            <span class="unpl-sost ell" [title]="u.sostav.join(' · ')">{{ u.sostav.join(' · ') }}</span>
            <span class="mut nowrap">{{ u.station_oper }}@if (u.rasst != null) { · {{ u.rasst }} км }</span>
            <button nz-button nzSize="small" nz-tooltip
                    nzTooltipTitle="Скрыть (появится снова при следующей смене станции без плана)"
                    (click)="dismiss(u)">Скрыть</button>
          </div>
        }
      </div>
    }
  `,
  styles: [`
    .card { border-radius: var(--radius-card); box-shadow: var(--shadow-card);
            padding: var(--space-xs) var(--space-sm); }
    .unpl { background: var(--color-warning-bg); border: 1px solid var(--color-warning);
            display: flex; flex-direction: column; gap: 2px; }
    .unpl-title { display: flex; align-items: center; gap: var(--space-sm); font-size: var(--font-size-sm); }
    .unpl-row { display: flex; align-items: center; gap: var(--space-sm); font-size: var(--font-size-sm);
                padding: 2px 0; min-width: 0; }
    .b { font-weight: 600; }
    .num { font-variant-numeric: tabular-nums; }
    /* Данные — основным цветом (как в «Прибывших»/«Ближайших»), серый только для пояснения. */
    .mut { color: inherit; }
    .hint { color: var(--color-text-secondary); }
    .nowrap { white-space: nowrap; }
    .unpl-sost { flex: 1 1 auto; min-width: 0; }
    .ell { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  `],
})
export class UnplannedCardComponent implements OnInit, OnDestroy {
  private readonly api = inject(OperativkaApiService);
  private readonly msg = inject(NzMessageService);

  ngOnInit(): void { this.api.attach(); }
  ngOnDestroy(): void { this.api.detach(); }

  trains(): UnplannedTrain[] {
    return this.api.data()?.unplanned ?? [];
  }

  async dismiss(u: UnplannedTrain): Promise<void> {
    try {
      await this.api.dismissUnplanned(u);
      this.msg.info(`Скрыто: поезд ${u.index || '—'} (${u.vagon_count} ваг.).`);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    }
  }
}
