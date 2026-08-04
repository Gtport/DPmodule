import { Component, signal } from '@angular/core';
import { NzTabsModule } from 'ng-zorro-antd/tabs';
import { TrainsComponent } from '../trains/trains.component';
import { HistoryComponent } from './history.component';

/**
 * Страница «Инструменты оператора» — контейнер вкладок (раскладка gtport
 * OperatorToolsLayout без вкладки «Типовые отчеты»: отчёты в DPmodule живут
 * один раз на странице «Справки и отчёты»). Содержимое рисуется по @if —
 * невыбранная вкладка не инстанцируется: «История» не трогает сеть, пока
 * открыты «Поезда», и наоборот.
 */
@Component({
  selector: 'app-operator-tools',
  imports: [NzTabsModule, TrainsComponent, HistoryComponent],
  template: `
    <div class="page">
      <nz-tabs [nzSelectedIndex]="tab()" (nzSelectedIndexChange)="tab.set($event)">
        <nz-tab nzTitle="Поезда в движении"></nz-tab>
        <nz-tab nzTitle="Работа с историческими данными"></nz-tab>
      </nz-tabs>
      @if (tab() === 0) { <app-trains /> }
      @if (tab() === 1) { <app-history /> }
    </div>
  `,
  styles: [`
    .page { display: flex; flex-direction: column; gap: var(--space-sm); }
    nz-tabs { background: var(--color-bg-surface); border-radius: var(--radius-card);
              box-shadow: var(--shadow-card); padding: 0 var(--space-md); }
  `],
})
export class OperatorToolsComponent {
  readonly tab = signal(0);
}
