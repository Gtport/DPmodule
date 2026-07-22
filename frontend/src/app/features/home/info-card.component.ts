import { Component, OnDestroy, OnInit, inject, signal } from '@angular/core';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { MissingApiService, MissingVagon, Status6Vagon } from '../missing/missing-api.service';
import { VagonListModalComponent, VagonListRow } from './vagon-list-modal.component';
import { CargoWorkModalComponent } from './cargo-work-modal.component';

/**
 * Карточка «Информация» рядом со «Статусом системы» (правая половина верхней
 * строки колонки «Оперативка»): счётчики списков «сбоку от снимка» — пропавшие
 * (статус 8) и доноры перегруза (статус 6). Клик по счётчику открывает
 * перемещаемую модалку с таблицей, где ПКМ по строке даёт историю движения
 * вагона. Автообновление раз в минуту, как у соседних карточек.
 */
@Component({
  selector: 'app-info-card',
  imports: [NzIconModule, NzTooltipModule, VagonListModalComponent, CargoWorkModalComponent],
  template: `
    <div class="card">
      <div class="head"><b>Информация</b></div>

      <button class="row" type="button" (click)="openMissing()"
              nz-tooltip nzTooltipTitle="Исчезли из выгрузки в незавершённом рейсе — открыть список">
        <span class="lbl">Пропавшие</span>
        <span class="cnt" [class.warn]="missing().length > 0">{{ missing().length }}</span>
        <span nz-icon nzType="right" class="go"></span>
      </button>

      <button class="row" type="button" (click)="openDonors()"
              nz-tooltip nzTooltipTitle="Доноры перегруза (статус 6): у них приёмники забирают груз — открыть список">
        <span class="lbl">Перегруз (ст. 6)</span>
        <span class="cnt">{{ donors().length }}</span>
        <span nz-icon nzType="right" class="go"></span>
      </button>

      <button class="row" type="button" (click)="openCargoWork()"
              nz-tooltip nzTooltipTitle="Суточный учёт выгрузки и погрузки по терминалам — открыть">
        <span class="lbl">Грузовая работа</span>
        <span nz-icon nzType="right" class="go ml"></span>
      </button>
    </div>

    @if (showMissing()) {
      <app-vagon-list-modal title="Пропавшие вагоны" sinceLabel="Пропал"
                            hint="Исчезли из выгрузки до завершения рейса; показана последняя известная позиция."
                            [rows]="missingRows()" (reload)="load()" (closed)="showMissing.set(false)" />
    }
    @if (showCargoWork()) {
      <app-cargo-work-modal (closed)="showCargoWork.set(false)" />
    }
    @if (showDonors()) {
      <app-vagon-list-modal title="Перегруз — доноры (статус 6)" sinceLabel="Донор с"
                            hint="Вагоны, у которых приёмники забирают груз и назначение."
                            [rows]="donorRows()" (reload)="load()" (closed)="showDonors.set(false)" />
    }
  `,
  styles: [`
    .card { background: var(--color-bg-surface); border-radius: var(--radius-card);
            box-shadow: var(--shadow-card); padding: var(--space-sm) var(--space-md) var(--space-sm); }
    /* Шапка — как у соседних карточек страницы (один размер заголовка). */
    .head { margin-bottom: var(--space-xs); }
    .row { display: flex; align-items: center; gap: var(--space-sm); width: 100%;
           padding: 3px 4px; border: none; background: none; cursor: pointer; text-align: left;
           border-radius: var(--radius-sm); font-size: var(--font-size-sm); color: inherit; }
    .row:hover { background: var(--color-bg-hover); }
    /* Текст — как в таблицах «Прибывшие»/«Ближайшие»: основной цвет, не серый. */
    .lbl { color: inherit; }
    .cnt { margin-left: auto; font-variant-numeric: tabular-nums; font-weight: 600; }
    .cnt.warn { color: var(--color-danger); }
    .go { font-size: 10px; color: var(--color-text-muted); }
    /* У «Грузовой работы» нет счётчика — стрелку прижимаем сами. */
    .go.ml { margin-left: auto; }
  `],
})
export class InfoCardComponent implements OnInit, OnDestroy {
  private readonly api = inject(MissingApiService);
  private readonly msg = inject(NzMessageService);

  readonly missing = signal<MissingVagon[]>([]);
  readonly donors = signal<Status6Vagon[]>([]);
  readonly showMissing = signal(false);
  readonly showDonors = signal(false);
  readonly showCargoWork = signal(false);

  private timer: ReturnType<typeof setInterval> | null = null;

  ngOnInit(): void {
    void this.load(true);
    this.timer = setInterval(() => void this.load(), 60_000);
  }

  ngOnDestroy(): void {
    if (this.timer) clearInterval(this.timer);
  }

  /** Списки короткие (TTL-очистка и снятие доноров) — тянем целиком, счётчик = длина. */
  async load(initial = false): Promise<void> {
    try {
      const [missing, donors] = await Promise.all([this.api.getMissing(), this.api.getStatus6()]);
      this.missing.set(missing ?? []);
      this.donors.set(donors ?? []);
    } catch (err) {
      if (initial) this.msg.error(apiErrorMessage(err));
    }
  }

  openMissing(): void { this.showMissing.set(true); }
  openDonors(): void { this.showDonors.set(true); }
  openCargoWork(): void { this.showCargoWork.set(true); }

  /** Пропавшие → общая форма строки таблицы. */
  missingRows(): VagonListRow[] {
    return this.missing().map((r) => ({
      id: r.id, vagon: r.vagon, index: r.index,
      station_oper: r.station_oper, doroga_oper: r.doroga_oper, oper_s: r.oper_s,
      time_op: r.time_op, naznach: r.naznach, cargo_s: r.cargo_s, ves: r.ves,
      since: r.missing_since, days: r.days_missing,
    }));
  }

  /** Доноры перегруза → та же форма (различие только в подписи давности). */
  donorRows(): VagonListRow[] {
    return this.donors().map((r) => ({
      id: r.id, vagon: r.vagon, index: r.index,
      station_oper: r.station_oper, doroga_oper: r.doroga_oper, oper_s: r.oper_s,
      time_op: r.time_op, naznach: r.naznach, cargo_s: r.cargo_s, ves: r.ves,
      since: r.since, days: r.days_donor,
    }));
  }
}
