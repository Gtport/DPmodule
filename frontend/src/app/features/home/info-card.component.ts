import { Component, OnDestroy, OnInit, effect, inject, input, signal } from '@angular/core';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { MissingApiService, Status6Vagon } from './missing-api.service';
import { VagonListModalComponent, VagonListRow } from './vagon-list-modal.component';
import { CargoWorkModalComponent } from './cargo-work-modal.component';
import { BrosModalComponent } from './bros-modal.component';
import { BrosApiService } from './bros-api.service';
import { DelaysApiService } from './delays-api.service';
import { DelaysModalComponent } from './delays-modal.component';
import { UnmatchedApiService } from './unmatched-api.service';
import { UnmatchedModalComponent } from './unmatched-modal.component';
import { OverdueApiService } from './overdue-api.service';
import { OverdueModalComponent } from './overdue-modal.component';
import { LongStandApiService } from './long-stand-api.service';
import { LongStandModalComponent } from './long-stand-modal.component';

/**
 * Карточка «Работа» (бывшая «Информация», решение владельца 10.08.2026) рядом
 * со «Статусом системы» (правая половина верхней строки колонки «Оперативка»):
 * счётчики списков «сбоку от снимка». Клик по строке открывает перемещаемую
 * модалку с таблицей, где ПКМ по строке даёт историю движения вагона.
 * Автообновление раз в минуту, как у соседних карточек. Пропавшие (статус 8)
 * отсюда переехали в станционные карточки «Кандидаты на прибытие» (10.08.2026).
 *
 * Имена строк — решение владельца 10.08.2026, короче внутренних терминов:
 * «Неопознанные» = без атрибуции marka, «Проблемные вагоны» = доноры перегруза
 * (статус 6), «Ввод выгрузки» = грузовая работа (суточный учётный лист).
 * Внутренние термины остались в подсказках и заголовках модалок.
 */
@Component({
  selector: 'app-info-card',
  imports: [NzIconModule, NzTooltipModule, VagonListModalComponent, CargoWorkModalComponent, BrosModalComponent, DelaysModalComponent, UnmatchedModalComponent, OverdueModalComponent, LongStandModalComponent],
  template: `
    <div class="card">
      <div class="head"><b>Работа</b></div>

      <button class="row" type="button" (click)="openUnmatched()"
              nz-tooltip nzTooltipTitle="Гружёные вагоны без атрибуции: не сматчены со справочником Marka (без грузоотправителя/клиента/СМС) — открыть список и заполнить">
        <span class="lbl">Неопознанные</span>
        <span class="cnt" [class.warn]="unmatchedCount() > 0">{{ unmatchedCount() }}</span>
        <span nz-icon nzType="right" class="go"></span>
      </button>

      <button class="row" type="button" (click)="openBros()"
              nz-tooltip nzTooltipTitle="Брошенные поезда (статус 5): журнал и коды бросания — открыть">
        <span class="lbl">Брошенные поезда</span>
        <span class="cnt" [class.warn]="brosCount() > 0">{{ brosCount() }}</span>
        <span nz-icon nzType="right" class="go"></span>
      </button>

      <button class="row" type="button" (click)="openDelays()"
              nz-tooltip nzTooltipTitle="Вагоны, задержанные в пути прямо сейчас (простои и бросания); отчёт за период — внутри">
        <span class="lbl">Задержанные вагоны</span>
        <span class="cnt">{{ delaysCount() }}</span>
        <span nz-icon nzType="right" class="go"></span>
      </button>

      <button class="row" type="button" (click)="openOverdue()"
              nz-tooltip nzTooltipTitle="Вагоны в пути с истекшим нормативным сроком доставки — по накладным; отчёт для претензионной работы (ст. 97 УЖТ) — внутри">
        <span class="lbl">Просрочка доставки</span>
        <span class="cnt" [class.warn]="overdueCount() > 0">{{ overdueCount() }}</span>
        <span nz-icon nzType="right" class="go"></span>
      </button>

      <button class="row" type="button" (click)="openLongStand()"
              [nz-tooltip]="'Вагоны, стоящие на станции назначения дольше ' + longStandHours() + ' ч с прибытия: и ждущие выгрузки, и уже выгруженные, но не убранные — открыть список'">
        <span class="lbl">Долгостой</span>
        <span class="cnt" [class.warn]="longStandCount() > 0">{{ longStandCount() }}</span>
        <span nz-icon nzType="right" class="go"></span>
      </button>

      <button class="row" type="button" (click)="openCargoWork()"
              nz-tooltip nzTooltipTitle="Грузовая работа: суточный учёт выгрузки и погрузки по терминалам — открыть">
        <span class="lbl">Ввод выгрузки</span>
        <span nz-icon nzType="right" class="go ml"></span>
      </button>

      <button class="row" type="button" (click)="openDonors()"
              nz-tooltip nzTooltipTitle="Доноры перегруза (статус 6): у них приёмники забирают груз — открыть список">
        <span class="lbl">Проблемные вагоны</span>
        <span class="cnt">{{ donors().length }}</span>
        <span nz-icon nzType="right" class="go"></span>
      </button>
    </div>

    @if (showCargoWork()) {
      <app-cargo-work-modal (closed)="showCargoWork.set(false)" />
    }
    @if (showBros()) {
      <app-bros-modal (closed)="showBros.set(false)" />
    }
    @if (showDelays()) {
      <app-delays-modal (closed)="showDelays.set(false)" />
    }
    @if (showUnmatched()) {
      <app-unmatched-modal (reload)="load()" (closed)="showUnmatched.set(false)" />
    }
    @if (showOverdue()) {
      <app-overdue-modal (closed)="showOverdue.set(false)" />
    }
    @if (showLongStand()) {
      <app-long-stand-modal (closed)="showLongStand.set(false)" />
    }
    @if (showDonors()) {
      <app-vagon-list-modal title="Проблемные вагоны" sinceLabel="Донор с"
                            hint="Доноры перегруза (статус 6): у них приёмники забирают груз и назначение."
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
    .cnt.warn { color: var(--color-danger-text); }
    .go { font-size: 10px; color: var(--color-text-muted); }
    /* У «Грузовой работы» нет счётчика — стрелку прижимаем сами. */
    .go.ml { margin-left: auto; }
  `],
})
export class InfoCardComponent implements OnInit, OnDestroy {
  private readonly api = inject(MissingApiService);
  private readonly bros = inject(BrosApiService);
  private readonly delays = inject(DelaysApiService);
  private readonly unmatched = inject(UnmatchedApiService);
  private readonly overdue = inject(OverdueApiService);
  private readonly longStand = inject(LongStandApiService);
  private readonly msg = inject(NzMessageService);

  readonly donors = signal<Status6Vagon[]>([]);
  readonly brosCount = signal(0);
  /** Открытые эпизоды задержек; без warn — задержанные есть почти всегда, красный тут не сигнал. */
  readonly delaysCount = signal(0);
  readonly unmatchedCount = signal(0);
  /** Вагоны в пути с delay > 0 (прибывшие исключены на сервере). */
  readonly overdueCount = signal(0);
  /** Долгостой: стоят на станции назначения дольше порога (статусы ≥ 10).
   *  Здесь только счётчик — список со своей группировкой тянет сама модалка. */
  readonly longStandCount = signal(0);
  /** Порог с сервера (client_settings), чтобы подписи не разъезжались с настройкой. */
  readonly longStandHours = signal(48);
  readonly showDonors = signal(false);
  readonly showCargoWork = signal(false);
  readonly showBros = signal(false);
  readonly showDelays = signal(false);
  readonly showUnmatched = signal(false);
  readonly showOverdue = signal(false);
  readonly showLongStand = signal(false);

  /** Deep-link колокольчика (/home?open=…): открыть модалку. Объект, не строка —
   *  повторный переход тем же типом должен открыть модалку снова. */
  readonly openModal = input<{ kind: string } | null>(null);

  private timer: ReturnType<typeof setInterval> | null = null;

  constructor() {
    effect(() => {
      const m = this.openModal();
      if (m?.kind === 'bros') this.showBros.set(true);
      else if (m?.kind === 'unmatched') this.showUnmatched.set(true);
      else if (m?.kind === 'overdue') this.showOverdue.set(true);
    });
  }

  ngOnInit(): void {
    void this.load(true);
    this.timer = setInterval(() => void this.load(), 60_000);
  }

  ngOnDestroy(): void {
    if (this.timer) clearInterval(this.timer);
  }

  /** Списки короткие (снятие доноров) — тянем целиком, счётчик = длина. */
  async load(initial = false): Promise<void> {
    try {
      const [donors, bros, unm, delays, overdue, stand] = await Promise.all([
        this.api.getStatus6(), this.bros.getActive(), this.unmatched.getGroups(),
        this.delays.current(), this.overdue.getGroups(), this.longStand.getList(),
      ]);
      this.donors.set(donors ?? []);
      this.brosCount.set(bros?.length ?? 0);
      this.unmatchedCount.set((unm ?? []).reduce((s, g) => s + g.vagon_count, 0));
      this.delaysCount.set(delays?.length ?? 0);
      this.overdueCount.set((overdue ?? []).reduce((s, g) => s + g.vagon_count, 0));
      this.longStandCount.set(stand?.vagons?.length ?? 0);
      if (stand?.threshold_hours) this.longStandHours.set(stand.threshold_hours);
    } catch (err) {
      if (initial) this.msg.error(apiErrorMessage(err));
    }
  }

  openDonors(): void { this.showDonors.set(true); }
  openCargoWork(): void { this.showCargoWork.set(true); }
  openBros(): void { this.showBros.set(true); }
  openDelays(): void { this.showDelays.set(true); }
  openUnmatched(): void { this.showUnmatched.set(true); }
  openOverdue(): void { this.showOverdue.set(true); }
  openLongStand(): void { this.showLongStand.set(true); }

  /** Доноры перегруза → общая форма строки таблицы. */
  donorRows(): VagonListRow[] {
    return this.donors().map((r) => ({
      id: r.id, vagon: r.vagon, index: r.index,
      station_oper: r.station_oper, doroga_oper: r.doroga_oper, oper_s: r.oper_s,
      time_op: r.time_op, naznach: r.naznach,
      station_nach: r.station_nach, gruzotpr: r.gruzotpr,
      cargo_s: r.cargo_s, ves: r.ves,
      since: r.since, days: r.days_donor,
    }));
  }
}
