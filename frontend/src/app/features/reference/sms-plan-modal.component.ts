import { Component, ElementRef, OnInit, inject, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { DragDropModule } from '@angular/cdk/drag-drop';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzMessageService } from 'ng-zorro-antd/message';
import { toBlob } from 'html-to-image';
import { apiErrorMessage } from '../../core/api/api-error';
import {
  BroadcastResult, PlanFormLine, PlanFormTerminal, ReferenceApiService, groupTrains,
} from './reference-api.service';
import { addDaysIso, todayMsk } from '../../shared/msk-date';

/** Строка сводки: подпись + значение из линии. */
interface Row { label: string; get: (l: PlanFormLine) => string; }

/**
 * «Утренняя СМС с ПП» — форма плана подвода (перенос gtport SmsPlan, вызывается
 * из «Справок»). По терминалу карточка «ЖД сутки»: «Вчера» (факт учётного листа +
 * прибывшие), «Сегодня» (остаток/образование/простой + поезда), будущие дни
 * (плановые поезда). Картинку рисует фронт (html-to-image), рассылку по маршруту
 * (форма × терминал) делает бэк. Перемещаемая модалка (канон), печать.
 */
@Component({
  selector: 'app-sms-plan-modal',
  imports: [FormsModule, DragDropModule, NzButtonModule, NzIconModule, NzModalModule,
    NzSpinModule, NzTooltipModule],
  template: `
    <nz-modal [nzVisible]="true" [nzTitle]="ttl" [nzFooter]="null" nzWidth="1220px"
              [nzMask]="false" (nzOnCancel)="closed.emit()">
      <ng-template #ttl>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          Утренняя СМС с ПП
        </div>
      </ng-template>

      <ng-container *nzModalContent>
        <div class="bar no-print">
          <input type="date" [ngModel]="date()" (ngModelChange)="date.set($event); reload()" class="date" />
          <span class="spacer"></span>
          <button nz-button nzSize="small" (click)="reload()" nz-tooltip nzTooltipTitle="Обновить">
            <span nz-icon nzType="sync"></span>
          </button>
          <button nz-button nzSize="small" (click)="print()" nz-tooltip nzTooltipTitle="Печать">
            <span nz-icon nzType="printer"></span>
          </button>
          <button nz-button nzType="primary" nzSize="small" [nzLoading]="sending()" (click)="sendComposite()"
                  nz-tooltip nzTooltipTitle="Сводную картинку всех терминалов — в общий чат">
            <span nz-icon nzType="send"></span> Сводка в MAX
          </button>
        </div>

        @if (loading()) {
          <div class="center"><nz-spin nzSimple></nz-spin></div>
        } @else {
          <div class="grid">
            @for (f of forms(); track f.terminal) {
              <div class="term" [id]="'sp-' + f.terminal">
                <div class="term-head" [style.background]="f.color || '#eee'">{{ f.terminal }} ЖД СУТКИ</div>

                @for (d of days(f); track d.date) {
                  <div class="blk" [class.prev]="isPrev(d.date)">{{ dayLabel(d.date) }}</div>

                  @if (isPrev(d.date)) {
                    <table class="sum">
                      <thead><tr><th class="ind"></th>
                        @for (l of f.lines; track l.cargo_key) { <th>{{ l.label }}</th> }
                      </tr></thead>
                      <tbody>
                        @for (row of YEST; track row.label) {
                          <tr><td class="ind">{{ row.label }}</td>
                            @for (l of f.lines; track l.cargo_key) { <td>{{ row.get(l) }}</td> }
                          </tr>
                        }
                      </tbody>
                    </table>
                  } @else if (isToday(d.date)) {
                    <table class="sum">
                      <thead><tr><th class="ind"></th>
                        @for (l of f.lines; track l.cargo_key) { <th>{{ l.label }}</th> }
                      </tr></thead>
                      <tbody>
                        @for (row of TODAY; track row.label) {
                          <tr><td class="ind">{{ row.label }}</td>
                            @for (l of f.lines; track l.cargo_key) { <td>{{ row.get(l) }}</td> }
                          </tr>
                        }
                      </tbody>
                    </table>
                  }

                  @for (tr of d.trains; track $index) {
                    <div class="tr"><b>{{ bold(tr) }}</b>{{ rest(tr) }}</div>
                  }
                }

                <div class="term-actions no-print">
                  <button nz-button nzSize="small" (click)="exportPng(f.terminal)"
                          nz-tooltip nzTooltipTitle="Сохранить картинку">
                    <span nz-icon nzType="download"></span>
                  </button>
                  <button nz-button nzSize="small" [nzLoading]="busy() === f.terminal"
                          (click)="sendImage(f.terminal)">
                    <span nz-icon nzType="picture"></span> В MAX
                  </button>
                </div>
              </div>
            } @empty {
              <div class="empty">Нет терминалов</div>
            }
          </div>
        }
      </ng-container>
    </nz-modal>
  `,
  styles: [`
    .ttl { cursor: move; }
    .bar { display: flex; align-items: center; gap: var(--space-sm); margin-bottom: var(--space-sm); }
    .date { padding: 3px 8px; border: 1px solid var(--color-border); border-radius: var(--radius-sm); }
    .spacer { flex: 1 1 auto; }
    .center { display: flex; justify-content: center; padding: var(--space-xl); }
    .grid { display: flex; flex-wrap: wrap; gap: var(--space-md); align-items: flex-start; }

    .term { width: 360px; background: #fff; border: 1px solid var(--color-border);
            border-radius: var(--radius-card); overflow: hidden; }
    .term-head { padding: var(--space-sm); text-align: center; font-weight: 700; }
    .blk { padding: 4px 8px; background: rgba(0,0,0,.04); font-weight: 700; font-size: var(--font-size-sm);
           border-top: 1px solid var(--color-border-light); }
    .blk.prev { color: var(--color-text-secondary); }

    .sum { width: 100%; border-collapse: collapse; font-size: var(--font-size-sm); }
    .sum th, .sum td { border: 1px solid var(--color-border-light); padding: 2px 8px; text-align: center; }
    .sum th { background: var(--color-bg-subtle); font-weight: 600; }
    .sum .ind { text-align: left; white-space: nowrap; font-weight: 500; }

    .tr { padding: 3px 8px; font-size: var(--font-size-sm); border-bottom: 1px solid var(--color-border-light);
          white-space: normal; word-break: break-word; }
    .tr b { font-weight: 700; }
    .empty { padding: var(--space-sm); text-align: center; color: var(--color-text-secondary); }
    .term-actions { display: flex; gap: var(--space-xs); padding: var(--space-sm);
                    border-top: 1px solid var(--color-border-light); }

    @media print { .no-print { display: none !important; } .term { page-break-inside: avoid; } }
  `],
})
export class SmsPlanModalComponent implements OnInit {
  private readonly api = inject(ReferenceApiService);
  private readonly msg = inject(NzMessageService);
  private readonly host = inject(ElementRef<HTMLElement>);

  readonly closed = output<void>();

  readonly date = signal(todayMsk());
  readonly loading = signal(false);
  readonly sending = signal(false);
  readonly busy = signal('');
  readonly forms = signal<PlanFormTerminal[]>([]);

  readonly YEST: Row[] = [
    { label: 'Остаток на 18:', get: (l) => String(l.ost_18) },
    { label: 'Прибыло:', get: (l) => String(l.prib) },
    { label: 'Полезное образование:', get: (l) => String(l.useful_y) },
    { label: 'Образование:', get: (l) => String(l.total_y) },
    { label: 'Выгрузка:', get: (l) => String(l.vigr_fact) },
    { label: 'Остаток:', get: (l) => String(l.ost_y) },
  ];
  readonly TODAY: Row[] = [
    { label: 'Остаток:', get: (l) => String(l.ost_today) },
    { label: 'Полезное образование:', get: (l) => `${l.useful_today}/${l.total_today}` },
    { label: 'Простой порта(прогноз):', get: (l) => l.downtime_today || '0:00' },
  ];

  ngOnInit(): void { void this.reload(); }

  /** Раскладка по ЖД-суткам (порядок задан бэком: отсечка 18:00). */
  days(f: PlanFormTerminal) { return groupTrains(f.trains, 'jd'); }

  prevDate(): string { return addDaysIso(this.date(), -1); }
  isPrev(d: string): boolean { return d === this.prevDate(); }
  isToday(d: string): boolean { return d === this.date(); }

  async reload(): Promise<void> {
    this.loading.set(true);
    try {
      this.forms.set(await this.api.planForm(this.date()));
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.loading.set(false);
    }
  }

  fmtDate(d: string): string {
    return d.length >= 10 ? `${d.slice(8, 10)}.${d.slice(5, 7)}.${d.slice(0, 4)}` : d;
  }
  dayLabel(d: string): string {
    return this.isPrev(d) ? `${this.fmtDate(d)} (вчера)` : this.fmtDate(d);
  }

  private cut(tr: string): number { const i = tr.indexOf('('); return i === -1 ? tr.length : i; }
  bold(tr: string): string { return tr.slice(0, this.cut(tr)); }
  rest(tr: string): string { return tr.slice(this.cut(tr)); }

  private async png(el: HTMLElement): Promise<Blob> {
    const blob = await toBlob(el, { pixelRatio: 2, backgroundColor: '#ffffff' });
    if (!blob) throw new Error('не удалось отрисовать картинку');
    return blob;
  }

  async exportPng(terminal: string): Promise<void> {
    const el = this.host.nativeElement.querySelector(`#sp-${CSS.escape(terminal)}`) as HTMLElement | null;
    if (!el) return;
    try {
      const blob = await this.png(el);
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `План_${terminal}_${this.date()}.png`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) { this.msg.error(apiErrorMessage(err)); }
  }

  async sendImage(terminal: string): Promise<void> {
    const el = this.host.nativeElement.querySelector(`#sp-${CSS.escape(terminal)}`) as HTMLElement | null;
    if (!el) return;
    this.busy.set(terminal);
    try {
      const blob = await this.png(el);
      const res = await this.api.sendImage('spravki', terminal, blob, `План_${terminal}.png`,
        `План подвода ${terminal} ${this.fmtDate(this.date())}`);
      this.report(res, terminal);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally { this.busy.set(''); }
  }

  async sendComposite(): Promise<void> {
    const el = this.host.nativeElement.querySelector('.grid') as HTMLElement | null;
    if (!el) return;
    this.sending.set(true);
    try {
      const blob = await this.png(el);
      const res = await this.api.sendImage('plan', '', blob, `План_подвода_${this.date()}.png`,
        `План подвода ${this.fmtDate(this.date())}`);
      this.report(res, 'сводка');
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally { this.sending.set(false); }
  }

  print(): void { window.print(); }

  private report(res: BroadcastResult, what: string): void {
    if (res.chats === 0) { this.msg.warning(`${what}: нет настроенного маршрута рассылки`); return; }
    const failed = Object.keys(res.failed);
    if (failed.length) this.msg.warning(`${what}: отправлено в ${res.sent.length}, не ушло — ${failed.join(', ')}`);
    else this.msg.success(`${what}: отправлено в чаты (${res.sent.join(', ')})`);
  }
}
