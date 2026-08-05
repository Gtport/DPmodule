import { Component, OnInit, inject, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { DragDropModule } from '@angular/cdk/drag-drop';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzPopconfirmModule } from 'ng-zorro-antd/popconfirm';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../../core/api/api-error';
import {
  GtForecastApiService, GtSimulateRequest, GtSnapshotMeta,
} from './gt-forecast-api.service';

/**
 * Диалог сохранённых планов (перенос gtport SavedPlansDialog): сохранить
 * текущий план на дату (сервер пересчитывает по входу сеанса и хранит свой
 * результат), список за период, открыть архив read-only, удалить,
 * CSV-аналитика «прогноз vs факт» за период (ZIP).
 */
@Component({
  selector: 'app-gt-snapshots',
  imports: [FormsModule, DragDropModule, NzButtonModule, NzModalModule, NzPopconfirmModule, NzSpinModule],
  template: `
    <nz-modal [nzVisible]="true" [nzTitle]="ttl" [nzFooter]="null" nzWidth="640px"
              (nzOnCancel)="closed.emit()">
      <ng-template #ttl>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          Сохранённые планы ГТ
        </div>
      </ng-template>
      <ng-container *nzModalContent>
        <div class="save-row">
          <label>Сохранить текущий план на:
            <input type="date" [(ngModel)]="saveDate" />
          </label>
          <button nz-button nzType="primary" nzSize="small" (click)="save()" [disabled]="busy()">
            Сохранить
          </button>
          <span class="note">пересчитывается сервером с правками сеанса ({{ overridesCount() }})</span>
        </div>

        <div class="filter-row">
          <label>С: <input type="date" [(ngModel)]="from" (ngModelChange)="load()" /></label>
          <label>По: <input type="date" [(ngModel)]="to" (ngModelChange)="load()" /></label>
          <span class="spacer"></span>
          <button nz-button nzSize="small" (click)="downloadAnalytics()" [disabled]="busy() || list().length === 0"
                  title="ZIP: trains / gantt_days / free_slots за период, прогноз vs факт">
            Аналитика CSV
          </button>
        </div>

        @if (loading()) {
          <div class="empty"><nz-spin nzSimple /></div>
        } @else {
          <table class="dp-tbl">
            <thead>
              <tr><th>План на</th><th>Расчёт</th><th>Сохранил</th><th>Обновлён</th><th></th></tr>
            </thead>
            <tbody>
              @for (s of list(); track s.plan_date + s.station) {
                <tr>
                  <td class="b">{{ dmy(s.plan_date) }}</td>
                  <td>с {{ dmy(s.start_date) }} · {{ s.days_count }} дн.</td>
                  <td>{{ s.saved_by || '—' }}</td>
                  <td>{{ dt(s.updated_at) }}</td>
                  <td class="acts">
                    <button nz-button nzSize="small" (click)="openSnap.emit(s)">Открыть</button>
                    <button nz-button nzSize="small" nzDanger
                            nz-popconfirm nzPopconfirmTitle="Удалить сохранённый план?"
                            (nzOnConfirm)="remove(s)">✕</button>
                  </td>
                </tr>
              } @empty {
                <tr><td colspan="5" class="empty">За период сохранённых планов нет</td></tr>
              }
            </tbody>
          </table>
        }
      </ng-container>
    </nz-modal>
  `,
  styles: [`
    .ttl { cursor: move; }
    .save-row, .filter-row { display: flex; align-items: center; gap: var(--space-sm);
      flex-wrap: wrap; margin-bottom: var(--space-sm); font-size: 12px; }
    .save-row input, .filter-row input { border: 1px solid var(--color-border, #d9d9d9);
      border-radius: 4px; padding: 1px 6px; font-size: 12px; margin-left: 4px; }
    .note { color: #888; font-size: 11px; }
    .spacer { flex: 1; }
    .dp-tbl td, .dp-tbl th { font-size: 12px; padding: 3px 6px; text-align: center; }
    .b { font-weight: 600; }
    .acts { display: flex; gap: 4px; justify-content: center; }
    .empty { color: #888; padding: var(--space-md); text-align: center; }
  `],
})
export class GtSnapshotsComponent implements OnInit {
  private readonly api = inject(GtForecastApiService);
  private readonly msg = inject(NzMessageService);

  /** Текущий вход сеанса — сохраняется сервером как есть. */
  readonly request = input.required<GtSimulateRequest>();
  readonly journal = input<unknown[]>([]);
  readonly overridesCount = input<number>(0);

  readonly openSnap = output<GtSnapshotMeta>();
  readonly closed = output<void>();

  readonly list = signal<GtSnapshotMeta[]>([]);
  readonly loading = signal(false);
  readonly busy = signal(false);

  saveDate = '';
  from = '';
  to = '';

  ngOnInit(): void {
    const req = this.request();
    this.saveDate = req.start_date;
    // Диапазон списка накрывает и прошлые планы, и сохраняемые вперёд.
    this.from = shiftDate(req.start_date, -14);
    this.to = shiftDate(req.start_date, 7);
    void this.load();
  }

  async load(): Promise<void> {
    if (!this.from || !this.to) return;
    this.loading.set(true);
    try {
      this.list.set(await this.api.listSnapshots(this.from, this.to, this.request().station));
    } catch (e) {
      this.msg.error(apiErrorMessage(e));
    } finally {
      this.loading.set(false);
    }
  }

  async save(): Promise<void> {
    if (!this.saveDate) return;
    this.busy.set(true);
    try {
      await this.api.saveSnapshot(this.saveDate, this.request(), this.journal());
      this.msg.success(`План на ${this.dmy(this.saveDate)} сохранён`);
      if (this.saveDate > this.to) this.to = this.saveDate;
      if (this.saveDate < this.from) this.from = this.saveDate;
      await this.load();
    } catch (e) {
      this.msg.error(apiErrorMessage(e));
    } finally {
      this.busy.set(false);
    }
  }

  async remove(s: GtSnapshotMeta): Promise<void> {
    try {
      await this.api.deleteSnapshot(s.plan_date, s.station);
      await this.load();
    } catch (e) {
      this.msg.error(apiErrorMessage(e));
    }
  }

  async downloadAnalytics(): Promise<void> {
    this.busy.set(true);
    try {
      const blob = await this.api.analytics(this.from, this.to, this.request().station);
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `gt_analytics_${this.from}_${this.to}.zip`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (e) {
      this.msg.error(apiErrorMessage(e));
    } finally {
      this.busy.set(false);
    }
  }

  dmy(iso: string): string {
    return `${iso.slice(8, 10)}.${iso.slice(5, 7)}.${iso.slice(0, 4)}`;
  }

  dt(iso: string | null): string {
    if (!iso) return '—';
    return `${this.dmy(iso)} ${iso.slice(11, 16)}`;
  }
}

/** Дата YYYY-MM-DD ± дней (без часовых поясов: полдень как якорь). */
function shiftDate(iso: string, days: number): string {
  const d = new Date(`${iso}T12:00:00`);
  d.setDate(d.getDate() + days);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}
