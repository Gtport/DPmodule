import { Component, HostListener, booleanAttribute, input, output, signal } from '@angular/core';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzUploadModule, NzUploadFile } from 'ng-zorro-antd/upload';

/**
 * Единый загрузчик файлов модуля (стиль gtport FileUploader): зона с пунктиром,
 * куда файл можно ПЕРЕТАЩИТЬ из проводника или нажать и выбрать. Используется
 * везде, где принимаются файлы (план подвода, ЛК, будущие загрузки) — вместо
 * разрозненных кнопок «Загрузить».
 *
 * Файлы отдаются наружу по одному (`file`), очередь/отправку ведёт родитель —
 * компонент только принимает и фильтрует по расширению (accept). Режимы:
 * - по умолчанию — постоянная зона (для карточек, напр. приём ЛК);
 * - `compact` — узкая однострочная зона для тулбаров;
 * - `overlay` — места не занимает: маленькая кнопка (клик → проводник), а при
 *   перетаскивании файла над страницей поверх экрана появляется большая зона
 *   «отпустите файл» (слушатели drag* — на документе, активны только пока
 *   компонент на экране).
 */
@Component({
  selector: 'app-file-drop',
  imports: [NzUploadModule, NzIconModule, NzButtonModule, NzTooltipModule],
  template: `
    @if (overlay()) {
      <nz-upload [nzAccept]="accept()" [nzMultiple]="multiple()"
                 [nzShowUploadList]="false" [nzBeforeUpload]="beforeUpload" [nzDisabled]="busy()">
        <button nz-button nzType="text" [nzLoading]="busy()" nz-tooltip
                [nzTooltipTitle]="text() + ' — нажмите или перетащите файл на страницу'">
          <span nz-icon nzType="upload"></span>@if (buttonLabel()) { {{ buttonLabel() }} }
        </button>
      </nz-upload>
      @if (dragActive() && !busy()) {
        <div class="ov" (dragover)="$event.preventDefault()" (drop)="onOverlayDrop($event)">
          <div class="ov-box">
            <span nz-icon nzType="inbox" class="icon"></span>
            <div class="text">Отпустите файл — {{ text() }}</div>
          </div>
        </div>
      }
    } @else {
      <nz-upload nzType="drag" [nzAccept]="accept()" [nzMultiple]="multiple()"
                 [nzShowUploadList]="false" [nzBeforeUpload]="beforeUpload"
                 [nzDisabled]="busy()" class="drop" [class.compact]="compact()">
        <div class="inner">
          <span nz-icon [nzType]="busy() ? 'loading' : 'inbox'" class="icon"></span>
          <div class="texts">
            <div class="text">{{ text() }}</div>
            @if (hint() && !compact()) { <div class="hint">{{ hint() }}</div> }
          </div>
        </div>
      </nz-upload>
    }
  `,
  styles: [`
    /* Вид зоны — как gtport FileUploader (Dragger): пунктир в брендовом синем,
       лёгкая синяя подложка, при наведении — ярче. ::ng-deep — точечно, только
       на контейнер собственной drag-зоны (Less-переменной для этого нет). */
    :host { display: block; }
    .drop { display: block; height: 100%; }
    :host ::ng-deep .ant-upload.ant-upload-drag {
      border: 2px dashed var(--color-primary);
      background: var(--color-drop-bg);
      border-radius: var(--radius-md);
      height: 100%;
    }
    :host ::ng-deep .ant-upload.ant-upload-drag:hover,
    :host ::ng-deep .ant-upload.ant-upload-drag.ant-upload-drag-hover {
      border-color: var(--color-primary-hover);
      background: var(--color-drop-bg-hover);
    }
    :host ::ng-deep .ant-upload.ant-upload-drag .ant-upload-btn {
      height: 100%; display: flex; align-items: center; justify-content: center;
      padding: var(--space-md);
    }
    .inner { display: flex; flex-direction: column; align-items: center; gap: var(--space-xs); }
    .icon { font-size: 32px; color: var(--color-primary); }
    .text { color: var(--color-text); font-size: var(--font-size-base); }
    .hint { color: var(--color-text-muted); font-size: var(--font-size-sm); }
    /* Компактный режим — для тулбаров: одна строка, небольшая высота. */
    .compact ::ng-deep .ant-upload.ant-upload-drag .ant-upload-btn { padding: 2px var(--space-sm); }
    .compact .inner { flex-direction: row; gap: var(--space-sm); }
    .compact .icon { font-size: 18px; }
    .compact .text { font-size: var(--font-size-sm); }
    /* Оверлей на весь экран — появляется только пока файл тянут над страницей. */
    .ov {
      position: fixed; inset: 0; z-index: var(--z-modal);
      background: var(--color-drop-overlay);
      display: flex; align-items: center; justify-content: center;
      padding: var(--space-xl);
    }
    .ov-box {
      display: flex; flex-direction: column; align-items: center; gap: var(--space-sm);
      width: 100%; height: 100%; justify-content: center;
      border: 3px dashed var(--color-primary);
      border-radius: var(--radius-card);
      background: var(--color-bg-surface);
      box-shadow: var(--shadow-card);
      pointer-events: none; /* цель drop — сам оверлей, рамка только рисуется */
    }
    .ov-box .icon { font-size: 48px; }
    .ov-box .text { font-size: var(--font-size-card-title); }
  `],
})
export class FileDropComponent {
  /** Допустимые расширения (атрибут accept), напр. «.xlsx». */
  readonly accept = input('.xlsx');
  /** Разрешить выбор/перетаскивание нескольких файлов за раз. */
  readonly multiple = input(false);
  /** Занятость родителя: зона/кнопка блокируется, иконка — спиннер. */
  readonly busy = input(false);
  /** Основной текст зоны; в overlay-режиме — что произойдёт («план загрузится»). */
  readonly text = input('Нажмите или перетащите файл в эту область');
  /** Подсказка под текстом (в компактном режиме не показывается). */
  readonly hint = input('');
  /** Узкая однострочная зона для тулбаров (атрибут: <app-file-drop compact>). */
  readonly compact = input(false, { transform: booleanAttribute });
  /** Режим «кнопка + оверлей»: постоянной зоны нет, drop-зона на весь экран при перетаскивании. */
  readonly overlay = input(false, { transform: booleanAttribute });
  /** Подпись кнопки overlay-режима; пусто — только иконка (подсказка в тултипе). */
  readonly buttonLabel = input('');

  /** Принятый файл (по одному событию на файл, в порядке выбора). */
  readonly file = output<File>();

  /** Оверлей виден (файл тянут над страницей). Глубина — из-за парных enter/leave. */
  readonly dragActive = signal(false);
  private dragDepth = 0;

  /** false — штатный XHR nz-upload не нужен, файл отдаём родителю. */
  readonly beforeUpload = (f: NzUploadFile, _list: NzUploadFile[]): boolean => {
    this.file.emit((f.originFileObj ?? f) as unknown as File);
    return false;
  };

  // ── overlay: слушатели документа (активны, пока компонент на экране) ──────
  @HostListener('document:dragenter', ['$event'])
  onDocDragEnter(e: DragEvent): void {
    if (!this.overlay() || this.busy() || !hasFiles(e)) return;
    this.dragDepth++;
    this.dragActive.set(true);
  }

  @HostListener('document:dragleave', ['$event'])
  onDocDragLeave(_e: DragEvent): void {
    if (!this.overlay()) return;
    if (--this.dragDepth <= 0) {
      this.dragDepth = 0;
      this.dragActive.set(false);
    }
  }

  /** Drop мимо оверлея (когда тот скрыт) — глушим, чтобы браузер не открыл файл. */
  @HostListener('document:drop', ['$event'])
  onDocDrop(e: DragEvent): void {
    if (!this.overlay()) return;
    if (this.dragActive()) e.preventDefault();
    this.dragDepth = 0;
    this.dragActive.set(false);
  }

  /** Глушим dragover документа — иначе drop не срабатывает. */
  @HostListener('document:dragover', ['$event'])
  onDocDragOver(e: DragEvent): void {
    if (this.overlay() && this.dragActive()) e.preventDefault();
  }

  onOverlayDrop(e: DragEvent): void {
    e.preventDefault();
    this.dragDepth = 0;
    this.dragActive.set(false);
    const exts = this.accept().split(',').map((s) => s.trim().toLowerCase()).filter(Boolean);
    const files = [...(e.dataTransfer?.files ?? [])].filter(
      (f) => !exts.length || exts.some((ext) => f.name.toLowerCase().endsWith(ext)),
    );
    for (const f of this.multiple() ? files : files.slice(0, 1)) this.file.emit(f);
  }
}

/** В переносе участвуют файлы (а не текст/ссылка). */
function hasFiles(e: DragEvent): boolean {
  return [...(e.dataTransfer?.types ?? [])].includes('Files');
}
