import { Component, ElementRef, OnInit, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { DragDropModule } from '@angular/cdk/drag-drop';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzMessageService } from 'ng-zorro-antd/message';
import { toBlob } from 'html-to-image';
import { apiErrorMessage } from '../../core/api/api-error';
import {
  BroadcastApiService, BroadcastResult, PlanFormLine, PlanFormTerminal,
} from './broadcast-api.service';
import { addDaysIso, todayMsk } from '../../shared/msk-date';

/** Строка сводки: подпись + значение из линии. */
interface Row { label: string; get: (l: PlanFormLine) => string; }

/**
 * Экран «Рассылка» (перенос gtport SmsPlan/SmsOper): по терминалу карточка «ЖД
 * сутки» под картинку. По ЖД-датам сверху вниз: «Вчера» (факт учётного листа +
 * прибывшие поезда), «Сегодня» (остаток + прогноз + поезда), будущие дни (только
 * плановые поезда). Строки поездов приходят готовыми с бэка (формат gtport:
 * середина индекса, «приб», подгруппы «(N) середина SMS от терминал»).
 *
 * Картинку рисует фронт (html-to-image); текст оперативки собирается тут же.
 * Карточки перемещаемы (cdkDrag) и печатаются.
 */
@Component({
  selector: 'app-broadcast',
  imports: [FormsModule, DragDropModule, NzButtonModule, NzIconModule, NzSpinModule, NzTooltipModule],
  template: `
    <div class="page">
      <div class="toolbar no-print">
        <b class="ttl">Рассылка форм</b>
        <input type="date" [ngModel]="date()" (ngModelChange)="date.set($event); reload()" class="date" />
        <span class="spacer"></span>
        <button nz-button nzSize="small" (click)="reload()" nz-tooltip nzTooltipTitle="Обновить данные">
          <span nz-icon nzType="sync"></span>
        </button>
        <button nz-button nzSize="small" (click)="print()" nz-tooltip nzTooltipTitle="Печать карточек">
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
        <div class="grid" #grid>
          @for (f of forms(); track f.terminal) {
            <div class="term" cdkDrag [id]="'term-' + f.terminal">
              <div class="term-head" [style.background]="f.color || '#eee'" cdkDragHandle>
                {{ f.terminal }} ЖД СУТКИ
              </div>

              @for (d of f.days; track d.date) {
                <div class="blk" [class.prev]="isPrev(d.date)">{{ dayLabel(d.date) }}</div>

                <!-- Сводка: вчера (6 строк) под своей датой, сегодня (3 строки) под своей -->
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

                <!-- Поезда дня -->
                @for (tr of d.trains; track $index) {
                  <div class="tr"><b>{{ trainBold(tr) }}</b>{{ trainRest(tr) }}</div>
                }
              } @empty {
                <div class="empty">Нет данных</div>
              }

              <div class="term-actions no-print">
                <button nz-button nzSize="small" (click)="exportPng(f.terminal)"
                        nz-tooltip nzTooltipTitle="Сохранить картинку">
                  <span nz-icon nzType="download"></span>
                </button>
                <button nz-button nzSize="small" [nzLoading]="busy() === f.terminal + ':img'"
                        (click)="sendImage(f.terminal)">
                  <span nz-icon nzType="picture"></span> Картинкой
                </button>
                <button nz-button nzSize="small" [nzLoading]="busy() === f.terminal + ':txt'"
                        (click)="sendText(f)">
                  <span nz-icon nzType="message"></span> Текстом
                </button>
              </div>
            </div>
          } @empty {
            <div class="empty">Нет терминалов</div>
          }
        </div>
      }
    </div>
  `,
  styles: [`
    .page { padding: var(--space-md); }
    .toolbar { display: flex; align-items: center; gap: var(--space-sm); margin-bottom: var(--space-md); }
    .ttl { font-size: var(--font-size-lg); }
    .date { padding: 3px 8px; border: 1px solid var(--color-border); border-radius: var(--radius-sm); }
    .spacer { flex: 1 1 auto; }
    .center { display: flex; justify-content: center; padding: var(--space-xl); }
    .grid { display: flex; flex-wrap: wrap; gap: var(--space-md); align-items: flex-start; }

    .term { width: 360px; background: #fff; border: 1px solid var(--color-border);
            border-radius: var(--radius-card); overflow: hidden; box-shadow: var(--shadow-card); }
    .term-head { padding: var(--space-sm); text-align: center; font-weight: 700; cursor: move; }
    .blk { padding: 4px 8px; background: rgba(0,0,0,.04); font-weight: 700; font-size: var(--font-size-sm);
           border-top: 1px solid var(--color-border-light); }
    .blk.prev { color: var(--color-text-secondary); } /* «вчера» приглушённо, как gtport */

    .sum { width: 100%; border-collapse: collapse; font-size: var(--font-size-sm); }
    .sum th, .sum td { border: 1px solid var(--color-border-light); padding: 2px 8px; text-align: center; }
    .sum th { background: var(--color-bg-subtle); font-weight: 600; }
    .sum .ind { text-align: left; white-space: nowrap; font-weight: 500; }

    .tr { padding: 3px 8px; font-size: var(--font-size-sm); border-bottom: 1px solid var(--color-border-light);
          white-space: normal; word-break: break-word; }
    .tr b { font-weight: 700; }
    .empty { padding: var(--space-sm); text-align: center; color: var(--color-text-secondary);
             font-size: var(--font-size-sm); }

    .term-actions { display: flex; gap: var(--space-xs); padding: var(--space-sm);
                    border-top: 1px solid var(--color-border-light); }

    @media print {
      .no-print { display: none !important; }
      .term { box-shadow: none; page-break-inside: avoid; }
    }
  `],
})
export class BroadcastComponent implements OnInit {
  private readonly api = inject(BroadcastApiService);
  private readonly msg = inject(NzMessageService);
  private readonly host = inject(ElementRef<HTMLElement>);

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

  ngOnInit(): void {
    void this.reload();
  }

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

  /** Жирный префикс строки поезда — до первой подгруппы «(»; остальное обычным. */
  private splitAt(tr: string): number {
    const i = tr.indexOf('(');
    return i === -1 ? tr.length : i;
  }
  trainBold(tr: string): string { return tr.slice(0, this.splitAt(tr)); }
  trainRest(tr: string): string { return tr.slice(this.splitAt(tr)); }

  // ── Экспорт / рассылка ──────────────────────────────────────────────────

  private cardEl(terminal: string): HTMLElement | null {
    return this.host.nativeElement.querySelector(`#term-${CSS.escape(terminal)}`);
  }

  private async png(el: HTMLElement): Promise<Blob> {
    const blob = await toBlob(el, { pixelRatio: 2, backgroundColor: '#ffffff' });
    if (!blob) throw new Error('не удалось отрисовать картинку');
    return blob;
  }

  async exportPng(terminal: string): Promise<void> {
    const el = this.cardEl(terminal);
    if (!el) return;
    try {
      const blob = await this.png(el);
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `План_${terminal}_${this.date()}.png`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    }
  }

  async sendImage(terminal: string): Promise<void> {
    const el = this.cardEl(terminal);
    if (!el) return;
    this.busy.set(`${terminal}:img`);
    try {
      const blob = await this.png(el);
      const res = await this.api.sendImage('spravki', terminal, blob, `План_${terminal}.png`,
        `План подвода ${terminal} ${this.fmtDate(this.date())}`);
      this.report(res, terminal);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.busy.set('');
    }
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
    } finally {
      this.sending.set(false);
    }
  }

  async sendText(f: PlanFormTerminal): Promise<void> {
    const text = this.buildText(f);
    if (!text) { this.msg.warning(`${f.terminal}: нет поездов для оперативки`); return; }
    this.busy.set(`${f.terminal}:txt`);
    try {
      const res = await this.api.sendText('oper', f.terminal, text);
      this.report(res, f.terminal);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.busy.set('');
    }
  }

  /** Текст оперативки: заголовок терминала + поезда по ЖД-датам (без сводки). */
  private buildText(f: PlanFormTerminal): string {
    const days = f.days.filter((d) => d.trains.length);
    if (!days.length) return '';
    const parts = [`${f.terminal} ЖД сутки`];
    for (const d of days) {
      parts.push(this.dayLabel(d.date));
      for (const tr of d.trains) parts.push(tr);
    }
    return parts.join('\n');
  }

  print(): void { window.print(); }

  private report(res: BroadcastResult, what: string): void {
    if (res.chats === 0) { this.msg.warning(`${what}: нет настроенного маршрута рассылки`); return; }
    const failed = Object.keys(res.failed);
    if (failed.length) this.msg.warning(`${what}: отправлено в ${res.sent.length}, не ушло — ${failed.join(', ')}`);
    else this.msg.success(`${what}: отправлено в чаты (${res.sent.join(', ')})`);
  }
}
