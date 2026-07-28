import { Component, signal } from '@angular/core';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { BrosModalComponent } from '../home/bros-modal.component';
import { SmsOperModalComponent } from './sms-oper-modal.component';
import { SmsPlanModalComponent } from './sms-plan-modal.component';

/**
 * Экран «Справки и отчёты» — единый каталог отчётных форм (перенос gtport
 * Tools.tsx). В оригинале тот же набор был скопирован в три вкладки («Справки»,
 * «Справки и отчёты», «Инструменты оператора → Типовые отчеты») — здесь он
 * живёт один раз; страница «Справки» влита сюда (решение владельца 29.07.2026).
 *
 * Карточки-блоки с кнопками, каждая открывает форму перемещаемой модалкой.
 * Блок «Оперативка»: «Утренняя СМС с ПП», «Оперативная СМС с ПП» и «Брошенные
 * поезда» (модалка из features/home). Остальные блоки оригинала (Подход,
 * Погрузка/Выгрузка, Повагонка, Отчёты НМТП) добавляются по мере переноса
 * соответствующих отчётов — пустых кнопок не заводим.
 */
@Component({
  selector: 'app-reports',
  imports: [NzButtonModule, SmsPlanModalComponent, SmsOperModalComponent, BrosModalComponent],
  template: `
    <div class="page">
      <div class="blocks">
        <div class="card">
          <div class="head">Оперативка</div>
          <div class="body">
            <button nz-button nzBlock (click)="planOpen.set(true)">Утренняя СМС с ПП</button>
            <button nz-button nzBlock (click)="operOpen.set(true)">Оперативная СМС с ПП</button>
            <button nz-button nzBlock (click)="brosOpen.set(true)">Брошенные поезда</button>
          </div>
        </div>
      </div>
    </div>

    @if (planOpen()) { <app-sms-plan-modal (closed)="planOpen.set(false)" /> }
    @if (operOpen()) { <app-sms-oper-modal (closed)="operOpen.set(false)" /> }
    @if (brosOpen()) { <app-bros-modal (closed)="brosOpen.set(false)" /> }
  `,
  styles: [`
    .page { padding: var(--space-lg); display: flex; justify-content: center; align-items: flex-start; }
    .blocks { display: flex; flex-wrap: wrap; gap: var(--space-md); }
    .card { width: 260px; background: var(--color-bg-surface); border: 1px solid var(--color-border);
            border-top: 6px solid var(--color-primary); border-radius: var(--radius-card);
            box-shadow: var(--shadow-card); overflow: hidden; }
    .head { padding: var(--space-sm) var(--space-md); background: var(--color-bg-subtle);
            border-bottom: 1px solid var(--color-border-light); font-weight: 600; }
    .body { padding: var(--space-md); display: flex; flex-direction: column; gap: var(--space-sm); }
  `],
})
export class ReportsComponent {
  readonly planOpen = signal(false);
  readonly operOpen = signal(false);
  readonly brosOpen = signal(false);
}
