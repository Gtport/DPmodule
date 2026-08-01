import { Component, OnInit, computed, inject, output, signal } from '@angular/core';
import { DragDropModule } from '@angular/cdk/drag-drop';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { DislocationApiService, LKProcessResult, LKStatus } from '../dislocation/dislocation-api.service';
import { FileDropComponent } from '../../shared/file-drop.component';
import { LkFilesViewComponent } from './lk-files-view.component';
import { LkProcessResultComponent } from './lk-process-result.component';

/**
 * Перемещаемая модалка «ЛК» — РУЧНАЯ загрузка файлов кабинета (перенос карточки
 * со страницы «Дислокация» на главный экран, решение владельца). Двухшаговость
 * сохранена: шаг 1 — загрузка xlsx по грузополучателям с контролем свежести/
 * полноты, шаг 2 — «Обновить дислокацию» (пересборка снимка).
 *
 * Автозабор роботом переехал отсюда в отдельную модалку «АВТО ЛК» (решение
 * владельца 01.08.2026): там он вместе с обновлением дислокации идёт одним
 * фоновым запуском, здесь остаётся ручной путь на случай, когда кабинет РЖД
 * недоступен и файлы приносят руками.
 *
 * Сводка пересборки остаётся ЗДЕСЬ, внизу окна: на главном экране показываем
 * только короткий тост, подробности — тому, кто их действительно смотрит.
 */
@Component({
  selector: 'app-lk-intake-modal',
  imports: [
    DragDropModule, NzButtonModule, NzIconModule, NzModalModule, NzSpinModule, NzTagModule,
    NzTooltipModule, FileDropComponent, LkFilesViewComponent, LkProcessResultComponent,
  ],
  template: `
    <nz-modal [nzVisible]="true" [nzTitle]="title" [nzFooter]="null" nzWidth="560px"
              [nzMask]="false" (nzOnCancel)="closed.emit()">
      <ng-template #title>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          ЛК — ручная загрузка файлов
        </div>
      </ng-template>
      <ng-container *nzModalContent>
        <div class="bar">
          <span class="hint">Шаг 1 — загрузите xlsx (по одному на грузополучателя). Шаг 2 — «Обновить дислокацию».</span>
          <span class="spacer"></span>
          <button nz-button nzType="text" nzSize="small" nz-tooltip
                  nzTooltipTitle="Обновить список принятых файлов" (click)="loadStatus()">
            <span nz-icon nzType="reload"></span>
          </button>
        </div>

        <app-file-drop accept=".xlsx" [multiple]="true" [busy]="busyUpload()"
                       text="Нажмите или перетащите файлы ЛК в эту область"
                       hint="xlsx, по одному файлу на грузополучателя; можно несколько сразу"
                       (file)="onLkFile($event)" />

        <nz-spin [nzSpinning]="loadingStatus()">
          <app-lk-files-view [status]="status()"
                             emptyText="Файлы ЛК не загружены (для ручной загрузки). Основной источник — АСУ." />
        </nz-spin>

        <!-- Шаг 2 — отдельной строкой под файлами (визуально отделён от загрузки). -->
        @if (status(); as st) {
          @if (st.files.length) {
            <div class="step2">
              <nz-tag [nzColor]="st.ready ? 'success' : 'error'">
                {{ st.ready ? 'готово к обработке' : notReadyReason(st) }}
              </nz-tag>
              <span class="spacer"></span>
              <button nz-button nzType="primary" [disabled]="!st.ready" [nzLoading]="busyProcess()"
                      (click)="process()">
                Обновить дислокацию
              </button>
            </div>
          }
        }

        <app-lk-process-result [res]="processResult()" />
      </ng-container>
    </nz-modal>
  `,
  styles: [`
    .ttl { cursor: move; user-select: none; }
    .bar { display: flex; align-items: center; gap: var(--space-sm); margin-bottom: var(--space-sm); }
    .hint { color: var(--color-text-secondary); font-size: var(--font-size-sm); }
    .spacer { flex: 1 1 auto; }
    /* Шаг 2 — статус + кнопка обработки, отдельной строкой под списком файлов. */
    .step2 {
      display: flex; align-items: center; gap: var(--space-sm);
      margin-top: var(--space-md); padding-top: var(--space-md);
      border-top: 1px solid var(--color-border, #f0f0f0);
    }
  `],
})
export class LkIntakeModalComponent implements OnInit {
  private readonly api = inject(DislocationApiService);
  private readonly msg = inject(NzMessageService);

  readonly closed = output<void>();
  /** Снимок пересобран — родитель освежает статус-панель и счётчики. */
  readonly updated = output<void>();

  readonly status = signal<LKStatus | null>(null);
  readonly loadingStatus = signal(false);
  readonly pendingUploads = signal(0);
  readonly busyUpload = computed(() => this.pendingUploads() > 0);
  readonly busyProcess = signal(false);
  readonly processResult = signal<LKProcessResult | null>(null);

  /** Загрузки одного «выбора»/drop идут строго по очереди — на этой цепочке. */
  private uploadChain: Promise<void> = Promise.resolve();

  ngOnInit(): void {
    void this.loadStatus();
  }

  /** Файл из зоны загрузки (app-file-drop): очередь последовательной отправки. */
  onLkFile(raw: File): void {
    this.pendingUploads.update((n) => n + 1);
    this.uploadChain = this.uploadChain
      .then(() => this.doUpload(raw))
      .finally(() => this.pendingUploads.update((n) => n - 1));
  }

  /** Обработать принятые файлы ЛК в снимок (шаг 2). */
  async process(): Promise<void> {
    this.busyProcess.set(true);
    try {
      const res = await this.api.process();
      this.processResult.set(res);
      this.msg.success(`Дислокация обновлена из ЛК: ${res.count} ваг. (было ${res.prev_snapshot})`);
      this.updated.emit();
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.busyProcess.set(false);
      // Список файлов освежаем в любом исходе: отказ обработки тоже меняет
      // картину приёма (устарели, разъехались срезы), и увидеть её надо сразу.
      await this.loadStatus();
    }
  }

  private async doUpload(file: File): Promise<void> {
    try {
      const res = await this.api.upload(file);
      this.msg.success(
        `${res.filename}: ${res.organisation || res.okpo}${res.replaced ? ' (заменён более старый файл)' : ''}`,
      );
    } catch (err) {
      this.msg.error(`${file.name}: ${apiErrorMessage(err)}`);
    } finally {
      await this.loadStatus();
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

  /** Честный статус «почему не готово» по блокирующим замечаниям. */
  notReadyReason(st: LKStatus): string {
    const blocks = st.issues.filter((i) => i.level === 'block').map((i) => i.code);
    if (blocks.includes('stale')) return 'файлы устарели';
    if (blocks.includes('missing')) return 'не хватает файлов грузополучателей';
    if (blocks.includes('gap')) return 'файлы из разных срезов';
    return 'есть замечания — обработка невозможна';
  }
}
