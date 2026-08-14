import { Component, OnInit, inject, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { DragDropModule } from '@angular/cdk/drag-drop';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../../core/api/api-error';
import { GtForecastApiService, GtStation } from './gt-forecast-api.service';

/** Строка редактора: линия выгрузки со скоростями. */
interface SpeedRow {
  terminal: string;
  color: string;
  cargoKey: string;
  label: string;
  plan: number;
  norm: number;
  origPlan: number;
  origNorm: number;
}

/**
 * Диалог «Настройки прогноза ГТ» (перенос вкладки «План» gtport
 * GtSettingsDialog): плановая и нормативная скорость выгрузки на линию
 * (терминал × груз). Сохранение пишет в справочник port_cargo_line
 * (plan_speed и pc) — то же место правится и в Админе.
 */
@Component({
  selector: 'app-gt-speeds',
  imports: [FormsModule, DragDropModule, NzButtonModule, NzModalModule],
  template: `
    <nz-modal [nzVisible]="true" [nzTitle]="ttl" [nzFooter]="null" nzWidth="520px"
              (nzOnCancel)="closed.emit()">
      <ng-template #ttl>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          Настройки прогноза — скорости выгрузки
        </div>
      </ng-template>
      <ng-container *nzModalContent>
        <table class="dp-tbl">
          <thead>
            <tr>
              <th>Терминал</th>
              <th>Груз</th>
              <th title="Реальная производственная скорость, ваг/сут">План</th>
              <th title="Нормативная скорость (способность линии) — полезное образование, ваг/сут">Норма</th>
            </tr>
          </thead>
          <tbody>
            @for (r of rows(); track r.terminal + r.cargoKey) {
              <tr [class.changed]="r.plan !== r.origPlan || r.norm !== r.origNorm">
                <td><span class="chip" [style.background]="r.color || '#f5f5f5'">{{ r.terminal }}</span></td>
                <td>{{ r.label || 'Общий' }}</td>
                <td><input type="number" min="1" [(ngModel)]="r.plan" /></td>
                <td><input type="number" min="1" [(ngModel)]="r.norm" /></td>
              </tr>
            }
          </tbody>
        </table>
        <div class="note">Скорости хранятся в справочнике линий выгрузки (port_cargo_line) —
          то же место правится в Админе. Влияют на симуляцию у всех.</div>
        <div class="btns">
          <button nz-button (click)="closed.emit()">Отмена</button>
          <button nz-button nzType="primary" (click)="save()" [disabled]="busy() || !hasChanges()">
            Сохранить
          </button>
        </div>
      </ng-container>
    </nz-modal>
  `,
  styles: [`
    .ttl { cursor: move; }
    .dp-tbl td, .dp-tbl th { font-size: 12px; padding: 3px 6px; text-align: center; }
    .dp-tbl input { width: 64px; border: 1px solid var(--color-border, #d9d9d9);
                    border-radius: 4px; padding: 1px 4px; text-align: center; }
    .chip { border-radius: 3px; padding: 0 8px; font-weight: 600; }
    tr.changed td { background: rgba(21,101,192,0.06); }
    .note { font-size: 11px; color: #888; margin: var(--space-sm) 0; }
    .btns { display: flex; justify-content: flex-end; gap: var(--space-sm); }
  `],
})
export class GtSpeedsComponent implements OnInit {
  private readonly api = inject(GtForecastApiService);
  private readonly msg = inject(NzMessageService);

  /** Станция режима: её терминалы и линии со скоростями (из context). */
  readonly station = input.required<GtStation>();

  /** Скорости сохранены — родителю нужно перечитать context и пересчитать. */
  readonly saved = output<void>();
  readonly closed = output<void>();

  readonly rows = signal<SpeedRow[]>([]);
  readonly busy = signal(false);

  ngOnInit(): void {
    const out: SpeedRow[] = [];
    for (const term of this.station().terminals) {
      for (const ln of term.lines) {
        out.push({
          terminal: term.name, color: term.color, cargoKey: ln.cargo_key, label: ln.label,
          plan: ln.plan_speed, norm: ln.norm_speed,
          origPlan: ln.plan_speed, origNorm: ln.norm_speed,
        });
      }
    }
    this.rows.set(out);
  }

  hasChanges(): boolean {
    return this.rows().some((r) => r.plan !== r.origPlan || r.norm !== r.origNorm);
  }

  async save(): Promise<void> {
    const changed = this.rows().filter((r) => r.plan !== r.origPlan || r.norm !== r.origNorm);
    if (changed.some((r) => !r.plan || r.plan <= 0 || !r.norm || r.norm <= 0)) {
      this.msg.error('Скорости должны быть больше нуля');
      return;
    }
    this.busy.set(true);
    try {
      await this.api.updateSpeeds(changed.map((r) => ({
        terminal: r.terminal, cargo_key: r.cargoKey, plan_speed: r.plan, norm_speed: r.norm,
      })));
      this.msg.success('Скорости сохранены');
      this.saved.emit();
    } catch (e) {
      this.msg.error(apiErrorMessage(e));
    } finally {
      this.busy.set(false);
    }
  }
}
