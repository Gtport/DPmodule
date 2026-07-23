import { Component, OnInit, inject, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { DragDropModule } from '@angular/cdk/drag-drop';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import {
  BroadcastResult, PlanFormTerminal, ReferenceApiService, groupTrains,
} from './reference-api.service';
import { todayMsk } from '../../shared/msk-date';

/** Раскладка суток: ЖД (18:00→18:00) либо грузовые/МСК (00:00→24:00). */
type Mode = 'jd' | 'msk';

/**
 * «Оперативная СМС с ПП» — оперативный план поездов текстом (перенос gtport
 * SmsOper, вызывается из «Справок»). В отличие от «Утренней СМС» здесь нет блока
 * грузовой работы: только перечень поездов по суткам, с переключателем ЖД/ГР.
 *
 * Куда шлём (как в gtport): ЖД-сутки → чаты справок терминала (форма `spravki`),
 * ГР-сутки → оперативные чаты (форма `oper`). Адресатов разрешает бэк по маршруту.
 */
@Component({
  selector: 'app-sms-oper-modal',
  imports: [FormsModule, DragDropModule, NzButtonModule, NzIconModule, NzModalModule,
    NzSpinModule, NzTooltipModule],
  template: `
    <nz-modal [nzVisible]="true" [nzTitle]="ttl" [nzFooter]="null" nzWidth="1120px"
              [nzMask]="false" (nzOnCancel)="closed.emit()">
      <ng-template #ttl>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          Оперативная СМС с ПП
        </div>
      </ng-template>

      <ng-container *nzModalContent>
        <div class="bar">
          <input type="date" [ngModel]="date()" (ngModelChange)="date.set($event); reload()" class="date" />
          <div class="seg" nz-tooltip nzTooltipTitle="ЖД сутки 18:00→18:00 · ГР сутки 00:00→24:00">
            <button nz-button nzSize="small" [nzType]="mode() === 'jd' ? 'primary' : 'default'"
                    (click)="mode.set('jd')">ЖД сутки</button>
            <button nz-button nzSize="small" [nzType]="mode() === 'msk' ? 'primary' : 'default'"
                    (click)="mode.set('msk')">ГР сутки</button>
          </div>
          <span class="spacer"></span>
          <button nz-button nzSize="small" (click)="reload()" nz-tooltip nzTooltipTitle="Обновить">
            <span nz-icon nzType="sync"></span>
          </button>
        </div>

        @if (loading()) {
          <div class="center"><nz-spin nzSimple></nz-spin></div>
        } @else {
          <div class="grid">
            @for (f of forms(); track f.terminal) {
              <div class="term">
                <div class="term-head" [style.background]="f.color || '#eee'">{{ title(f) }}</div>

                <div class="body">
                  @for (d of days(f); track d.date) {
                    <div class="blk">{{ fmtDate(d.date) }}</div>
                    @for (tr of d.trains; track $index) {
                      <div class="tr"><b>{{ bold(tr) }}</b>{{ rest(tr) }}</div>
                    }
                  } @empty {
                    <div class="empty">Нет поездов</div>
                  }
                </div>

                <div class="term-actions">
                  <button nz-button nzSize="small" (click)="copy(f)"
                          nz-tooltip nzTooltipTitle="Скопировать текст в буфер">
                    <span nz-icon nzType="copy"></span>
                  </button>
                  <button nz-button nzSize="small" [nzLoading]="busy() === f.terminal" (click)="send(f)">
                    <span nz-icon nzType="message"></span> В MAX
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
    .seg { display: flex; gap: 2px; }
    .spacer { flex: 1 1 auto; }
    .center { display: flex; justify-content: center; padding: var(--space-xl); }
    .grid { display: flex; flex-wrap: wrap; gap: var(--space-md); align-items: flex-start; }

    .term { width: 340px; background: #fff; border: 1px solid var(--color-border);
            border-radius: var(--radius-card); overflow: hidden; display: flex; flex-direction: column; }
    .term-head { padding: var(--space-sm); text-align: center; font-weight: 700; font-size: var(--font-size-sm); }
    .body { max-height: 52vh; overflow: auto; flex: 1 1 auto; }
    .blk { padding: 4px 8px; background: rgba(0,0,0,.04); font-weight: 700; font-size: var(--font-size-sm); }
    .tr { padding: 3px 8px; font-size: var(--font-size-sm); border-bottom: 1px solid var(--color-border-light);
          white-space: normal; word-break: break-word; }
    .tr b { font-weight: 700; }
    .empty { padding: var(--space-sm); text-align: center; color: var(--color-text-secondary);
             font-size: var(--font-size-sm); }
    .term-actions { display: flex; gap: var(--space-xs); padding: var(--space-sm);
                    border-top: 1px solid var(--color-border-light); }
  `],
})
export class SmsOperModalComponent implements OnInit {
  private readonly api = inject(ReferenceApiService);
  private readonly msg = inject(NzMessageService);

  readonly closed = output<void>();

  readonly date = signal(todayMsk());
  readonly mode = signal<Mode>('jd');
  readonly loading = signal(false);
  readonly busy = signal('');
  readonly forms = signal<PlanFormTerminal[]>([]);

  ngOnInit(): void { void this.reload(); }

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

  /** Раскладка поездов по выбранным суткам. */
  days(f: PlanFormTerminal) { return groupTrains(f.trains, this.mode()); }

  /** Заголовок как в gtport: ЖД сутки либо «Обновленный план подвода(МСК)». */
  title(f: PlanFormTerminal): string {
    return this.mode() === 'jd'
      ? `${f.terminal} ЖД СУТКИ`
      : `${f.terminal} Обновленный план подвода(МСК)`;
  }

  fmtDate(d: string): string {
    return d.length >= 10 ? `${d.slice(8, 10)}.${d.slice(5, 7)}.${d.slice(0, 4)}` : d;
  }

  private cut(tr: string): number { const i = tr.indexOf('('); return i === -1 ? tr.length : i; }
  bold(tr: string): string { return tr.slice(0, this.cut(tr)); }
  rest(tr: string): string { return tr.slice(this.cut(tr)); }

  /** Текст формы: заголовок + поезда по суткам (как gtport formatTextForCopy). */
  private text(f: PlanFormTerminal): string {
    const days = this.days(f);
    if (!days.length) return '';
    const parts = [this.title(f)];
    for (const d of days) {
      parts.push(this.fmtDate(d.date));
      for (const tr of d.trains) parts.push(tr);
    }
    return parts.join('\n');
  }

  async copy(f: PlanFormTerminal): Promise<void> {
    const t = this.text(f);
    if (!t) { this.msg.warning(`${f.terminal}: нет поездов`); return; }
    try {
      await navigator.clipboard.writeText(t);
      this.msg.success(`${f.terminal}: текст скопирован`);
    } catch {
      this.msg.error('Не удалось скопировать');
    }
  }

  /** ЖД-сутки → чаты справок (spravki), ГР-сутки → оперативные чаты (oper). */
  async send(f: PlanFormTerminal): Promise<void> {
    const t = this.text(f);
    if (!t) { this.msg.warning(`${f.terminal}: нет поездов`); return; }
    this.busy.set(f.terminal);
    try {
      const report = this.mode() === 'jd' ? 'spravki' : 'oper';
      this.report(await this.api.sendText(report, f.terminal, t), f.terminal);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally { this.busy.set(''); }
  }

  private report(res: BroadcastResult, what: string): void {
    if (res.chats === 0) { this.msg.warning(`${what}: нет настроенного маршрута рассылки`); return; }
    const failed = Object.keys(res.failed);
    if (failed.length) this.msg.warning(`${what}: отправлено в ${res.sent.length}, не ушло — ${failed.join(', ')}`);
    else this.msg.success(`${what}: отправлено в чаты (${res.sent.join(', ')})`);
  }
}
