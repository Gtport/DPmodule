import { Component, signal } from '@angular/core';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { SmsOperModalComponent } from './sms-oper-modal.component';
import { SmsPlanModalComponent } from './sms-plan-modal.component';

/**
 * Экран «Справки» (перенос gtport ReferenceEditor): карточки-блоки с кнопками,
 * каждая открывает форму модалкой. Отдельной страницы под рассылку нет — формы
 * живут здесь, как в оригинале.
 *
 * Блок «Оперативка»: «Утренняя СМС с ПП» (план подвода, картинкой) и
 * «Оперативная СМС с ПП» (перечень поездов текстом, ЖД/ГР сутки). Остальные
 * блоки оригинала (Подход, Отчёты НМТП, Погрузка/Выгрузка) добавятся по мере
 * переноса соответствующих отчётов — пустых кнопок не заводим.
 */
@Component({
  selector: 'app-reference',
  imports: [NzButtonModule, SmsPlanModalComponent, SmsOperModalComponent],
  template: `
    <div class="page">
      <div class="blocks">
        <div class="card">
          <div class="head">Оперативка</div>
          <div class="body">
            <button nz-button nzBlock (click)="planOpen.set(true)">Утренняя СМС с ПП</button>
            <button nz-button nzBlock (click)="operOpen.set(true)">Оперативная СМС с ПП</button>
          </div>
        </div>
      </div>
    </div>

    @if (planOpen()) { <app-sms-plan-modal (closed)="planOpen.set(false)" /> }
    @if (operOpen()) { <app-sms-oper-modal (closed)="operOpen.set(false)" /> }
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
export class ReferenceComponent {
  readonly planOpen = signal(false);
  readonly operOpen = signal(false);
}
