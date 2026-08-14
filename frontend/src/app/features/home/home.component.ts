import { Component, OnInit, computed, inject, signal } from '@angular/core';
import { ActivatedRoute, Router } from '@angular/router';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { ArrivalsApiService, TerminalTarget } from './arrivals-api.service';
import { ArrivalsCardComponent } from './arrivals-card.component';
import { CandidatesCardComponent } from './candidates-card.component';
import { NearestCardComponent } from './nearest-card.component';
import { OperativkaApiService } from './operativka-api.service';
import { OperativkaCardComponent } from './operativka-card.component';
import { SystemStatusCardComponent } from './system-status-card.component';
import { InfoCardComponent } from './info-card.component';
import { UnplannedCardComponent } from './unplanned-card.component';
import { stationTitle } from '../../shared/station-name';

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
 * Мыс перед Находкой). В станционных колонках — блок «Кандидаты на прибытие»
 * (статусы 9 и 8 своей станции, решение владельца 10.08.2026), под ним
 * «Прибывшие» (компактный, автообновляемый, с разворотом в перемещаемую
 * модалку) и «Ближайшие поезда».
 *
 * Колонка «Оперативка» начинается со «Статуса системы» — туда перенесён весь
 * функционал бывшей страницы «Дислокация» (решение владельца): актуальность
 * снимка и планов, «Обновить из АСУ» и «Приём ЛК» перемещаемой модалкой. После
 * пересборки снимка счётчики «Оперативки» перечитываются сразу, не дожидаясь
 * минутного автообновления. Справа от статуса — карточка «Работа»
 * (пропавшие и доноры перегруза со списками по клику).
 */
@Component({
  selector: 'app-home',
  imports: [ArrivalsCardComponent, CandidatesCardComponent, NearestCardComponent, OperativkaCardComponent,
            SystemStatusCardComponent, InfoCardComponent, UnplannedCardComponent],
  template: `
    <div class="cols" [style.--col-count]="stations().length + 1">
      <section class="col">
        <h2 class="st-title">Оперативка</h2>
        <!-- Сигнал «поезд едет без плана» — в ширину колонки, над карточками
             (решение владельца 11.08.2026; прежде полоса шла над всеми тремя
             колонками). Пустой список карточка не рендерит — появляется только
             когда тревога есть. -->
        <app-unplanned-card />
        <div class="duo">
          <app-system-status-card (refreshed)="onSnapshotRebuilt()" />
          <app-info-card [openModal]="deepLink()" />
        </div>
        <!-- Суточные счётчики терминалов — во всю ширину колонки (решение
             владельца 11.08.2026): показателям тесно в полширины. -->
        <app-operativka-card [openModal]="deepLink()" />
      </section>

      @for (st of stations(); track st.code) {
        <section class="col">
          <h2 class="st-title">{{ title(st.name) }}</h2>
          <app-candidates-card [station]="title(st.name)" [terminals]="st.terminals" />
          <app-arrivals-card [station]="title(st.name)" [terminals]="st.terminals" />
          <app-nearest-card [station]="title(st.name)" [terminals]="st.terminals" />
        </section>
      } @empty {
        @if (!loading()) { <p class="mut">Нет терминалов в реестре ports.</p> }
      }
    </div>
  `,
  styles: [`
    :host { display: flex; flex-direction: column; gap: var(--space-md); }
    /* Колонок — «Оперативка» + по одной на станцию реестра ports (--col-count из
       шаблона): у клиента с одной станцией не остаётся пустой трети экрана. */
    .cols { display: grid; grid-template-columns: repeat(var(--col-count, 3), 1fr);
            gap: var(--space-lg); align-items: start; }
    .col { display: flex; flex-direction: column; gap: var(--space-md); min-width: 0; }
    /* Верх «Оперативки»: слева статус системы, справа «Работа» (обе в половину
       ширины колонки); суточные счётчики терминалов — ниже, во всю ширину. */
    .duo { display: grid; grid-template-columns: 1fr 1fr; gap: var(--space-md); align-items: start; }
    .st-title { margin: 0; font-size: var(--font-size-card-title); font-weight: 600; text-align: center; }
    .mut { color: var(--color-text-secondary); }
    @media (max-width: 1100px) { .cols { grid-template-columns: 1fr; } }
  `],
})
export class HomeComponent implements OnInit {
  private readonly api = inject(ArrivalsApiService);
  private readonly msg = inject(NzMessageService);

  /** Счётчики «Оперативки» (прибытие/выгрузка и «без плана») — освежаем сразу
   *  после пересборки снимка, обе карточки живут от одного сервиса. */
  private readonly operativka = inject(OperativkaApiService);

  readonly loading = signal(false);
  readonly terminals = signal<TerminalTarget[]>([]);

  /** Deep-link колокольчика и «Справки»: /home?open=bros|unmatched|overdue
   *  открывает модалку «Работы», open=cargo-work — «Грузовую работу»
   *  (operativka-card). Подписка (не snapshot): клик по уведомлению, когда мы УЖЕ
   *  на главной, компонент не пересоздаёт. Значение — новый объект на каждый
   *  переход, иначе повторный клик по тому же типу не сработал бы (сигналы
   *  схлопывают равные значения). Параметр стирается из адреса, чтобы
   *  обновление страницы не открывало модалку заново. */
  readonly deepLink = signal<{ kind: string } | null>(null);
  private readonly route = inject(ActivatedRoute);
  private readonly router = inject(Router);

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
    this.route.queryParamMap.subscribe((params) => {
      const open = params.get('open');
      if (!open) return;
      this.deepLink.set({ kind: open });
      void this.router.navigate([], {
        queryParams: { open: null }, queryParamsHandling: 'merge', replaceUrl: true,
      });
    });
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
    void this.operativka.load();
  }

  /** «МЫС АСТАФЬЕВА» → «Мыс Астафьева» (заголовок половины); общий с вкладками
   *  «Плана подвода» хелпер — shared/station-name.ts. */
  title(name: string): string {
    return stationTitle(name);
  }
}
