import { Component, OnDestroy, OnInit, computed, inject, input, signal } from '@angular/core';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { StaleTracker } from '../../shared/stale';
import { ArrivalsApiService, CandidateGroup, TerminalTarget } from './arrivals-api.service';
import { CandidatesModalComponent } from './candidates-modal.component';
import { MissingApiService, MissingGroup } from './missing-api.service';

/** Строка компактной таблицы: живой кандидат (статус 9) или пропавший (статус 8). */
interface CandidateRow {
  key: string;
  kind: 'cand' | 'missing';
  index: string;
  sostav: string;
  /** Давность пропажи (только у пропавших) — в чипе «пропал Nд». */
  days: number;
  hint: string;
}

/**
 * Компактная карточка «Кандидаты на прибытие» половины станции (над
 * «Прибывшими», решение владельца 10.08.2026): всё, что вот-вот станет
 * «прибывшим», одним списком — живые кандидаты (статус 9: на станции
 * назначения, АСУ не дала дату) и пропавшие из дислокации (статус 8: исчезли
 * из выгрузки в незавершённом рейсе — вероятно, прибыли). Разворот открывает
 * перемещаемую модалку с действиями «Подтвердить прибытие»/«Скрыть»; вагон,
 * вернувшийся в дислокацию, уходит из пропавших автоматически.
 */
@Component({
  selector: 'app-candidates-card',
  imports: [NzButtonModule, NzIconModule, NzTooltipModule, CandidatesModalComponent],
  template: `
    <div class="card">
      <div class="head">
        <b>Кандидаты на прибытие</b>
        @if (stale.stale()) {
          <span class="dp-stale" nz-tooltip
                nzTooltipTitle="Автообновление не проходит — показаны последние полученные данные">{{ stale.label() }}</span>
        }
        @if (total()) { <span class="cnt">{{ total() }} ваг.</span> }
        <span class="spacer"></span>
        <button nz-button nzType="text" nzSize="small" nz-tooltip
                nzTooltipTitle="Кандидаты и пропавшие (полная таблица, подтверждение прибытия)"
                (click)="expanded.set(true)">
          <span nz-icon nzType="expand-alt"></span>
        </button>
      </div>
      <table class="mini">
        <thead>
          <tr><th class="w-kind">Статус</th><th class="w-idx">Индекс</th><th>Состав</th></tr>
        </thead>
        <tbody>
          @for (r of rows(); track r.key) {
            <tr>
              <td class="c">
                <span class="tag" [class.miss]="r.kind === 'missing'" nz-tooltip [nzTooltipTitle]="r.hint"
                      (click)="expanded.set(true)">{{ r.kind === 'missing' ? 'пропал ' + r.days + 'д' : 'ждёт' }}</span>
              </td>
              <td class="c num">{{ r.index || '—' }}</td>
              <td class="sost ell" [title]="r.sostav">{{ r.sostav }}</td>
            </tr>
          } @empty {
            <tr><td colspan="3" class="empty">{{ loading() ? 'Загрузка…' : 'Нет кандидатов' }}</td></tr>
          }
        </tbody>
      </table>
    </div>

    @if (expanded()) {
      <app-candidates-modal [station]="station()" [terminals]="terminals()"
                            (closed)="onModalClosed()" (changed)="load()" />
    }
  `,
  styles: [`
    .card { background: var(--color-bg-surface); border-radius: var(--radius-card);
            box-shadow: var(--shadow-card); padding: var(--space-sm) var(--space-md) var(--space-md); }
    .head { display: flex; align-items: center; gap: var(--space-sm); margin-bottom: var(--space-xs); }
    .spacer { flex: 1 1 auto; }
    .cnt { color: var(--color-text-secondary); font-size: var(--font-size-sm);
           font-variant-numeric: tabular-nums; }
    .mini { width: 100%; border-collapse: collapse; font-size: var(--font-size-sm); }
    .mini th { background: var(--color-bg-subtle); font-weight: 600; padding: 3px 8px;
               border: 1px solid var(--color-border-light); }
    .mini td { padding: 3px 8px; border: 1px solid var(--color-border-light); }
    .c { text-align: center; white-space: nowrap; }
    .num { font-variant-numeric: tabular-nums; font-weight: 500; }
    .w-kind { width: 84px; }
    .w-idx { width: 118px; }
    /* Чип вида строки (канон: без заливки, контур и текст в цвете): жёлтый —
       живой кандидат ждёт подтверждения, красный — пропал из дислокации. */
    .tag { border: 1px solid var(--color-warning); color: var(--color-warning-text);
           border-radius: 10px; padding: 0 8px; font-size: var(--font-size-sm);
           font-weight: 600; cursor: pointer; white-space: nowrap; }
    .tag.miss { border-color: var(--color-danger); color: var(--color-danger-text); }
    .sost { max-width: 0; } /* эллипсис в table-cell: ширину задаёт колонка, не контент */
    .ell { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .empty { text-align: center; color: var(--color-text-secondary); padding: var(--space-sm); }
  `],
})
export class CandidatesCardComponent implements OnInit, OnDestroy {
  private readonly arrivalsApi = inject(ArrivalsApiService);
  private readonly missingApi = inject(MissingApiService);
  private readonly msg = inject(NzMessageService);

  /** Станция половины и её терминалы (фильтр naznach). */
  readonly station = input.required<string>();
  readonly terminals = input.required<TerminalTarget[]>();

  readonly loading = signal(false);
  /** Свежесть фоновых обновлений: бейдж «данные N мин» при сбоях. */
  readonly stale = new StaleTracker();
  readonly cands = signal<CandidateGroup[]>([]);
  readonly missing = signal<MissingGroup[]>([]);
  readonly expanded = signal(false);

  /** Автообновление раз в минуту — фоновое, как у соседних карточек. */
  private timer: ReturnType<typeof setInterval> | null = null;

  readonly total = computed(() =>
    this.cands().reduce((n, c) => n + c.vagon_count, 0) +
    this.missing().reduce((n, g) => n + g.vagon_count, 0));

  /** Живые кандидаты первыми (они ждут действия прямо сейчас), затем пропавшие
   *  свежими первыми (порядок бэкенда). */
  readonly rows = computed<CandidateRow[]>(() => [
    ...this.cands().map((c) => ({
      key: 'c|' + c.key, kind: 'cand' as const, index: c.index,
      sostav: c.sub_groups.map((sg) => sg.display).join(' · ') || '—', days: 0,
      hint: 'На станции назначения, АСУ не дала дату прибытия — подтвердите или скройте (открыть)',
    })),
    ...this.missing().map((g) => ({
      key: 'm|' + g.key, kind: 'missing' as const, index: g.index,
      sostav: g.sub_groups.map((sg) => sg.display).join(' · ') || '—', days: g.days_missing,
      hint: 'Исчез из дислокации в незавершённом рейсе — вероятно, прибыл; подтвердите факт (открыть)',
    })),
  ]);

  ngOnInit(): void {
    void this.load(true);
    this.timer = setInterval(() => void this.load(), 60_000);
  }

  ngOnDestroy(): void {
    if (this.timer) clearInterval(this.timer);
  }

  /** initial — первая загрузка: «Загрузка…» и тост при ошибке; фоновые
   *  обновления молчаливые (контент подменяется без мигания). */
  async load(initial = false): Promise<void> {
    if (initial) this.loading.set(true);
    try {
      const names = this.terminals().map((t) => t.name);
      const [cands, missing] = await Promise.all([
        this.arrivalsApi.getCandidates(names),
        this.missingApi.getMissingGroups(names),
      ]);
      this.cands.set(cands ?? []);
      this.missing.set(missing ?? []);
      this.stale.ok();
    } catch (err) {
      this.stale.fail();
      if (initial) this.msg.error(apiErrorMessage(err));
    } finally {
      if (initial) this.loading.set(false);
    }
  }

  /** Закрытие модалки: подтянуть результат правок (подтверждения/скрытия). */
  onModalClosed(): void {
    this.expanded.set(false);
    void this.load();
  }
}
