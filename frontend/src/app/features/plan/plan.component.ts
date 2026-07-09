import { Component, OnInit, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { HttpErrorResponse } from '@angular/common/http';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzCardModule } from 'ng-zorro-antd/card';
import { NzAlertModule } from 'ng-zorro-antd/alert';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { NzTableModule } from 'ng-zorro-antd/table';
import { NzSelectModule } from 'ng-zorro-antd/select';
import { NzUploadModule, NzUploadFile } from 'ng-zorro-antd/upload';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzDescriptionsModule } from 'ng-zorro-antd/descriptions';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { apiErrorMessage } from '../../core/api/api-error';
import { PlanApiService, PlanGrid, PlanUploadResult } from './plan-api.service';

/**
 * Станции плана подвода со встроенным профилем на бэке (см.
 * internal/parser/plan/profile.go, ResolveProfile). Список статичный —
 * эндпоинта «перечислить коды плана» нет; появится профиль из БД — добавить
 * сюда строку.
 */
const STATION_OPTIONS: { code: string; label: string }[] = [
  { code: 'ma', label: 'Мыс Астафьева' },
  { code: 'nk', label: 'Находка' },
];

/**
 * Раздел «План подвода»: загрузка xlsx-расписания ниток для выбранной станции
 * (разбор + матч с вагонами дислокации + простановка PlanMsk — одним шагом на
 * бэке), затем отображение сохранённой сетки ниток.
 */
@Component({
  selector: 'app-plan',
  imports: [
    FormsModule, NzButtonModule, NzCardModule, NzAlertModule, NzTagModule,
    NzTableModule, NzSelectModule, NzUploadModule, NzIconModule,
    NzDescriptionsModule, NzSpinModule,
  ],
  template: `
    <div class="page">
      <nz-card nzTitle="Загрузка плана подвода" class="card">
        <div class="row">
          <span>Станция плана:</span>
          <nz-select [ngModel]="selectedCode()" (ngModelChange)="onCodeChange($event)" style="width: 220px">
            @for (o of stationOptions; track o.code) {
              <nz-option [nzValue]="o.code" [nzLabel]="o.label" />
            }
          </nz-select>

          <nz-upload nzAccept=".xlsx" [nzShowUploadList]="false" [nzBeforeUpload]="beforeUpload">
            <button nz-button [nzLoading]="busyUpload()">
              <span nz-icon nzType="upload"></span>
              Выбрать файл плана
            </button>
          </nz-upload>
        </div>

        @if (uploadResult(); as res) {
          <nz-alert
            class="msg" nzType="success" nzShowIcon nzCloseable
            [nzMessage]="res.filename + ': ниток ' + res.nitki + ', сопоставлено ' + res.matched + ', вагонов застолблено ' + res.stamped + (res.cleared ? ', очищено ' + res.cleared : '')"
          />
        }
        @if (error()) {
          <nz-alert class="msg" nzType="error" [nzMessage]="error()!" nzShowIcon nzCloseable />
        }
      </nz-card>

      <nz-card [nzTitle]="'Сетка плана: ' + selectedLabel()" class="card">
        <nz-spin [nzSpinning]="loadingGrid()">
          @if (grid(); as g) {
            <nz-descriptions [nzColumn]="3" nzBordered nzSize="small" class="header">
              <nz-descriptions-item nzTitle="Файл">{{ g.plan.source_file }}</nz-descriptions-item>
              <nz-descriptions-item nzTitle="Загружен">{{ g.plan.loaded_at || '—' }}</nz-descriptions-item>
              <nz-descriptions-item nzTitle="Ниток">{{ g.plan.nitki }}</nz-descriptions-item>
              <nz-descriptions-item nzTitle="Сопоставлено">{{ g.plan.matched }}</nz-descriptions-item>
              <nz-descriptions-item nzTitle="Вагонов застолблено">{{ g.plan.stamped }}</nz-descriptions-item>
            </nz-descriptions>

            <nz-table [nzData]="g.nitki" [nzShowPagination]="g.nitki.length > 20" [nzScroll]="{ x: '1100px' }">
              <thead>
                <tr>
                  <th>№</th>
                  <th>Индекс</th>
                  <th>Индекс ПП</th>
                  <th>План МСК</th>
                  <th>План ЖД</th>
                  <th>Факт МСК</th>
                  <th>Откл.</th>
                  <th>Вагонов</th>
                  <th>Activ</th>
                  <th>Сопоставлена</th>
                  <th>Застолблено</th>
                </tr>
              </thead>
              <tbody>
                @for (n of g.nitki; track n.ord) {
                  <tr>
                    <td>{{ n.ord }}</td>
                    <td>{{ n.index }}</td>
                    <td>{{ n.index_pp || '—' }}</td>
                    <td>{{ n.plan_msk || '—' }}</td>
                    <td>{{ n.plan_jd || '—' }}</td>
                    <td>{{ n.fact_msk || '—' }}</td>
                    <td>{{ n.otkl || '—' }}</td>
                    <td>{{ n.wagons }}</td>
                    <td>{{ n.activ }}</td>
                    <td>
                      @if (n.matched) {
                        <nz-tag nzColor="success">да</nz-tag>
                      } @else {
                        <nz-tag>нет</nz-tag>
                      }
                    </td>
                    <td>{{ n.matched_wagons }}</td>
                  </tr>
                }
              </tbody>
            </nz-table>
          } @else if (!loadingGrid()) {
            <p class="muted">План для станции «{{ selectedLabel() }}» ещё не загружен.</p>
          }
        </nz-spin>
      </nz-card>
    </div>
  `,
  styles: [`
    .page { display: flex; flex-direction: column; gap: var(--space-md); max-width: 1160px; }
    .card { border-radius: var(--radius-md); box-shadow: var(--shadow-sm); }
    .row { display: flex; align-items: center; gap: var(--space-md); flex-wrap: wrap; }
    .msg { margin-top: var(--space-md); }
    .header { margin-bottom: var(--space-md); }
    .muted { color: var(--color-text-muted); }
  `],
})
export class PlanComponent implements OnInit {
  private readonly api = inject(PlanApiService);

  readonly stationOptions = STATION_OPTIONS;
  readonly selectedCode = signal(STATION_OPTIONS[0].code);
  readonly selectedLabel = signal(STATION_OPTIONS[0].label);

  readonly grid = signal<PlanGrid | null>(null);
  readonly loadingGrid = signal(false);
  readonly busyUpload = signal(false);
  readonly uploadResult = signal<PlanUploadResult | null>(null);
  readonly error = signal<string | null>(null);

  ngOnInit(): void {
    this.loadGrid(this.selectedCode());
  }

  onCodeChange(code: string): void {
    this.selectedCode.set(code);
    this.selectedLabel.set(this.stationOptions.find((o) => o.code === code)?.label ?? code);
    this.uploadResult.set(null);
    this.loadGrid(code);
  }

  /** Возврат false — сами шлём файл через API-сервис, штатный XHR nz-upload не нужен. */
  readonly beforeUpload = (file: NzUploadFile): boolean => {
    this.doUpload(file.originFileObj ?? (file as unknown as File));
    return false;
  };

  private async doUpload(file: File): Promise<void> {
    this.busyUpload.set(true);
    this.error.set(null);
    this.uploadResult.set(null);
    try {
      this.uploadResult.set(await this.api.upload(this.selectedCode(), file));
      await this.loadGrid(this.selectedCode());
    } catch (err) {
      this.error.set(apiErrorMessage(err));
    } finally {
      this.busyUpload.set(false);
    }
  }

  private async loadGrid(code: string): Promise<void> {
    this.loadingGrid.set(true);
    try {
      this.grid.set(await this.api.getPlan(code));
    } catch (err) {
      if (err instanceof HttpErrorResponse && err.status === 404) {
        this.grid.set(null);
      } else {
        this.error.set(apiErrorMessage(err));
      }
    } finally {
      this.loadingGrid.set(false);
    }
  }
}
