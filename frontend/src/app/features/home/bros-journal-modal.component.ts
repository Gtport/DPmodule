import { Component, OnInit, computed, inject, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { DragDropModule } from '@angular/cdk/drag-drop';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzRadioModule } from 'ng-zorro-antd/radio';
import { NzSelectModule } from 'ng-zorro-antd/select';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { BrosApiService, BrosRecord, BrosReasonCode, BrosJournalEntry } from './bros-api.service';

/**
 * Перемещаемая модалка «Журнал броска»: история записей поезда + форма новой
 * записи (фиксация состояния оператором). Особые коды повторяют бэк:
 *   · 05 (платное размещение) — реквизиты заявки: №/дата, дата подъёма, причина;
 *   · 01 (неприём) — согласованное/несогласованное; согласованное — реквизиты
 *     гарантийного письма.
 * Успешное сохранение — событие `saved` (родитель перечитывает активные броски).
 */
@Component({
  selector: 'app-bros-journal-modal',
  imports: [
    FormsModule, DragDropModule, NzModalModule, NzButtonModule, NzInputModule,
    NzSelectModule, NzRadioModule, NzIconModule,
  ],
  template: `
    <nz-modal [nzVisible]="true" [nzTitle]="ttl" [nzFooter]="null" nzWidth="640px"
              [nzMask]="false" (nzOnCancel)="closed.emit()">
      <ng-template #ttl>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          Журнал: {{ record().index_1 || record().index_0 }} · {{ record().gruzpol_s }}
        </div>
      </ng-template>

      <ng-container *nzModalContent>
        <div class="sub">{{ record().station_br }} · {{ record().vagon_count }} ваг · {{ record().sostav }}</div>

        <!-- Форма новой записи -->
        <div class="form">
          <div class="fld">
            <label>Код бросания</label>
            <nz-select class="grow" nzShowSearch nzAllowClear nzPlaceHolder="выберите код"
                       [ngModel]="reason()" (ngModelChange)="reason.set($event)">
              @for (c of codes(); track c.code) {
                <nz-option [nzValue]="c.code" [nzLabel]="c.code + ' — ' + c.description"></nz-option>
              }
            </nz-select>
          </div>

          @if (selectedDesc(); as d) { <div class="desc">{{ d }}</div> }

          <div class="fld">
            <label>Комментарий</label>
            <input nz-input class="grow" [ngModel]="comment()" (ngModelChange)="comment.set($event)"
                   maxlength="200" placeholder="необязательно" />
          </div>

          <!-- Код 01: согласованность -->
          @if (isCode01()) {
            <div class="fld">
              <label>Тип</label>
              <nz-radio-group [ngModel]="isAgreed()" (ngModelChange)="isAgreed.set($event)">
                <label nz-radio [nzValue]="true">Согласованное (письмо)</label>
                <label nz-radio [nzValue]="false">Несогласованное</label>
              </nz-radio-group>
            </div>
          }

          <!-- Реквизиты заявки/письма: код 05 или согласованный 01 -->
          @if (needsPapers()) {
            <div class="row2">
              <div class="fld">
                <label>{{ isCode05() ? '№ заявки' : '№ письма' }}</label>
                <input nz-input [ngModel]="zayavkaNomer()" (ngModelChange)="zayavkaNomer.set($event)" />
              </div>
              <div class="fld">
                <label>Дата {{ isCode05() ? 'заявки' : 'письма' }}</label>
                <input nz-input type="date" [ngModel]="zayavkaDate()" (ngModelChange)="zayavkaDate.set($event)" />
              </div>
            </div>
            <div class="fld">
              <label>Дата подъёма</label>
              <input nz-input type="date" [ngModel]="datePod()" (ngModelChange)="datePod.set($event)" />
            </div>
          }

          <!-- Код 05: причина (текст) -->
          @if (isCode05()) {
            <div class="fld">
              <label>Причина</label>
              <input nz-input class="grow" [ngModel]="reasonText()" (ngModelChange)="reasonText.set($event)"
                     placeholder="текст причины (для кода 05)" />
            </div>
          }

          <div class="actions">
            <button nz-button nzType="primary" nzSize="small" [disabled]="!valid() || saving()"
                    (click)="save()">
              @if (saving()) { <span nz-icon nzType="loading"></span> } Записать
            </button>
            <button nz-button nzType="text" nzSize="small" (click)="closed.emit()">Закрыть</button>
          </div>
        </div>

        <!-- История записей -->
        <div class="hist-h">История записей ({{ history().length }})</div>
        <div class="hist">
          @for (e of history(); track e.id) {
            <div class="he">
              <span class="d">{{ fmtDate(e.date) }}</span>
              <span class="code">{{ e.reason || '—' }}</span>
              <span class="rt" [title]="e.reason_text || ''">{{ e.reason_text || e.comment || '' }}</span>
              @if (e.is_agreed !== null) {
                <span class="ag" [class.no]="!e.is_agreed">{{ e.is_agreed ? 'согл.' : 'несогл.' }}</span>
              }
              <span class="by">{{ e.created_by }}</span>
            </div>
          } @empty {
            <div class="empty">Записей пока нет</div>
          }
        </div>
      </ng-container>
    </nz-modal>
  `,
  styles: [`
    .ttl { cursor: move; user-select: none; }
    .sub { color: var(--color-text-muted); font-size: var(--font-size-sm); margin-bottom: var(--space-sm);
           overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .form { display: flex; flex-direction: column; gap: var(--space-sm); }
    .fld { display: flex; align-items: center; gap: var(--space-sm); }
    .fld > label { width: 110px; flex: 0 0 auto; font-size: var(--font-size-sm); color: var(--color-text-secondary); }
    .grow { flex: 1 1 auto; }
    .row2 { display: flex; gap: var(--space-sm); }
    .row2 .fld { flex: 1 1 0; }
    .row2 .fld > label { width: auto; }
    .desc { margin: -4px 0 2px 118px; font-size: var(--font-size-sm); color: var(--color-text-muted); }
    .actions { display: flex; gap: var(--space-sm); margin-top: var(--space-xs); }
    .hist-h { margin: var(--space-md) 0 var(--space-xs); font-weight: 600; font-size: var(--font-size-sm); }
    .hist { max-height: 30vh; overflow: auto; border: 1px solid var(--color-border-light); border-radius: var(--radius-sm); }
    .he { display: flex; align-items: center; gap: var(--space-sm); padding: 3px 8px;
          border-bottom: 1px solid var(--color-border-light); font-size: var(--font-size-sm); }
    .he:last-child { border-bottom: none; }
    .he .d { width: 74px; flex: 0 0 auto; font-variant-numeric: tabular-nums; color: var(--color-text-muted); }
    .he .code { width: 34px; flex: 0 0 auto; font-weight: 600; }
    .he .rt { flex: 1 1 auto; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .he .ag { color: var(--color-success, #389e0d); } .he .ag.no { color: var(--color-danger, #cf1322); }
    .he .by { flex: 0 0 auto; color: var(--color-text-muted); }
    .empty { padding: var(--space-sm); text-align: center; color: var(--color-text-secondary); font-size: var(--font-size-sm); }
  `],
})
export class BrosJournalModalComponent implements OnInit {
  private readonly api = inject(BrosApiService);
  private readonly msg = inject(NzMessageService);

  readonly record = input.required<BrosRecord>();
  readonly codes = input.required<BrosReasonCode[]>();
  readonly closed = output<void>();
  readonly saved = output<void>();

  readonly history = signal<BrosJournalEntry[]>([]);
  readonly saving = signal(false);

  readonly reason = signal('');
  readonly comment = signal('');
  readonly zayavkaNomer = signal('');
  readonly zayavkaDate = signal('');
  readonly datePod = signal('');
  readonly reasonText = signal('');
  readonly isAgreed = signal<boolean | null>(null);

  readonly isCode05 = computed(() => this.reason() === '05');
  readonly isCode01 = computed(() => this.reason() === '01');
  readonly needsPapers = computed(() => this.isCode05() || (this.isCode01() && this.isAgreed() === true));

  readonly selectedDesc = computed(() => {
    const c = this.codes().find((x) => x.code === this.reason());
    return c?.description ?? '';
  });

  readonly valid = computed(() => {
    if (!this.reason()) return false;
    if (this.isCode05()) {
      return !!(this.zayavkaNomer() && this.zayavkaDate() && this.datePod() && this.reasonText());
    }
    if (this.isCode01()) {
      if (this.isAgreed() === null) return false;
      if (this.isAgreed() === true) return !!(this.zayavkaNomer() && this.zayavkaDate() && this.datePod());
      return true;
    }
    return true;
  });

  ngOnInit(): void {
    const r = this.record();
    this.reason.set(r.reason || '');
    if (r.date_pod) this.datePod.set(r.date_pod.slice(0, 10));
    void this.loadHistory();
  }

  private async loadHistory(): Promise<void> {
    try {
      const entries = await this.api.getJournal(this.record().id);
      this.history.set(entries);
      // Предзаполнение из последней записи (актуальнее полей снимка).
      const last = entries[0];
      if (last) {
        this.reason.set(last.reason || this.reason());
        if (last.date_pod) this.datePod.set(last.date_pod.slice(0, 10));
        if (last.zayavka_nomer) this.zayavkaNomer.set(last.zayavka_nomer);
        if (last.zayavka_date) this.zayavkaDate.set(last.zayavka_date.slice(0, 10));
        if (last.reason === '01') this.isAgreed.set(last.is_agreed);
        if (last.reason_text && last.reason === '05') this.reasonText.set(last.reason_text);
      }
    } catch {
      /* история не критична */
    }
  }

  async save(): Promise<void> {
    if (!this.valid() || this.saving()) return;
    this.saving.set(true);
    try {
      await this.api.createJournal({
        bros_id: this.record().id,
        reason: this.reason(),
        comment: this.comment(),
        plan_pod: this.record().plan ?? undefined,
        zayavka_nomer: this.needsPapers() ? this.zayavkaNomer() : undefined,
        zayavka_date: this.needsPapers() ? this.zayavkaDate() : undefined,
        date_pod: this.needsPapers() ? this.datePod() : undefined,
        reason_text: this.isCode05() ? this.reasonText() : undefined,
        is_agreed: this.isCode01() ? this.isAgreed() : undefined,
      });
      this.msg.success('Запись добавлена в журнал');
      this.saved.emit();
      this.closed.emit();
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.saving.set(false);
    }
  }

  /** «2026-07-25T00:00:00» → «25.07.26»; пусто → «—». */
  fmtDate(ts: string | null): string {
    if (!ts || ts.length < 10) return '—';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)}.${ts.slice(2, 4)}`;
  }
}
