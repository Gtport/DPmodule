import { Component, OnInit, computed, inject, signal } from '@angular/core';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { ArrivalsApiService, TerminalTarget } from './arrivals-api.service';
import { ArrivalsCardComponent } from './arrivals-card.component';

/** Половина рабочей зоны: станция и её терминалы (из реестра ports). */
interface StationHalf {
  code: string;
  name: string;
  terminals: TerminalTarget[];
}

/**
 * Домашняя страница — рабочая зона диспетчера: по половине на каждую станцию
 * предприятия (раскладка из реестра терминалов, не хардкод; порядок — по коду
 * станции: Находка слева, Мыс справа). В каждой половине — блок «Прибывшие»
 * (компактный, с разворотом в перемещаемую модалку); блоки «Ближайшие поезда»
 * и «Информация» — следующие итерации.
 */
@Component({
  selector: 'app-home',
  imports: [ArrivalsCardComponent],
  template: `
    <div class="halves">
      @for (st of stations(); track st.code) {
        <section class="half">
          <h2 class="st-title">{{ title(st.name) }}</h2>
          <app-arrivals-card [station]="title(st.name)" [terminals]="st.terminals" />
          <div class="soon">Ближайшие поезда · Информация — скоро</div>
        </section>
      } @empty {
        @if (!loading()) { <p class="mut">Нет терминалов в реестре ports.</p> }
      }
    </div>
  `,
  styles: [`
    .halves { display: grid; grid-template-columns: 1fr 1fr; gap: var(--space-lg); align-items: start; }
    .half { display: flex; flex-direction: column; gap: var(--space-md); min-width: 0; }
    .st-title { margin: 0; font-size: var(--font-size-card-title); font-weight: 600; text-align: center; }
    .soon { color: var(--color-text-muted); font-size: var(--font-size-sm); text-align: center; }
    .mut { color: var(--color-text-secondary); }
    @media (max-width: 900px) { .halves { grid-template-columns: 1fr; } }
  `],
})
export class HomeComponent implements OnInit {
  private readonly api = inject(ArrivalsApiService);
  private readonly msg = inject(NzMessageService);

  readonly loading = signal(false);
  readonly terminals = signal<TerminalTarget[]>([]);

  /** Станции из реестра терминалов; порядок — по 4-значному коду станции
   *  (9847 Находка раньше 9857 Мыс — левая/правая половины по решению владельца). */
  readonly stations = computed<StationHalf[]>(() => {
    const byCode = new Map<string, StationHalf>();
    for (const t of this.terminals()) {
      if (!t.station_code) continue;
      const st = byCode.get(t.station_code) ?? { code: t.station_code, name: t.station, terminals: [] };
      st.terminals.push(t);
      byCode.set(t.station_code, st);
    }
    return [...byCode.values()].sort((a, b) => a.code.localeCompare(b.code));
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

  /** «МЫС АСТАФЬЕВА» → «Мыс Астафьева» (заголовок половины). */
  title(name: string): string {
    return name.toLowerCase().replace(/(^|[\s-])\p{L}/gu, (m) => m.toUpperCase());
  }
}
