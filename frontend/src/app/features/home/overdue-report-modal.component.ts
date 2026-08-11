import { Component, OnInit, inject, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { DragDropModule } from '@angular/cdk/drag-drop';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzSelectModule } from 'ng-zorro-antd/select';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzMessageService } from 'ng-zorro-antd/message';
import { blobErrorMessage } from '../../shared/blob-error';
import { addDaysIso, todayMsk } from '../../shared/msk-date';
import { ArrivalsApiService } from './arrivals-api.service';
import { OverdueApiService } from './overdue-api.service';

/**
 * Перемещаемая модалка «Отчёт для претензии» — Excel по фактам просрочки
 * доставки из истории рейсов (vagon_history.delay > 0, фиксируется при
 * прибытии) за период прибытия. Книгу собирает сервер: лист «Накладные»
 * (единица претензии, агрегаты + пустые графы провозной платы и пеней под
 * ручное заполнение) и лист «Вагоны» (повагонная фактура). Дефолт периода —
 * последние 45 дней: срок предъявления претензии по пеням (ст. 123 УЖТ).
 */
@Component({
  selector: 'app-overdue-report-modal',
  imports: [
    FormsModule, DragDropModule, NzButtonModule, NzIconModule, NzModalModule,
    NzSelectModule, NzTooltipModule,
  ],
  template: `
    <nz-modal [nzVisible]="true" [nzTitle]="ttl" [nzFooter]="null" nzWidth="560px"
              [nzMask]="false" (nzOnCancel)="closed.emit()">
      <ng-template #ttl>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          Отчёт для претензии — просрочка доставки
        </div>
      </ng-template>

      <ng-container *nzModalContent>
        <div class="filters">
          <label class="fl">Прибытие с <input type="date" class="date" [ngModel]="start()" (ngModelChange)="start.set($event)" /></label>
          <label class="fl">по <input type="date" class="date" [ngModel]="end()" (ngModelChange)="end.set($event)" /></label>
          <button nz-button nzSize="small" (click)="lastDays(45)">45 дней</button>
          <button nz-button nzSize="small" (click)="lastDays(30)">30 дней</button>
        </div>
        <div class="filters">
          <nz-select class="term" nzSize="small" [ngModel]="terminal()" (ngModelChange)="terminal.set($event)">
            <nz-option nzValue="" nzLabel="Все терминалы"></nz-option>
            @for (t of terminals(); track t) { <nz-option [nzValue]="t" [nzLabel]="t"></nz-option> }
          </nz-select>
          <span class="spacer"></span>
          <button nz-button nzType="primary" nzSize="small" [nzLoading]="downloading()" (click)="download()">
            <span nz-icon nzType="file-excel"></span> Скачать Excel
          </button>
        </div>
        <p class="hint">
          Лист «Накладные» — единица претензии (агрегаты по вагонам накладной), лист «Вагоны» — повагонная
          фактура. Претензия по пеням подаётся в течение 45 дней с даты выдачи груза (ст. 123 УЖТ);
          пени — 6% провозной платы за каждые сутки просрочки, не более 50% (ст. 97). Провозная плата
          и пени заполняются вручную по накладной — в книге под них оставлены пустые графы.
        </p>
      </ng-container>
    </nz-modal>
  `,
  styles: [`
    .ttl { cursor: move; user-select: none; }
    .filters { display: flex; align-items: center; gap: var(--space-sm); margin-bottom: var(--space-sm); flex-wrap: wrap; }
    .fl { font-size: var(--font-size-sm); color: var(--color-text-secondary); display: inline-flex; align-items: center; gap: 4px; }
    .date { padding: 3px 6px; border: 1px solid var(--color-border); border-radius: var(--radius-sm); }
    .term { width: 170px; }
    .spacer { flex: 1 1 auto; }
    .hint { margin: var(--space-xs) 0 0; color: var(--color-text-muted); font-size: var(--font-size-sm); }
  `],
})
export class OverdueReportModalComponent implements OnInit {
  private readonly api = inject(OverdueApiService);
  private readonly arrivals = inject(ArrivalsApiService);
  private readonly msg = inject(NzMessageService);

  readonly closed = output<void>();

  // Дефолт — последние 45 дней (срок претензии по пеням, ст. 123 УЖТ).
  readonly start = signal(addDaysIso(todayMsk(), -44));
  readonly end = signal(todayMsk());
  readonly terminal = signal('');
  readonly terminals = signal<string[]>([]);
  readonly downloading = signal(false);

  ngOnInit(): void {
    void this.loadTerminals();
  }

  private async loadTerminals(): Promise<void> {
    try {
      this.terminals.set((await this.arrivals.getTerminals()).map((t) => t.name));
    } catch {
      /* справочник не критичен */
    }
  }

  lastDays(n: number): void {
    this.start.set(addDaysIso(todayMsk(), -(n - 1)));
    this.end.set(todayMsk());
  }

  async download(): Promise<void> {
    this.downloading.set(true);
    try {
      const blob = await this.api.claimExcel(this.start(), this.end(), this.terminal());
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `просрочка_доставки_${this.fmtDate(this.start())}-${this.fmtDate(this.end())}.xlsx`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      this.msg.error(await blobErrorMessage(err));
    } finally {
      this.downloading.set(false);
    }
  }

  /** «2026-08-11» → «11.08.2026». */
  fmtDate(iso: string): string {
    if (!iso || iso.length < 10) return iso;
    return `${iso.slice(8, 10)}.${iso.slice(5, 7)}.${iso.slice(0, 4)}`;
  }
}
