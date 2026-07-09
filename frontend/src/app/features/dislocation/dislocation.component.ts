import { Component, OnInit, inject, signal } from '@angular/core';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzCardModule } from 'ng-zorro-antd/card';
import { NzAlertModule } from 'ng-zorro-antd/alert';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { NzTableModule } from 'ng-zorro-antd/table';
import { NzUploadModule, NzUploadFile } from 'ng-zorro-antd/upload';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzDescriptionsModule } from 'ng-zorro-antd/descriptions';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { apiErrorMessage } from '../../core/api/api-error';
import {
  DislocationApiService,
  LKProcessResult,
  LKStatus,
} from './dislocation-api.service';

/**
 * Раздел «Дислокация»: двухшаговый приём ЛК — загрузка xlsx-выгрузок (шаг 1,
 * по файлу на грузополучателя) → контроль приёма → «Обработать в снимок»
 * (шаг 2, перестраивает актуальную дислокацию). Смысл разделения на шаги —
 * дать диспетчеру увидеть все ожидаемые файлы и замечания контроля ДО того,
 * как снимок будет перестроен.
 */
@Component({
  selector: 'app-dislocation',
  imports: [
    NzButtonModule, NzCardModule, NzAlertModule, NzTagModule, NzTableModule,
    NzUploadModule, NzIconModule, NzDescriptionsModule, NzSpinModule,
  ],
  template: `
    <div class="page">
      <nz-card nzTitle="Загрузка ЛК" class="card">
        <p class="hint">
          Загрузите xlsx-выгрузку дислокации из ЛК — по одному файлу на
          грузополучателя. Файлы копятся здесь, пока не нажата «Обработать».
        </p>
        <nz-upload nzAccept=".xlsx" [nzShowUploadList]="false" [nzBeforeUpload]="beforeUpload">
          <button nz-button [nzLoading]="busyUpload()">
            <span nz-icon nzType="upload"></span>
            Выбрать файл ЛК
          </button>
        </nz-upload>
        @if (uploadMsg()) {
          <nz-alert class="msg" nzType="success" [nzMessage]="uploadMsg()!" nzShowIcon nzCloseable />
        }
      </nz-card>

      <nz-card nzTitle="Принятые файлы" class="card">
        <nz-spin [nzSpinning]="loadingStatus()">
          @if (status(); as st) {
            <div class="ready-row">
              @if (st.ready) {
                <nz-tag nzColor="success">Готово к обработке</nz-tag>
              } @else {
                <nz-tag nzColor="error">Есть блокирующие замечания</nz-tag>
              }
              <span class="muted">группа совместного среза: {{ st.co_arrival_group || '—' }}</span>
            </div>

            @for (issue of st.issues; track $index) {
              <nz-alert
                class="issue"
                [nzType]="issue.level === 'block' ? 'error' : 'warning'"
                [nzMessage]="issue.message"
                nzShowIcon
              />
            }

            <nz-table [nzData]="st.files" [nzShowPagination]="false" [nzScroll]="{ x: '760px' }">
              <thead>
                <tr>
                  <th>Организация</th>
                  <th>ОКПО</th>
                  <th>Терминалы</th>
                  <th>Метка формирования</th>
                  <th>Возраст, мин</th>
                  <th>Файл</th>
                </tr>
              </thead>
              <tbody>
                @for (f of st.files; track f.filename) {
                  <tr>
                    <td>{{ f.organisation || '—' }}</td>
                    <td>{{ f.okpo }}</td>
                    <td>{{ f.terminals.join(', ') || '—' }}</td>
                    <td>{{ f.formation_ts }}</td>
                    <td>{{ f.age_minutes }}</td>
                    <td>{{ f.filename }}</td>
                  </tr>
                } @empty {
                  <tr><td colspan="6" class="muted">Файлы ещё не загружены</td></tr>
                }
              </tbody>
            </nz-table>

            <button nz-button nzType="primary" class="process-btn"
                    [disabled]="!st.ready" [nzLoading]="busyProcess()"
                    (click)="process()">
              Обработать в снимок
            </button>
          }
        </nz-spin>

        @if (error()) {
          <nz-alert class="msg" nzType="error" [nzMessage]="error()!" nzShowIcon nzCloseable />
        }
      </nz-card>

      @if (processResult(); as res) {
        <nz-card nzTitle="Результат обработки" class="card">
          <nz-descriptions [nzColumn]="3" nzBordered nzSize="small">
            <nz-descriptions-item nzTitle="Записей в снимке">{{ res.count }}</nz-descriptions-item>
            <nz-descriptions-item nzTitle="Файлов обработано">{{ res.files }}</nz-descriptions-item>
            <nz-descriptions-item nzTitle="Прежний снимок">{{ res.prev_snapshot }}</nz-descriptions-item>
            <nz-descriptions-item nzTitle="Назначение обогащено">{{ res.nazn_enriched }}</nz-descriptions-item>
            <nz-descriptions-item nzTitle="Порт не резолвится">{{ res.port_unresolved }}</nz-descriptions-item>
            <nz-descriptions-item nzTitle="Порт выключен">{{ res.port_disabled }}</nz-descriptions-item>
            <nz-descriptions-item nzTitle="Статус 9 (новых)">{{ res.status9_inserted }}</nz-descriptions-item>
            <nz-descriptions-item nzTitle="Статус 9 (снято)">{{ res.status9_removed }}</nz-descriptions-item>
            <nz-descriptions-item nzTitle="Статус 8 (пропавших)">{{ res.status8_missing }}</nz-descriptions-item>
            <nz-descriptions-item nzTitle="Carry-over (совпало)">{{ res.carry_matched }}</nz-descriptions-item>
            <nz-descriptions-item nzTitle="Carry-over (новых)">{{ res.carry_new }}</nz-descriptions-item>
            <nz-descriptions-item nzTitle="Статус удержан 4/5">{{ res.carry_sticky }}</nz-descriptions-item>
            <nz-descriptions-item nzTitle="Доноры (статус 6)">{{ res.status6_donors }}</nz-descriptions-item>
            <nz-descriptions-item nzTitle="Донорство добрано">{{ res.status6_matched }}</nz-descriptions-item>
            <nz-descriptions-item nzTitle="Marka заполнено">{{ res.marka_filled }}</nz-descriptions-item>
            <nz-descriptions-item nzTitle="Marka не нашла">{{ res.marka_missed }}</nz-descriptions-item>
            <nz-descriptions-item nzTitle="Назначение переставлено">{{ res.naznach_override }}</nz-descriptions-item>
            <nz-descriptions-item nzTitle="Расчётный ход">{{ res.forecast_computed }}</nz-descriptions-item>
            <nz-descriptions-item nzTitle="История: новых">{{ res.history_inserted }}</nz-descriptions-item>
            <nz-descriptions-item nzTitle="История: обновлено">{{ res.history_updated }}</nz-descriptions-item>
          </nz-descriptions>

          @if (res.stations_not_found.length) {
            <p class="warn-line">Станции вне справочника: {{ res.stations_not_found.join(', ') }}</p>
          }
          @if (res.ops_not_found.length) {
            <p class="warn-line">Операции вне справочника: {{ res.ops_not_found.join(', ') }}</p>
          }
        </nz-card>
      }
    </div>
  `,
  styles: [`
    .page { display: flex; flex-direction: column; gap: var(--space-md); max-width: 960px; }
    .card { border-radius: var(--radius-md); box-shadow: var(--shadow-sm); }
    .hint { color: var(--color-text-secondary); font-size: var(--font-size-subtitle); margin: 0 0 var(--space-md); }
    .msg { margin-top: var(--space-md); }
    .ready-row { display: flex; align-items: center; gap: var(--space-sm); margin-bottom: var(--space-md); }
    .muted { color: var(--color-text-muted); }
    .issue { margin-bottom: var(--space-sm); }
    .process-btn { margin-top: var(--space-md); }
    .warn-line { margin: var(--space-sm) 0 0; color: var(--color-warning); font-size: var(--font-size-sm); }
  `],
})
export class DislocationComponent implements OnInit {
  private readonly api = inject(DislocationApiService);

  readonly status = signal<LKStatus | null>(null);
  readonly loadingStatus = signal(false);
  readonly busyUpload = signal(false);
  readonly busyProcess = signal(false);
  readonly uploadMsg = signal<string | null>(null);
  readonly processResult = signal<LKProcessResult | null>(null);
  readonly error = signal<string | null>(null);

  ngOnInit(): void {
    this.loadStatus();
  }

  /** Возврат false — сами шлём файл через API-сервис, штатный XHR nz-upload не нужен. */
  readonly beforeUpload = (file: NzUploadFile): boolean => {
    this.doUpload(file.originFileObj ?? (file as unknown as File));
    return false;
  };

  async process(): Promise<void> {
    this.busyProcess.set(true);
    this.error.set(null);
    try {
      this.processResult.set(await this.api.process());
      await this.loadStatus();
    } catch (err) {
      this.error.set(apiErrorMessage(err));
    } finally {
      this.busyProcess.set(false);
    }
  }

  private async doUpload(file: File): Promise<void> {
    this.busyUpload.set(true);
    this.error.set(null);
    this.uploadMsg.set(null);
    try {
      const res = await this.api.upload(file);
      this.uploadMsg.set(
        `${res.filename}: ${res.organisation || res.okpo}${res.replaced ? ' (заменён более старый файл)' : ''}`,
      );
      await this.loadStatus();
    } catch (err) {
      this.error.set(apiErrorMessage(err));
    } finally {
      this.busyUpload.set(false);
    }
  }

  private async loadStatus(): Promise<void> {
    this.loadingStatus.set(true);
    try {
      this.status.set(await this.api.getStatus());
    } catch (err) {
      this.error.set(apiErrorMessage(err));
    } finally {
      this.loadingStatus.set(false);
    }
  }
}
