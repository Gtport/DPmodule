import { Component, OnInit, computed, inject, signal, viewChild } from '@angular/core';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { ArrivalsApiService, TerminalTarget } from './arrivals-api.service';
import { ArrivalsCardComponent } from './arrivals-card.component';
import { NearestCardComponent } from './nearest-card.component';
import { OperativkaCardComponent } from './operativka-card.component';
import { SystemStatusCardComponent } from './system-status-card.component';
import { InfoCardComponent } from './info-card.component';

/** Половина рабочей зоны: станция и её терминалы (из реестра ports). */
interface StationHalf {
  code: string;
  name: string;
  terminals: TerminalTarget[];
}

/**
 * Домашняя страница — рабочая зона диспетчера: три колонки равной ширины
 * (решение владельца): «Оперативка» + по колонке на каждую станцию предприятия
 * (раскладка из реестра терминалов, не хардкод; порядок станций — по коду,
 * Мыс перед Находкой). В станционных колонках — блок «Прибывшие» (компактный,
 * автообновляемый, с разворотом в перемещаемую модалку) и «Ближайшие поезда»;
 * блок «Информация» — следующая итерация.
 *
 * Колонка «Оперативка» начинается со «Статуса системы» — туда перенесён весь
 * функционал бывшей страницы «Дислокация» (решение владельца): актуальность
 * снимка и планов, «Обновить из АСУ» и «Приём ЛК» перемещаемой модалкой. После
 * пересборки снимка счётчики «Оперативки» перечитываются сразу, не дожидаясь
 * минутного автообновления. Справа от статуса — карточка «Информация»
 * (пропавшие и доноры перегруза со списками по клику).
 */
@Component({
  selector: 'app-home',
  imports: [ArrivalsCardComponent, NearestCardComponent, OperativkaCardComponent, SystemStatusCardComponent, InfoCardComponent],
  template: `
    <div class="cols">
      <section class="col">
        <h2 class="st-title">Оперативка</h2>
        <div class="duo">
          <app-system-status-card (refreshed)="onSnapshotRebuilt()" />
          <app-info-card />
        </div>
        <app-operativka-card />
      </section>

      @for (st of stations(); track st.code) {
        <section class="col">
          <h2 class="st-title">{{ title(st.name) }}</h2>
          <app-arrivals-card [station]="title(st.name)" [terminals]="st.terminals" />
          <app-nearest-card [station]="title(st.name)" [terminals]="st.terminals" />
          <div class="soon">Информация — скоро</div>
        </section>
      } @empty {
        @if (!loading()) { <p class="mut">Нет терминалов в реестре ports.</p> }
      }
    </div>
  `,
  styles: [`
    .cols { display: grid; grid-template-columns: repeat(3, 1fr); gap: var(--space-lg); align-items: start; }
    .col { display: flex; flex-direction: column; gap: var(--space-md); min-width: 0; }
    /* Верх «Оперативки»: статус системы и информация — по половине ширины колонки. */
    .duo { display: grid; grid-template-columns: 1fr 1fr; gap: var(--space-md); align-items: start; }
    .st-title { margin: 0; font-size: var(--font-size-card-title); font-weight: 600; text-align: center; }
    .oper { padding: var(--space-sm) var(--space-md) var(--space-md); }
    .oper-empty { color: var(--color-text-muted); font-size: var(--font-size-sm); text-align: center;
                  padding: var(--space-lg) 0; }
    .soon { color: var(--color-text-muted); font-size: var(--font-size-sm); text-align: center; }
    .mut { color: var(--color-text-secondary); }
    @media (max-width: 1100px) { .cols { grid-template-columns: 1fr; } }
  `],
})
export class HomeComponent implements OnInit {
  private readonly api = inject(ArrivalsApiService);
  private readonly msg = inject(NzMessageService);

  /** Счётчики «Оперативки» — освежаем сразу после пересборки снимка. */
  private readonly operativka = viewChild(OperativkaCardComponent);

  readonly loading = signal(false);
  readonly terminals = signal<TerminalTarget[]>([]);

  /** Станции из реестра терминалов; порядок — по 4-значному коду станции по
   *  убыванию (9857 Мыс раньше 9847 Находка — раскладка трёх колонок по решению
   *  владельца: Оперативка · Мыс · Находка). */
  readonly stations = computed<StationHalf[]>(() => {
    const byCode = new Map<string, StationHalf>();
    for (const t of this.terminals()) {
      if (!t.station_code) continue;
      const st = byCode.get(t.station_code) ?? { code: t.station_code, name: t.station, terminals: [] };
      st.terminals.push(t);
      byCode.set(t.station_code, st);
    }
    return [...byCode.values()].sort((a, b) => b.code.localeCompare(a.code));
  });

  ngOnInit(): void {
    void this.load();
  }

  async load(): Promise<void> {
    this.loading.set(true);
    try {
      this.terminals.set(await this.api.getTerminals());
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.loading.set(false);
    }
  }

  /** Дислокация пересобрана (АСУ/ЛК из статус-панели) — счётчики устарели. */
  onSnapshotRebuilt(): void {
    void this.operativka()?.load();
  }

  /** «МЫС АСТАФЬЕВА» → «Мыс Астафьева» (заголовок половины). */
  title(name: string): string {
    return name.toLowerCase().replace(/(^|[\s-])\p{L}/gu, (m) => m.toUpperCase());
  }
}
