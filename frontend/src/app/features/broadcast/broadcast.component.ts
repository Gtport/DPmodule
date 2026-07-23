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
  BroadcastApiService, BroadcastResult, PlanFormLine, PlanFormTerminal, PlanFormTrain,
} from './broadcast-api.service';
import { addDaysIso, todayMsk } from '../../shared/msk-date';

/** Строка сводки: подпись + значение из линии. */
interface Row { label: string; get: (l: PlanFormLine) => string; }

/** Поезда одной ЖД-даты (для разделителей дат в списке). */
interface TrainDay { date: string; trains: PlanFormTrain[]; }

/**
 * Экран «Рассылка» (перенос gtport SmsPlan/SmsOper): по терминалу сводная
 * карточка «ЖД сутки» под картинку — блок «Вчера» (факт из учётного листа) и
 * «Сегодня» (прогноз движком над подходом), затем список поездов (приб + подход).
 * Данные — одна ручка GET /dislocation/plan-form (сбор на бэке, как оригинал).
 * Картинку рисует фронт (html-to-image); рассылку по маршруту (форма × терминал)
 * делает бэк. Карточки перемещаемы (cdkDrag) и печатаются.
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

              <!-- Вчера (факт) -->
              <div class="blk">{{ fmtDate(prevDate()) }} (вчера)</div>
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

              <!-- Сегодня (прогноз) -->
              <div class="blk">{{ fmtDate(date()) }}</div>
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

              <!-- Поезда: приб + подход, по ЖД-датам -->
              <div class="trains">
                @for (g of trainDays(f); track g.date) {
                  <div class="tday">{{ fmtDate(g.date) }}</div>
                  @for (t of g.trains; track $index) {
                    <div class="tr" [class.prib]="t.arrived">{{ trainLine(t) }}</div>
                  }
                } @empty {
                  <div class="empty">Нет поездов</div>
                }
              </div>

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

    .term { width: 340px; background: #fff; border: 1px solid var(--color-border);
            border-radius: var(--radius-card); overflow: hidden; box-shadow: var(--shadow-card); }
    .term-head { padding: var(--space-sm); text-align: center; font-weight: 700; cursor: move; }
    .blk { padding: 3px 8px; background: rgba(0,0,0,.04); font-weight: 600; font-size: var(--font-size-sm);
           border-top: 1px solid var(--color-border-light); }

    .sum { width: 100%; border-collapse: collapse; font-size: var(--font-size-sm); }
    .sum th, .sum td { border: 1px solid var(--color-border-light); padding: 2px 8px; text-align: center; }
    .sum th { background: var(--color-bg-subtle); font-weight: 600; }
    .sum .ind { text-align: left; white-space: nowrap; font-weight: 500; }

    .trains { max-height: 40vh; overflow: auto; border-top: 1px solid var(--color-border-light); }
    .tday { padding: 3px 8px; background: rgba(0,0,0,.04); font-weight: 600; font-size: var(--font-size-sm); }
    .tr { padding: 2px 8px; font-size: var(--font-size-sm); border-bottom: 1px solid var(--color-border-light);
          white-space: normal; word-break: break-word; }
    .tr.prib { color: var(--color-text-secondary); } /* прибывшие — приглушённо */
    .empty { padding: var(--space-sm); text-align: center; color: var(--color-text-secondary);
             font-size: var(--font-size-sm); }

    .term-actions { display: flex; gap: var(--space-xs); padding: var(--space-sm);
                    border-top: 1px solid var(--color-border-light); }

    @media print {
      .no-print { display: none !important; }
      .term { box-shadow: none; page-break-inside: avoid; }
      .trains { max-height: none; overflow: visible; }
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

  // «Вчера» (факт) — 6 строк как в gtport.
  readonly YEST: Row[] = [
    { label: 'Остаток на 18:', get: (l) => String(l.ost_18) },
    { label: 'Прибыло:', get: (l) => String(l.prib) },
    { label: 'Полезное образование:', get: (l) => String(l.useful_y) },
    { label: 'Образование:', get: (l) => String(l.total_y) },
    { label: 'Выгрузка:', get: (l) => String(l.vigr_fact) },
    { label: 'Остаток:', get: (l) => String(l.ost_y) },
  ];
  // «Сегодня» (прогноз) — 3 строки как в gtport.
  readonly TODAY: Row[] = [
    { label: 'Остаток:', get: (l) => String(l.ost_today) },
    { label: 'Полезное образование:', get: (l) => `${l.useful_today}/${l.total_today}` },
    { label: 'Простой порта(прогноз):', get: (l) => l.downtime_today || '0:00' },
  ];

  ngOnInit(): void {
    void this.reload();
  }

  prevDate(): string {
    return addDaysIso(this.date(), -1);
  }

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

  /** дд.мм чч:мм из LocalTime. */
  fmtDT(ts: string | null): string {
    if (!ts || ts.length < 16) return '—';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)} ${ts.slice(11, 16)}`;
  }

  /** чч:мм. */
  private hhmm(ts: string | null): string {
    return ts && ts.length >= 16 ? ts.slice(11, 16) : '—';
  }

  /** Поезда по ЖД-датам (разделители); внутри — по времени. */
  trainDays(f: PlanFormTerminal): TrainDay[] {
    const by: Record<string, PlanFormTrain[]> = {};
    for (const t of f.trains) {
      const day = t.time ? t.time.slice(0, 10) : '—';
      (by[day] ??= []).push(t);
    }
    return Object.keys(by).sort().map((date) => ({ date, trains: by[date] }));
  }

  /** Строка поезда: «783 - приб 14:15 (40) уголь». */
  trainLine(t: PlanFormTrain): string {
    const prib = t.arrived ? 'приб ' : '';
    const cargo = t.cargo ? ` ${t.cargo.toLowerCase()}` : '';
    return `${t.index || '—'} - ${prib}${this.hhmm(t.time)} (${t.count})${cargo}`;
  }

  // ── Экспорт / рассылка ──────────────────────────────────────────────────

  private cardEl(terminal: string): HTMLElement | null {
    return this.host.nativeElement.querySelector(`#term-${CSS.escape(terminal)}`);
  }

  private async png(el: HTMLElement): Promise<Blob> {
    const scrollers = Array.from(el.querySelectorAll<HTMLElement>('.trains'));
    const saved = scrollers.map((s) => ({ s, mh: s.style.maxHeight, ov: s.style.overflow }));
    for (const { s } of saved) { s.style.maxHeight = 'none'; s.style.overflow = 'visible'; }
    try {
      const blob = await toBlob(el, { pixelRatio: 2, backgroundColor: '#ffffff' });
      if (!blob) throw new Error('не удалось отрисовать картинку');
      return blob;
    } finally {
      for (const { s, mh, ov } of saved) { s.style.maxHeight = mh; s.style.overflow = ov; }
    }
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

  /** Текст оперативки: заголовок + строки поездов по датам. */
  private buildText(f: PlanFormTerminal): string {
    const days = this.trainDays(f);
    if (!days.length) return '';
    const parts = [`${f.terminal} ЖД сутки`];
    for (const g of days) {
      parts.push(this.fmtDate(g.date));
      for (const t of g.trains) parts.push(this.trainLine(t));
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
