import { Component, OnInit, computed, inject, input, signal } from '@angular/core';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { ArrivalGroup, ArrivalsApiService, TerminalTarget } from './arrivals-api.service';
import { ArrivalsHistoryComponent } from './arrivals-history.component';

/**
 * Компактный блок «Прибывшие» половины станции (перенос gtport HistoryTableM):
 * последние прибывшие поезда за вчера/сегодня (Прибытие · Индекс), кнопка
 * разворота открывает перемещаемую модалку «История прибывших» с полной таблицей.
 */
@Component({
  selector: 'app-arrivals-card',
  imports: [NzButtonModule, NzIconModule, NzTooltipModule, ArrivalsHistoryComponent],
  template: `
    <div class="card">
      <div class="head">
        <b>Прибывшие</b>
        @if (candidatesCount()) {
          <span class="cand-badge" nz-tooltip
                nzTooltipTitle="Кандидаты на прибытие — АСУ не дала дату, требуется подтверждение (открыть историю)"
                (click)="expanded.set(true)">кандидаты: {{ candidatesCount() }}</span>
        }
        <span class="spacer"></span>
        <button nz-button nzType="text" nzSize="small" nz-tooltip
                nzTooltipTitle="История прибывших (полная таблица)" (click)="expanded.set(true)">
          <span nz-icon nzType="expand-alt"></span>
        </button>
      </div>
      <table class="mini">
        <thead>
          <tr><th class="w-dt">Прибытие</th><th class="w-idx">Индекс</th><th>Состав</th></tr>
        </thead>
        <tbody>
          @for (g of topGroups(); track g.key) {
            <tr>
              <td class="c">{{ fmtDT(g.date_prib) }}</td>
              <td class="c num">{{ g.index_pp || '—' }}</td>
              <td class="sost ell" [title]="sostav(g)">{{ sostav(g) }}</td>
            </tr>
          } @empty {
            <tr><td colspan="3" class="empty">{{ loading() ? 'Загрузка…' : 'Нет прибывших' }}</td></tr>
          }
        </tbody>
      </table>
    </div>

    @if (expanded()) {
      <app-arrivals-history [station]="station()" [terminals]="terminals()"
                            (closed)="expanded.set(false)" />
    }
  `,
  styles: [`
    .card { background: var(--color-bg-surface); border-radius: var(--radius-card);
            box-shadow: var(--shadow-card); padding: var(--space-sm) var(--space-md) var(--space-md); }
    .head { display: flex; align-items: center; gap: var(--space-sm); margin-bottom: var(--space-xs); }
    .spacer { flex: 1 1 auto; }
    /* Чип-предупреждение (канон: без заливки, контур и текст в цвете). */
    .cand-badge { border: 1px solid var(--color-warning); color: var(--color-warning);
                  border-radius: 10px; padding: 0 8px; font-size: var(--font-size-sm);
                  font-weight: 600; cursor: pointer; white-space: nowrap; }
    .mini { width: 100%; border-collapse: collapse; font-size: var(--font-size-sm); }
    .mini th { background: var(--color-bg-subtle); font-weight: 600; padding: 3px 8px;
               border: 1px solid var(--color-border-light); }
    .mini td { padding: 3px 8px; border: 1px solid var(--color-border-light); }
    .c { text-align: center; white-space: nowrap; }
    .num { font-variant-numeric: tabular-nums; font-weight: 500; }
    .w-dt { width: 92px; }
    .w-idx { width: 118px; }
    .sost { max-width: 0; } /* эллипсис в table-cell: ширину задаёт колонка, не контент */
    .ell { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .empty { text-align: center; color: var(--color-text-secondary); padding: var(--space-sm); }
  `],
})
export class ArrivalsCardComponent implements OnInit {
  private readonly api = inject(ArrivalsApiService);
  private readonly msg = inject(NzMessageService);

  /** Станция половины и её терминалы (фильтр naznach). */
  readonly station = input.required<string>();
  readonly terminals = input.required<TerminalTarget[]>();

  readonly loading = signal(false);
  readonly groups = signal<ArrivalGroup[]>([]);
  readonly expanded = signal(false);
  /** Вагонов-кандидатов на прибытие (статус 9, ждут подтверждения). */
  readonly candidatesCount = signal(0);

  /** Последние 5 прибывших, свежие сверху (группы приходят по возрастанию времени). */
  readonly topGroups = computed(() => [...this.groups()].reverse().slice(0, 5));

  ngOnInit(): void {
    void this.load();
  }

  async load(): Promise<void> {
    this.loading.set(true);
    try {
      const names = this.terminals().map((t) => t.name);
      const [res, cands] = await Promise.all([
        this.api.getArrivals(names),
        this.api.getCandidates(names),
      ]);
      this.groups.set(res.groups);
      this.candidatesCount.set((cands ?? []).reduce((n, c) => n + c.vagon_count, 0));
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.loading.set(false);
    }
  }

  /** дд.мм чч:мм (компакт — без года). */
  fmtDT(ts: string | null): string {
    if (!ts || ts.length < 16) return '—';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)} ${ts.slice(11, 16)}`;
  }

  /** Состав поезда одной строкой: display-строки подгрупп через « · ». */
  sostav(g: ArrivalGroup): string {
    return g.sub_groups.map((sg) => sg.display).join(' · ') || '—';
  }
}
