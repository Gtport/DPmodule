import { Component, OnInit, ViewChild, computed, inject, signal } from '@angular/core';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzCardModule } from 'ng-zorro-antd/card';
import { NzMessageService } from 'ng-zorro-antd/message';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { NzUploadModule, NzUploadFile } from 'ng-zorro-antd/upload';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzDescriptionsModule } from 'ng-zorro-antd/descriptions';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { apiErrorMessage } from '../../core/api/api-error';
import { DislocationApiService, LKIssue, LKProcessResult, LKStatus } from './dislocation-api.service';
import { PlanStatusPanelComponent } from '../plan/plan-status-panel.component';

/**
 * Раздел «Дислокация»: статус-панель системы сверху + компактный приём ЛК
 * (загрузка xlsx → контроль → «Обновить дислокацию») + ручной забор из АСУ.
 * Двухшаговость ЛК сохранена (диспетчер видит файлы/замечания до пересборки),
 * но подача компактная (тулбар + список), по образцу gtport LKManager2.
 */
@Component({
  selector: 'app-dislocation',
  imports: [
    NzButtonModule, NzCardModule, NzTagModule,
    NzUploadModule, NzIconModule, NzTooltipModule, NzDescriptionsModule, NzSpinModule,
    PlanStatusPanelComponent,
  ],
  template: `
    <div class="page">
      <app-plan-status-panel />

      <!-- АСУ: одношаговое обновление дислокации (в один клик, как автозабор) -->
      <div class="asu-bar">
        <button nz-button nzType="primary" [nzLoading]="busyAsu()" (click)="asuPull()">
          <span nz-icon nzType="cloud-download"></span> Обновить из АСУ
        </button>
        <span class="asu-hint">В один клик: заберёт из АСУ и сразу обновит дислокацию — отдельно ничего жать не нужно.</span>
      </div>

      <!-- ЛК: ручной двухшаговый приём (загрузка → обработка) -->
      <nz-card nzTitle="Приём ЛК (ручной)" class="card">
        <p class="hint">Шаг 1 — загрузите xlsx-файлы (по одному на грузополучателя). Шаг 2 — «Обновить дислокацию».</p>

        <div class="toolbar">
          <nz-upload nzAccept=".xlsx" [nzMultiple]="true" [nzShowUploadList]="false" [nzBeforeUpload]="beforeUpload">
            <button nz-button [nzLoading]="busyUpload()">
              <span nz-icon nzType="upload"></span> Загрузить ЛК
            </button>
          </nz-upload>

          <button nz-button nz-tooltip nzTooltipTitle="Обновить список принятых файлов" (click)="loadStatus()">
            <span nz-icon nzType="reload"></span>
          </button>
        </div>

        <nz-spin [nzSpinning]="loadingStatus()">
          <div class="files">
            @for (f of status()?.files ?? []; track f.filename) {
              <div class="frow" [class.frow-warn]="issuesFor(f.okpo).length">
                <span class="forg" [title]="f.organisation">{{ f.organisation || ('ОКПО ' + f.okpo) }}</span>
                <span class="fterm">{{ f.terminals.join(' · ') || '—' }}</span>
                <nz-tag class="chip" [nzColor]="ageColor(f.age_minutes)">{{ fmtTs(f.formation_ts) }} · {{ f.age_minutes }}м</nz-tag>
                @for (iss of issuesFor(f.okpo); track iss.code) {
                  <nz-tag class="chip" [nzColor]="iss.level === 'block' ? 'error' : 'warning'"
                          nz-tooltip [nzTooltipTitle]="iss.message">{{ issueLabel(iss.code) }}</nz-tag>
                }
                <span class="fname" [title]="f.filename">{{ f.filename }}</span>
              </div>
            } @empty {
              <p class="muted">Файлы ЛК не загружены (для ручной загрузки). Основной источник — АСУ выше.</p>
            }
            <!-- Общие замечания (не привязаны к конкретному файлу): нет файла, разрыв срезов. -->
            @for (iss of orphanIssues(); track $index) {
              <div class="frow frow-issue">
                <nz-tag class="chip" [nzColor]="iss.level === 'block' ? 'error' : 'warning'">{{ issueLabel(iss.code) }}</nz-tag>
                <span class="imsg">{{ iss.message }}</span>
              </div>
            }
          </div>
        </nz-spin>

        <!-- Шаг 2 — отдельной строкой под файлами (визуально отделён от загрузки). -->
        @if (status(); as st) {
          @if (st.files.length) {
            <div class="step2">
              <nz-tag [nzColor]="st.ready ? 'success' : 'error'">
                {{ st.ready ? 'готово к обработке' : notReadyReason(st) }}
              </nz-tag>
              <span class="spacer"></span>
              <button
                nz-button
                nzType="primary"
                [disabled]="!st.ready"
                [nzLoading]="busyProcess()"
                (click)="process()"
              >
                Обновить дислокацию
              </button>
            </div>
          }
        }
      </nz-card>

      @if (processResult(); as res) {
        <nz-card class="card">
          <div class="rsum">
            <b>Дислокация обновлена ({{ resultSource() }}):</b>
            <span>вагонов <b>{{ res.count }}</b> (было {{ res.prev_snapshot }})</span>
            <span>· прогноз {{ res.prog_computed }}</span>
            <span>· расч. ход {{ res.forecast_computed }}</span>
            <span>· пропали {{ res.status8_missing }}</span>
            <span>· история +{{ res.history_inserted }}/~{{ res.history_updated }}</span>
            <button nz-button nzType="link" nzSize="small" (click)="showDetails.set(!showDetails())">
              {{ showDetails() ? 'скрыть' : 'подробнее' }}
            </button>
          </div>

          @if (showDetails()) {
            <nz-descriptions class="details" [nzColumn]="3" nzBordered nzSize="small">
              <nz-descriptions-item nzTitle="Файлов">{{ res.files }}</nz-descriptions-item>
              <nz-descriptions-item nzTitle="Назначение обогащено">{{ res.nazn_enriched }}</nz-descriptions-item>
              <nz-descriptions-item nzTitle="Порт не резолвится">{{ res.port_unresolved }}</nz-descriptions-item>
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
              <nz-descriptions-item nzTitle="Прогноз (ProgMsk)">{{ res.prog_computed }}</nz-descriptions-item>
            </nz-descriptions>
            @if (res.stations_not_found.length) {
              <p class="warn-line">Станции вне справочника: {{ res.stations_not_found.join(', ') }}</p>
            }
            @if (res.ops_not_found.length) {
              <p class="warn-line">Операции вне справочника: {{ res.ops_not_found.join(', ') }}</p>
            }
          }
        </nz-card>
      }
    </div>
  `,
  styles: [`
    /* Страница — на всю ширину (статус-панель и т.п.); карточки приёма — по-прежнему компактные. */
    .page { display: flex; flex-direction: column; gap: var(--space-md); }
    .asu-bar, .card { max-width: 700px; }
    .card { border-radius: var(--radius-md); box-shadow: var(--shadow-sm); }
    /* АСУ — отдельная строка над карточкой ЛК (основной, одношаговый источник). */
    .asu-bar { display: flex; align-items: center; gap: var(--space-md); flex-wrap: wrap; }
    .asu-hint { color: var(--color-text-secondary); font-size: var(--font-size-sm); }
    .hint { color: var(--color-text-secondary); font-size: var(--font-size-sm); margin: 0 0 var(--space-md); }
    .toolbar { display: flex; align-items: center; gap: var(--space-sm); flex-wrap: wrap; }
    .spacer { flex: 1 1 auto; }
    /* Шаг 2 — статус + кнопка обработки, отдельной строкой под списком файлов. */
    .step2 {
      display: flex; align-items: center; gap: var(--space-sm);
      margin-top: var(--space-md); padding-top: var(--space-md);
      border-top: 1px solid var(--color-border, #f0f0f0);
    }
    .muted { color: var(--color-text-muted); margin: var(--space-sm) 0 0; }
    /* Компактный список файлов ЛК — каждый файл строго в одну строку. */
    .files { margin-top: var(--space-sm); display: flex; flex-direction: column; }
    .frow {
      display: flex; flex-wrap: nowrap; align-items: center; gap: var(--space-md);
      padding: 4px 2px; border-bottom: 1px solid var(--color-border, #f0f0f0); font-size: var(--font-size-sm);
    }
    .frow:last-child { border-bottom: none; }
    .frow-warn { background: var(--color-warning-bg, #fffbe6); }
    .frow-issue { color: var(--color-text-secondary); }
    .forg { flex: 0 1 220px; min-width: 120px; font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .fterm { flex: 0 0 auto; color: var(--color-text-secondary); white-space: nowrap; }
    .fname { flex: 1 1 auto; min-width: 0; color: var(--color-text-muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; text-align: right; }
    .imsg { flex: 1 1 auto; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .chip { margin: 0; white-space: nowrap; }
    /* Сводка результата */
    .rsum { display: flex; align-items: center; gap: var(--space-sm); flex-wrap: wrap; font-size: var(--font-size-sm); }
    .details { margin-top: var(--space-md); }
    .warn-line { margin: var(--space-sm) 0 0; color: var(--color-warning); font-size: var(--font-size-sm); }
  `],
})
export class DislocationComponent implements OnInit {
  private readonly api = inject(DislocationApiService);
  private readonly msg = inject(NzMessageService);

  @ViewChild(PlanStatusPanelComponent) private statusPanel?: PlanStatusPanelComponent;

  readonly status = signal<LKStatus | null>(null);
  readonly loadingStatus = signal(false);
  readonly pendingUploads = signal(0);
  readonly busyUpload = computed(() => this.pendingUploads() > 0);
  readonly busyProcess = signal(false);
  readonly busyAsu = signal(false);
  readonly processResult = signal<LKProcessResult | null>(null);
  readonly resultSource = signal<'ЛК' | 'АСУ'>('ЛК');
  readonly showDetails = signal(false);

  /** Загрузки одного «выбора»/drop идут строго по очереди — на этой цепочке. */
  private uploadChain: Promise<void> = Promise.resolve();

  ngOnInit(): void {
    this.loadStatus();
  }

  readonly beforeUpload = (file: NzUploadFile, _fileList: NzUploadFile[]): boolean => {
    const raw = file.originFileObj ?? (file as unknown as File);
    this.pendingUploads.update((n) => n + 1);
    this.uploadChain = this.uploadChain
      .then(() => this.doUpload(raw))
      .finally(() => this.pendingUploads.update((n) => n - 1));
    return false;
  };

  /** Обработать принятые файлы ЛК в снимок (шаг 2). */
  async process(): Promise<void> {
    this.busyProcess.set(true);
    try {
      this.processResult.set(await this.api.process());
      this.resultSource.set('ЛК');
      this.msg.success('Дислокация обновлена из ЛК');
      await this.loadStatus();
      void this.statusPanel?.load();
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.busyProcess.set(false);
    }
  }

  /** Ручной забор дислокации из АСУ (пересобирает снимок тем же конвейером). */
  async asuPull(): Promise<void> {
    this.busyAsu.set(true);
    try {
      this.processResult.set(await this.api.asuPull());
      this.resultSource.set('АСУ');
      this.msg.success('Дислокация обновлена из АСУ');
      await this.loadStatus();
      void this.statusPanel?.load();
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.busyAsu.set(false);
    }
  }

  private async doUpload(file: File): Promise<void> {
    try {
      const res = await this.api.upload(file);
      this.msg.success(
        `${res.filename}: ${res.organisation || res.okpo}${res.replaced ? ' (заменён более старый файл)' : ''}`,
      );
      await this.loadStatus();
    } catch (err) {
      this.msg.error(`${file.name}: ${apiErrorMessage(err)}`);
    }
  }

  async loadStatus(): Promise<void> {
    this.loadingStatus.set(true);
    try {
      this.status.set(await this.api.getStatus());
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.loadingStatus.set(false);
    }
  }

  /** Замечания, привязанные к файлу с этим ОКПО (устаревание, неизвестный ОКПО). */
  issuesFor(okpo: string): LKIssue[] {
    return (this.status()?.issues ?? []).filter((i) => i.okpo === okpo);
  }

  /** Общие замечания без своей строки-файла: нет файла (missing) и разрыв срезов (gap). */
  orphanIssues(): LKIssue[] {
    const present = new Set((this.status()?.files ?? []).map((f) => f.okpo));
    return (this.status()?.issues ?? []).filter((i) => !i.okpo || !present.has(i.okpo));
  }

  /** Честный статус «почему не готово» по блокирующим замечаниям (детали — в строках файлов). */
  notReadyReason(st: LKStatus): string {
    const blocks = st.issues.filter((i) => i.level === 'block').map((i) => i.code);
    if (blocks.includes('stale')) return 'файлы устарели — не годятся для обновления';
    if (blocks.includes('missing')) return 'не хватает файлов грузополучателей';
    if (blocks.includes('gap')) return 'файлы из разных срезов';
    return 'есть замечания — обработка невозможна';
  }

  /** Короткая подпись тега по коду замечания (полный текст — в тултипе/строке). */
  issueLabel(code: string): string {
    switch (code) {
      case 'stale': return 'устарел';
      case 'unknown': return 'нет в справочнике';
      case 'missing': return 'нет файла';
      case 'gap': return 'разрыв срезов';
      default: return code;
    }
  }

  /** Цвет чипа по возрасту метки формирования (мин): ≤60 синий, ≤180 оранжевый, иначе красный. */
  ageColor(age: number): string {
    if (age <= 60) return 'blue';
    if (age <= 180) return 'orange';
    return 'red';
  }

  /** «2026-07-14T03:42:33» → «14.07 03:42». */
  fmtTs(ts: string | null): string {
    if (!ts || ts.length < 16) return '—';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)} ${ts.slice(11, 16)}`;
  }
}
