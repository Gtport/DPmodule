import { Component, OnInit, computed, inject, input, signal } from '@angular/core';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzDropDownModule, NzContextMenuService, NzDropdownMenuComponent } from 'ng-zorro-antd/dropdown';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { NaznachStationRow, RearrTarget, RearrangeApiService } from './rearrange-api.service';

/**
 * Панель «Станции перестановок» (перенос gtport RearrangementStations):
 * колонки-корзины — по одной на включённый терминал + «По назначению».
 * Станцию (пару станций справочника naznach_station) можно ПЕРЕТАЩИТЬ в другую
 * колонку или сменить назначение через ПКМ — правка пишется в справочник сразу
 * (drag&drop с перезаписью в БД, как в gtport) и подхватывается Stage 2.
 */
@Component({
  selector: 'app-stations-panel',
  imports: [NzIconModule, NzDropDownModule],
  template: `
    <div class="panel">
      <div class="head">
        <b>Станции перестановок</b>
      </div>
      <p class="hint">Перетащите станцию в колонку или используйте правую кнопку мыши</p>

      <div class="cols">
        @for (col of columns(); track col.naznach) {
          <div class="col" [class.over]="dragOver() === col.naznach"
               (dragover)="$event.preventDefault(); dragOver.set(col.naznach)"
               (dragleave)="dragOver.set('')"
               (drop)="onDrop($event, col.naznach)">
            <div class="col-title">{{ col.title }} ({{ col.rows.length }})</div>
            @for (r of col.rows; track r.origin_station + r.dest_station) {
              <div class="st" draggable="true"
                   (dragstart)="onDragStart($event, r)"
                   (contextmenu)="openMenu($event, r, menu)">
                <span nz-icon nzType="environment" nzTheme="fill" class="pin"></span>
                <span class="st-name" [title]="'куда: ' + r.dest_station">{{ r.origin_station }}</span>
              </div>
            }
            @if (!col.rows.length) {
              <div class="empty">пусто</div>
            }
          </div>
        }
      </div>

      <nz-dropdown-menu #menu="nzDropdownMenu">
        <ul nz-menu>
          @for (t of targets(); track t.name) {
            @if (ctxRow()?.naznach !== t.name) {
              <li nz-menu-item (click)="setNaznach(t.name)">В {{ t.name }}</li>
            }
          }
          @if (ctxRow()?.naznach) {
            <li nz-menu-item (click)="setNaznach('')">По назначению</li>
          }
        </ul>
      </nz-dropdown-menu>
    </div>
  `,
  styles: [`
    :host { display: block; }
    /* Высота панели — по собственному контенту (не выравнивается под карточки портов). */
    .panel { background: var(--color-bg-surface); border-radius: var(--radius-card);
             box-shadow: var(--shadow-card); padding: var(--space-sm) var(--space-md) var(--space-md); }
    .head { display: flex; align-items: center; gap: 8px; }
    .spacer { flex: 1 1 auto; }
    .hint { color: var(--color-text-secondary); font-size: var(--font-size-sm); margin: 4px 0 8px; }
    /* Колонки-корзины: ширина строго по контенту — самое длинное название станции
       задаёт ширину колонки, панель в целом получает ширину от контента. */
    .cols { display: flex; gap: 6px; }
    .col { flex: 0 0 auto; min-width: 110px; border: 1px dashed var(--color-border, #d9d9d9);
           border-radius: 6px; padding: 4px; min-height: 160px; max-height: 70vh; overflow: auto; }
    .col.over { border-color: var(--color-primary, #1677ff); background: var(--color-primary-bg, #e6f4ff); }
    .col-title { font-size: var(--font-size-sm); font-weight: 600; border-bottom: 1px solid var(--color-border-light, #f0f0f0); padding: 2px 4px 4px; position: sticky; top: 0; background: var(--color-bg-surface, #fff); }
    .st { display: flex; align-items: center; gap: 4px; padding: 2px 4px; font-size: var(--font-size-sm); cursor: grab; border-radius: 4px; }
    .st:hover { background: var(--color-bg-container-secondary, #fafafa); }
    .pin { color: var(--color-primary, #1677ff); font-size: 12px; }
    /* Название станции — всегда в одну строку, ширину колонке задаёт контент. */
    .st-name { white-space: nowrap; }
    .empty { color: var(--color-text-secondary); font-size: var(--font-size-sm); text-align: center; padding: 12px 0; }
  `],
})
export class StationsPanelComponent implements OnInit {
  private readonly api = inject(RearrangeApiService);
  private readonly msg = inject(NzMessageService);
  private readonly ctxMenu = inject(NzContextMenuService);

  /** Цели-терминалы приходят от родителя (из ответа группировок). */
  readonly targets = input<RearrTarget[]>([]);

  readonly rows = signal<NaznachStationRow[]>([]);
  readonly dragOver = signal<string>('');
  readonly ctxRow = signal<NaznachStationRow | null>(null);
  private dragged: NaznachStationRow | null = null;

  /** Колонки: по терминалу на каждый + «По назначению» (пустой naznach). */
  readonly columns = computed(() => {
    const cols = this.targets().map((t) => ({
      title: t.name,
      naznach: t.name,
      rows: this.rows().filter((r) => r.naznach === t.name),
    }));
    cols.push({
      title: 'По назначению',
      naznach: '',
      rows: this.rows().filter((r) => !r.naznach || !this.targets().some((t) => t.name === r.naznach)),
    });
    return cols;
  });

  ngOnInit(): void {
    void this.load();
  }

  async load(): Promise<void> {
    try {
      this.rows.set(await this.api.getStations());
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    }
  }

  onDragStart(ev: DragEvent, r: NaznachStationRow): void {
    this.dragged = r;
    ev.dataTransfer?.setData('text/plain', r.origin_station);
  }

  async onDrop(ev: DragEvent, naznach: string): Promise<void> {
    ev.preventDefault();
    this.dragOver.set('');
    const r = this.dragged;
    this.dragged = null;
    if (!r || r.naznach === naznach) return;
    await this.save(r, naznach);
  }

  openMenu(ev: MouseEvent, r: NaznachStationRow, menu: NzDropdownMenuComponent): void {
    ev.preventDefault();
    this.ctxRow.set(r);
    this.ctxMenu.create(ev, menu);
  }

  async setNaznach(naznach: string): Promise<void> {
    const r = this.ctxRow();
    if (!r || r.naznach === naznach) return;
    await this.save(r, naznach);
  }

  private async save(r: NaznachStationRow, naznach: string): Promise<void> {
    try {
      await this.api.updateStationNaznach(r.dest_station, r.origin_station, naznach);
      this.msg.success(`${r.origin_station}: назначение — ${naznach || 'по назначению'}`);
      await this.load();
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    }
  }
}
