import { Component, signal } from '@angular/core';
import { NzTabsModule } from 'ng-zorro-antd/tabs';
import { ForecastComponent } from './forecast.component';
import { GtForecastComponent } from './gt/gt-forecast.component';

/**
 * Страница «Прогнозы» — контейнер вкладок (как gtport Forecasts.tsx, где жили
 * «Новый прогноз» и «Прогноз Макс»): вкладка 0 — «Новый прогноз» (перенос
 * PrognozNew), вкладка 1 — «Прогноз прибытия/выгрузки» (перенос страницы GT).
 * Содержимое по @if — невыбранная вкладка не инстанцируется и не ходит в сеть.
 */
@Component({
  selector: 'app-forecasts-page',
  imports: [NzTabsModule, ForecastComponent, GtForecastComponent],
  template: `
    <div class="page">
      <nz-tabs [nzSelectedIndex]="tab()" (nzSelectedIndexChange)="tab.set($event)">
        <nz-tab nzTitle="Новый прогноз"></nz-tab>
        <nz-tab nzTitle="Прогноз прибытия/выгрузки"></nz-tab>
      </nz-tabs>
      @if (tab() === 0) { <app-forecast /> }
      @if (tab() === 1) { <app-gt-forecast /> }
    </div>
  `,
  styles: [`
    .page { display: flex; flex-direction: column; gap: var(--space-sm); }
    nz-tabs { background: var(--color-bg-surface); border-radius: var(--radius-card);
              box-shadow: var(--shadow-card); padding: 0 var(--space-md); }
  `],
})
export class ForecastsPageComponent {
  readonly tab = signal(0);
}
