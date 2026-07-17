import { Component, OnInit, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzCardModule } from 'ng-zorro-antd/card';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzInputNumberModule } from 'ng-zorro-antd/input-number';
import { NzMessageService } from 'ng-zorro-antd/message';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzPopconfirmModule } from 'ng-zorro-antd/popconfirm';
import { NzSelectModule } from 'ng-zorro-antd/select';
import { NzSwitchModule } from 'ng-zorro-antd/switch';
import { NzTableModule } from 'ng-zorro-antd/table';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { apiErrorMessage } from '../../core/api/api-error';
import { DislocationApiService } from '../dislocation/dislocation-api.service';
import { AdminApiService, AdminColumn, AdminRow, AdminTable } from './admin-api.service';

/**
 * Страница «Админ»: универсальный редактор справочников (реестр list_tables,
 * перенос эталона gtport Dictionaries) + кнопка «Обновить справочники»
 * (перезагрузка словарей в RAM и пересчёт снимка — после правок).
 */
@Component({
  selector: 'app-admin',
  imports: [
    FormsModule, NzButtonModule, NzCardModule, NzIconModule, NzInputModule,
    NzInputNumberModule, NzModalModule, NzPopconfirmModule, NzSelectModule,
    NzSwitchModule, NzTableModule, NzTooltipModule,
  ],
  template: `
    <div class="page">
      <nz-card class="card">
        <div class="toolbar">
          <nz-select class="tsel" [ngModel]="selected()" (ngModelChange)="selectTable($event)"
                     nzPlaceHolder="Справочник">
            @for (t of tables(); track t.name) {
              <nz-option [nzValue]="t.name" [nzLabel]="t.name_ru" />
            }
          </nz-select>
          <input nz-input class="search" placeholder="Поиск по всем полям"
                 [ngModel]="search()" (ngModelChange)="search.set($event)" />
          <button nz-button nzType="primary" [disabled]="!selected()" (click)="openCreate()">
            <span nz-icon nzType="plus"></span> Добавить
          </button>
          <span class="spacer"></span>
          <button nz-button [nzLoading]="busyDict()" (click)="reloadDirectories()"
                  nz-tooltip nzTooltipTitle="Перезагрузить словари в память и пересчитать поля вагонов и прогнозы — жать после правок">
            <span nz-icon nzType="book"></span> Обновить справочники
          </button>
        </div>

        @if (selected()) {
          <nz-table #tbl [nzData]="filteredRows()" [nzLoading]="loading()"
                    nzSize="small" [nzPageSize]="20" [nzShowSizeChanger]="true"
                    [nzPageSizeOptions]="[10, 20, 50, 100]"
                    [nzScroll]="{ x: 'max-content', y: '62vh' }" class="grid">
            <thead>
              <tr>
                @for (c of visibleColumns(); track c.name) {
                  <th>{{ c.label || c.name }}</th>
                }
                <th nzRight nzWidth="120px"></th>
              </tr>
            </thead>
            <tbody>
              @for (row of tbl.data; track rowKey(row)) {
                <tr>
                  @for (c of visibleColumns(); track c.name) {
                    <td [class.num]="c.kind === 'number'">{{ cell(row, c) }}</td>
                  }
                  <td nzRight class="ops">
                    <button nz-button nzType="link" nzSize="small" nz-tooltip nzTooltipTitle="Копировать в новую строку"
                            (click)="openCopy(row)">
                      <span nz-icon nzType="copy"></span>
                    </button>
                    <button nz-button nzType="link" nzSize="small" (click)="openEdit(row)">
                      <span nz-icon nzType="edit"></span>
                    </button>
                    <button nz-button nzType="link" nzSize="small" nzDanger
                            nz-popconfirm nzPopconfirmTitle="Удалить строку?"
                            (nzOnConfirm)="remove(row)">
                      <span nz-icon nzType="delete"></span>
                    </button>
                  </td>
                </tr>
              }
            </tbody>
          </nz-table>
        } @else {
          <p class="muted">Выберите справочник.</p>
        }
      </nz-card>

      <nz-modal [nzVisible]="modalOpen()" [nzTitle]="modalTitle()"
                [nzOkLoading]="saving()" (nzOnOk)="save()" (nzOnCancel)="modalOpen.set(false)"
                nzOkText="Сохранить" nzCancelText="Отмена">
        <ng-container *nzModalContent>
          <div class="form">
            @for (c of formColumns(); track c.name) {
              <div class="frow">
                <label class="flabel" [class.req]="c.required">{{ c.label || c.name }}</label>
                @switch (c.kind) {
                  @case ('number') {
                    <nz-input-number class="fctl" [ngModel]="draftNum(c.name)"
                                     (ngModelChange)="setDraft(c.name, $event)" />
                  }
                  @case ('boolean') {
                    <nz-switch [ngModel]="draftBool(c.name)" (ngModelChange)="setDraft(c.name, $event)" />
                  }
                  @default {
                    <input nz-input class="fctl" [ngModel]="draftStr(c.name)"
                           (ngModelChange)="setDraft(c.name, $event)" />
                  }
                }
              </div>
            }
          </div>
        </ng-container>
      </nz-modal>
    </div>
  `,
  styles: [`
    .page { display: flex; flex-direction: column; gap: var(--space-md); }
    .card { border-radius: var(--radius-card); box-shadow: var(--shadow-card); }
    .toolbar { display: flex; align-items: center; gap: var(--space-sm); flex-wrap: wrap; margin-bottom: var(--space-md); }
    .tsel { min-width: 260px; }
    .search { max-width: 260px; }
    .spacer { flex: 1 1 auto; }
    .muted { color: var(--color-text-muted); }
    .grid { font-size: var(--font-size-sm); }
    /* Выравнивание по центру — просьба владельца (для всех справочников). */
    .grid th, .grid td { text-align: center; }
    .grid td.num { font-variant-numeric: tabular-nums; }
    .grid td.ops { white-space: nowrap; }
    .form { display: flex; flex-direction: column; gap: var(--space-sm); }
    .frow { display: flex; align-items: center; gap: var(--space-md); }
    .flabel { flex: 0 0 140px; text-align: right; color: var(--color-text-secondary); }
    .flabel.req::after { content: ' *'; color: var(--color-error, #ff4d4f); }
    .fctl { flex: 1 1 auto; }
  `],
})
export class AdminComponent implements OnInit {
  private readonly api = inject(AdminApiService);
  private readonly dislApi = inject(DislocationApiService);
  private readonly msg = inject(NzMessageService);

  readonly tables = signal<AdminTable[]>([]);
  readonly selected = signal<string>('');
  readonly pk = signal<string>('');
  readonly columns = signal<AdminColumn[]>([]);
  readonly rows = signal<AdminRow[]>([]);
  readonly loading = signal(false);
  readonly search = signal('');
  readonly busyDict = signal(false);

  readonly modalOpen = signal(false);
  readonly saving = signal(false);
  readonly editingId = signal<string | null>(null);
  readonly draft = signal<AdminRow>({});
  readonly copying = signal(false);

  readonly modalTitle = computed(() => {
    if (this.editingId() !== null) return 'Правка строки';
    return this.copying() ? 'Новая строка (копия)' : 'Новая строка';
  });

  /** Видимые колонки грида (служебные штампы скрыты). */
  readonly visibleColumns = computed(() => this.columns().filter((c) => !c.hidden));

  /** Колонки формы: ключ-serial и служебные не правятся руками. */
  readonly formColumns = computed(() => this.columns().filter((c) => !c.pk && !c.hidden));

  readonly filteredRows = computed(() => {
    const q = this.search().trim().toLowerCase();
    if (!q) return this.rows();
    return this.rows().filter((r) =>
      this.columns().some((c) => String(r[c.name] ?? '').toLowerCase().includes(q)),
    );
  });

  async ngOnInit(): Promise<void> {
    try {
      this.tables.set(await this.api.tables());
      if (this.tables().length) void this.selectTable(this.tables()[0].name);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    }
  }

  async selectTable(name: string): Promise<void> {
    this.selected.set(name);
    this.search.set('');
    this.loading.set(true);
    try {
      const data = await this.api.tableData(name);
      this.pk.set(data.table.pk);
      this.columns.set(data.columns);
      this.rows.set(data.rows ?? []);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
      this.columns.set([]);
      this.rows.set([]);
    } finally {
      this.loading.set(false);
    }
  }

  rowKey(row: AdminRow): string {
    return String(row[this.pk()] ?? '');
  }

  cell(row: AdminRow, c: AdminColumn): string {
    const v = row[c.name];
    if (v === null || v === undefined) return '';
    if (c.kind === 'boolean') return v ? 'да' : 'нет';
    return String(v);
  }

  openCreate(): void {
    this.editingId.set(null);
    this.copying.set(false);
    this.draft.set({});
    this.modalOpen.set(true);
  }

  /** Копия строки: форма новой записи с предзаполненными значениями (ключ сброшен) —
   *  удобно добавить отправителю ещё одну станцию, поправив одно поле. */
  openCopy(row: AdminRow): void {
    this.editingId.set(null);
    this.copying.set(true);
    const draft: AdminRow = {};
    for (const c of this.formColumns()) draft[c.name] = row[c.name];
    this.draft.set(draft);
    this.modalOpen.set(true);
  }

  openEdit(row: AdminRow): void {
    this.editingId.set(this.rowKey(row));
    this.copying.set(false);
    this.draft.set({ ...row });
    this.modalOpen.set(true);
  }

  draftStr(name: string): string {
    const v = this.draft()[name];
    return v === null || v === undefined ? '' : String(v);
  }
  draftNum(name: string): number | null {
    const v = this.draft()[name];
    return v === null || v === undefined || v === '' ? null : Number(v);
  }
  draftBool(name: string): boolean {
    return Boolean(this.draft()[name]);
  }
  setDraft(name: string, value: unknown): void {
    this.draft.update((d) => ({ ...d, [name]: value }));
  }

  async save(): Promise<void> {
    const table = this.selected();
    this.saving.set(true);
    try {
      const id = this.editingId();
      if (id === null) {
        await this.api.create(table, this.draft());
        this.msg.success('Строка добавлена');
      } else {
        await this.api.update(table, id, this.draft());
        this.msg.success('Строка сохранена');
      }
      this.modalOpen.set(false);
      await this.selectTable(table);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.saving.set(false);
    }
  }

  async remove(row: AdminRow): Promise<void> {
    try {
      await this.api.remove(this.selected(), this.rowKey(row));
      this.msg.success('Строка удалена');
      await this.selectTable(this.selected());
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    }
  }

  /** Применить правки: перезагрузка словарей в RAM + пересчёт снимка (dict_reload). */
  async reloadDirectories(): Promise<void> {
    this.busyDict.set(true);
    try {
      const res = await this.dislApi.reloadDirectories();
      const parts = [`обновлено ${res.refreshed}`, `заполнено ${res.filled + res.filled_by_train}`];
      if (res.still_empty) parts.push(`без атрибуции ${res.still_empty}`);
      this.msg.success(`Справочники перезагружены, снимок пересчитан: ${parts.join(', ')} из ${res.count} ваг`);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.busyDict.set(false);
    }
  }
}
