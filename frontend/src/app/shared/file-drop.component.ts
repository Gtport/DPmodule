import { Component, booleanAttribute, input, output } from '@angular/core';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzUploadModule, NzUploadFile } from 'ng-zorro-antd/upload';

/**
 * Единый загрузчик файлов модуля (стиль gtport FileUploader): зона с пунктиром,
 * куда файл можно ПЕРЕТАЩИТЬ из проводника или нажать и выбрать. Используется
 * везде, где принимаются файлы (план подвода, ЛК, будущие загрузки) — вместо
 * разрозненных кнопок «Загрузить».
 *
 * Файлы отдаются наружу по одному (`file`), очередь/отправку ведёт родитель —
 * компонент только принимает и валидирует расширение (accept). Режим `compact` —
 * узкая горизонтальная зона для тулбаров (иконка + текст в одну строку).
 */
@Component({
  selector: 'app-file-drop',
  imports: [NzUploadModule, NzIconModule],
  template: `
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
  `,
  styles: [`
    /* Вид зоны — как gtport FileUploader (Dragger): пунктир в брендовом синем,
       лёгкая синяя подложка, при наведении — ярче. ::ng-deep — точечно, только
       на контейнер собственной drag-зоны (Less-переменной для этого нет). */
    :host { display: block; }
    :host ::ng-deep .ant-upload.ant-upload-drag {
      border: 2px dashed var(--color-primary);
      background: var(--color-drop-bg);
      border-radius: var(--radius-md);
    }
    :host ::ng-deep .ant-upload.ant-upload-drag:hover,
    :host ::ng-deep .ant-upload.ant-upload-drag.ant-upload-drag-hover {
      border-color: var(--color-primary-hover);
      background: var(--color-drop-bg-hover);
    }
    :host ::ng-deep .ant-upload.ant-upload-drag .ant-upload-btn { padding: var(--space-md); }
    .inner { display: flex; flex-direction: column; align-items: center; gap: var(--space-xs); }
    .icon { font-size: 32px; color: var(--color-primary); }
    .text { color: var(--color-text); font-size: var(--font-size-base); }
    .hint { color: var(--color-text-muted); font-size: var(--font-size-sm); }
    /* Компактный режим — для тулбаров: одна строка, небольшая высота. */
    .compact ::ng-deep .ant-upload.ant-upload-drag .ant-upload-btn { padding: 2px var(--space-sm); }
    .compact .inner { flex-direction: row; gap: var(--space-sm); }
    .compact .icon { font-size: 18px; }
    .compact .text { font-size: var(--font-size-sm); }
  `],
})
export class FileDropComponent {
  /** Допустимые расширения (атрибут accept), напр. «.xlsx». */
  readonly accept = input('.xlsx');
  /** Разрешить выбор/перетаскивание нескольких файлов за раз. */
  readonly multiple = input(false);
  /** Занятость родителя: зона блокируется, иконка — спиннер. */
  readonly busy = input(false);
  /** Основной текст зоны. */
  readonly text = input('Нажмите или перетащите файл в эту область');
  /** Подсказка под текстом (в компактном режиме не показывается). */
  readonly hint = input('');
  /** Узкая однострочная зона для тулбаров (атрибут: <app-file-drop compact>). */
  readonly compact = input(false, { transform: booleanAttribute });

  /** Принятый файл (по одному событию на файл, в порядке выбора). */
  readonly file = output<File>();

  /** false — штатный XHR nz-upload не нужен, файл отдаём родителю. */
  readonly beforeUpload = (f: NzUploadFile, _list: NzUploadFile[]): boolean => {
    this.file.emit((f.originFileObj ?? f) as unknown as File);
    return false;
  };
}
