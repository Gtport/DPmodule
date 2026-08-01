import { Component, input, signal } from '@angular/core';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzDescriptionsModule } from 'ng-zorro-antd/descriptions';
import { LKProcessResult } from '../dislocation/dislocation-api.service';

/**
 * Сводка пересборки дислокации: короткая строка + «подробнее». Один вид на обе
 * модалки — ручную загрузку («ЛК») и автозабор («АВТО ЛК»); на главном экране
 * остаётся только тост, подробности смотрит тот, кому они нужны (решение
 * владельца).
 */
@Component({
  selector: 'app-lk-process-result',
  imports: [NzButtonModule, NzDescriptionsModule],
  template: `
    @if (res(); as r) {
      <div class="rsum">
        <b>Дислокация обновлена:</b>
        <span>вагонов <b>{{ r.count }}</b> (было {{ r.prev_snapshot }})</span>
        <span>· прогноз {{ r.prog_computed }}</span>
        <span>· расч. ход {{ r.forecast_computed }}</span>
        <span>· пропали {{ r.status8_missing }}</span>
        <span>· история +{{ r.history_inserted }}/~{{ r.history_updated }}</span>
        <button nz-button nzType="link" nzSize="small" (click)="showDetails.set(!showDetails())">
          {{ showDetails() ? 'скрыть' : 'подробнее' }}
        </button>
      </div>

      @if (showDetails()) {
        <nz-descriptions class="details" [nzColumn]="2" nzBordered nzSize="small">
          <nz-descriptions-item nzTitle="Файлов">{{ r.files }}</nz-descriptions-item>
          <nz-descriptions-item nzTitle="Назначение обогащено">{{ r.nazn_enriched }}</nz-descriptions-item>
          <nz-descriptions-item nzTitle="Порт не резолвится">{{ r.port_unresolved }}</nz-descriptions-item>
          <nz-descriptions-item nzTitle="Статус 9 (новых)">{{ r.status9_inserted }}</nz-descriptions-item>
          <nz-descriptions-item nzTitle="Статус 9 (снято)">{{ r.status9_removed }}</nz-descriptions-item>
          <nz-descriptions-item nzTitle="Статус 8 (пропавших)">{{ r.status8_missing }}</nz-descriptions-item>
          <nz-descriptions-item nzTitle="Carry-over (совпало)">{{ r.carry_matched }}</nz-descriptions-item>
          <nz-descriptions-item nzTitle="Carry-over (новых)">{{ r.carry_new }}</nz-descriptions-item>
          <nz-descriptions-item nzTitle="Статус удержан 4/5">{{ r.carry_sticky }}</nz-descriptions-item>
          <nz-descriptions-item nzTitle="Доноры (статус 6)">{{ r.status6_donors }}</nz-descriptions-item>
          <nz-descriptions-item nzTitle="Донорство добрано">{{ r.status6_matched }}</nz-descriptions-item>
          <nz-descriptions-item nzTitle="Marka заполнено">{{ r.marka_filled }}</nz-descriptions-item>
          <nz-descriptions-item nzTitle="Marka не нашла">{{ r.marka_missed }}</nz-descriptions-item>
          <nz-descriptions-item nzTitle="Назначение переставлено">{{ r.naznach_override }}</nz-descriptions-item>
        </nz-descriptions>
        @if (r.stations_not_found.length) {
          <p class="warn-line">Станции вне справочника: {{ r.stations_not_found.join(', ') }}</p>
        }
        @if (r.ops_not_found.length) {
          <p class="warn-line">Операции вне справочника: {{ r.ops_not_found.join(', ') }}</p>
        }
      }
    }
  `,
  styles: [`
    .rsum { display: flex; align-items: center; gap: var(--space-sm); flex-wrap: wrap;
            font-size: var(--font-size-sm); margin-top: var(--space-md); }
    .details { margin-top: var(--space-md); }
    .warn-line { margin: var(--space-sm) 0 0; color: var(--color-warning-text); font-size: var(--font-size-sm); }
  `],
})
export class LkProcessResultComponent {
  readonly res = input<LKProcessResult | null>(null);
  readonly showDetails = signal(false);
}
