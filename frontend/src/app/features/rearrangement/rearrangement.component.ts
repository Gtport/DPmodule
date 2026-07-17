import { Component, OnInit, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { NzTabsModule } from 'ng-zorro-antd/tabs';
import { NzRadioModule } from 'ng-zorro-antd/radio';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { NzCheckboxModule } from 'ng-zorro-antd/checkbox';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { RearrangeApiService, RearrGroup, RearrSubGroup } from './rearrange-api.service';

/**
 * Экран «Перестановки/Переадресация» (перенос gtport Rearrangement).
 *
 * Перестановки: вагоны с «управляемых» станций погрузки (пары станций из
 * справочника naznach_station) распределяются по терминалам — выбор группы/
 * подгруппы/вагонов, целевой терминал, «Переставить». Переадресация: увод
 * поезда целиком на свой терминал / внешний порт / отмена. Обе операции —
 * батчем: одна кнопка = один пересбор снимка (Stage 3–4) и одна запись журнала.
 */
@Component({
  selector: 'app-rearrangement',
  imports: [
    FormsModule, NzTabsModule, NzRadioModule, NzButtonModule, NzIconModule,
    NzTagModule, NzCheckboxModule, NzModalModule, NzInputModule,
  ],
  template: `
    <div class="page">
      <nz-tabs [nzSelectedIndex]="tab" (nzSelectedIndexChange)="tab = $event; onTabChange()">
        <nz-tab nzTitle="Перестановки"></nz-tab>
        <nz-tab nzTitle="Переадресация"></nz-tab>
      </nz-tabs>

      <div class="bar">
        @if (tab === 0) {
          <nz-radio-group [ngModel]="groupBy()" (ngModelChange)="setGroupBy($event)" nzButtonStyle="solid" nzSize="small">
            <label nz-radio-button nzValue="parent_index">По родительскому индексу</label>
            <label nz-radio-button nzValue="collective_train">По сборному поезду</label>
          </nz-radio-group>
        } @else {
          <span class="hint-inline">Доступны к переадресации только «чистые» поезда (зелёные)</span>
        }
        <span class="spacer"></span>
        <span class="count">групп: {{ groups().length }} · выбрано вагонов: {{ selected().size }}</span>
        <button nz-button nzSize="small" (click)="load()">
          <span nz-icon nzType="reload"></span>
        </button>
      </div>

      <!-- Панель действий -->
      <div class="actions">
        @if (tab === 0) {
          <span class="lbl">Переставить на:</span>
          <nz-radio-group [(ngModel)]="target" nzSize="small">
            @for (t of targets(); track t) {
              <label nz-radio [nzValue]="t">{{ t }}</label>
            }
          </nz-radio-group>
          <button nz-button nzType="primary" nzSize="small"
                  [disabled]="!selected().size || !target() || applying()"
                  (click)="applyRearrange()">
            Переставить ({{ selected().size }})
          </button>
        } @else {
          <span class="lbl">Переадресовать:</span>
          @for (t of targets(); track t) {
            <button nz-button nzSize="small" [disabled]="!selected().size || applying()"
                    (click)="applyRedirect('own', t)">{{ t }}</button>
          }
          <button nz-button nzSize="small" [disabled]="!selected().size || applying()"
                  (click)="portDialog.set(true)">
            Внешний порт…
          </button>
          <button nz-button nzDanger nzSize="small" [disabled]="!selected().size || applying()"
                  (click)="applyRedirect('cancel', '')">
            Отменить переадресацию
          </button>
        }
      </div>

      <!-- Дерево групп -->
      @if (loading()) {
        <p class="hint">Загрузка…</p>
      } @else if (!groups().length) {
        <p class="hint">Нет данных для этой вкладки.</p>
      }
      @for (g of groups(); track g.key) {
        <div class="group" [class.avail]="tab === 1 && g.available" [class.blocked]="tab === 1 && !g.available">
          <div class="g-head">
            <label nz-checkbox
                   [nzChecked]="groupChecked(g)"
                   [nzIndeterminate]="groupIndeterminate(g)"
                   [nzDisabled]="tab === 1 && !g.available"
                   (nzCheckedChange)="toggleGroup(g, $event)"></label>
            <span class="g-toggle" (click)="toggleExpand(g.key)">
              <span nz-icon [nzType]="expanded().has(g.key) ? 'down' : 'right'"></span>
              <b>{{ g.title }}</b>
              @if (g.subtitle) { <span class="sub">{{ g.subtitle }}</span> }
              @if (g.naznach) { <nz-tag class="term">{{ g.naznach }}</nz-tag> }
            </span>
            <span class="spacer"></span>
            @if (tab === 1) {
              <nz-tag [nzColor]="g.available ? 'green' : 'default'">
                {{ g.available ? 'доступен' : 'недоступен' }}
              </nz-tag>
            }
            <span class="cnt">{{ g.vagon_count }} ваг.</span>
          </div>

          @if (expanded().has(g.key)) {
            @for (sg of g.sub_groups; track sg.key) {
              <div class="sg">
                <div class="sg-head">
                  <label nz-checkbox
                         [nzChecked]="subChecked(sg)"
                         [nzIndeterminate]="subIndeterminate(sg)"
                         [nzDisabled]="tab === 1 && !g.available"
                         (nzCheckedChange)="toggleSub(sg, $event)"></label>
                  <span class="g-toggle" (click)="toggleExpand(g.key + '::' + sg.key)">
                    <span nz-icon [nzType]="expanded().has(g.key + '::' + sg.key) ? 'down' : 'right'"></span>
                    {{ sg.label }}
                  </span>
                  <span class="spacer"></span>
                  <span class="cnt">{{ sg.vagon_count }} ваг.</span>
                </div>
                @if (expanded().has(g.key + '::' + sg.key)) {
                  <div class="vagons">
                    @for (v of sg.vagons; track v.id) {
                      <span class="chip" [class.sel]="selected().has(v.id)"
                            (click)="tab === 1 && !g.available ? null : toggleVagon(v.id)"
                            [title]="'накладная ' + (v.invoice || '—')">
                        {{ v.npp_vag ?? '·' }} | {{ v.vagon }}
                      </span>
                    }
                  </div>
                }
              </div>
            }
          }
        </div>
      }

      <!-- Диалог внешнего порта -->
      <nz-modal [nzVisible]="portDialog()" nzTitle="Переадресация на внешний порт"
                (nzOnCancel)="portDialog.set(false)" (nzOnOk)="applyExternal()"
                nzOkText="Переадресовать" [nzOkDisabled]="!portName().trim()">
        <ng-container *nzModalContent>
          <p>Вагонов выбрано: {{ selected().size }}. Назначение станет «ВП», имя порта сохранится в карточке вагона.</p>
          <input nz-input placeholder="Название порта" [ngModel]="portName()" (ngModelChange)="portName.set($event)" />
        </ng-container>
      </nz-modal>
    </div>
  `,
  styles: [`
    .page { display: flex; flex-direction: column; gap: var(--space-sm); }
    .bar, .actions { display: flex; align-items: center; gap: var(--space-sm); flex-wrap: wrap; }
    .actions { padding: 6px 8px; background: var(--color-bg-container-secondary, #fafafa); border-radius: 6px; }
    .lbl { color: var(--color-text-secondary); font-size: var(--font-size-sm); }
    .spacer { flex: 1 1 auto; }
    .count, .cnt { color: var(--color-text-secondary); font-size: var(--font-size-sm); white-space: nowrap; }
    .hint, .hint-inline { color: var(--color-text-secondary); font-size: var(--font-size-sm); margin: 0; }
    .group { border: 1px solid var(--color-border, #f0f0f0); border-radius: 6px; }
    .group.avail { border-color: var(--color-success-border, #b7eb8f); }
    .group.blocked { opacity: .65; }
    .g-head, .sg-head { display: flex; align-items: center; gap: 8px; padding: 6px 10px; }
    .g-toggle { cursor: pointer; display: inline-flex; align-items: center; gap: 6px; min-width: 0; }
    .sub { color: var(--color-text-secondary); font-size: var(--font-size-sm); }
    .term { margin: 0; }
    .sg { border-top: 1px dashed var(--color-border, #f0f0f0); margin-left: 24px; }
    .vagons { display: flex; flex-wrap: wrap; gap: 4px; padding: 4px 10px 8px 34px; }
    .chip { font-size: var(--font-size-sm); font-variant-numeric: tabular-nums; padding: 1px 6px;
            border: 1px solid var(--color-border, #d9d9d9); border-radius: 4px; cursor: pointer; }
    .chip.sel { background: var(--color-primary, #1677ff); color: #fff; border-color: var(--color-primary, #1677ff); }
  `],
})
export class RearrangementComponent implements OnInit {
  private readonly api = inject(RearrangeApiService);
  /** Уведомления — только тостами (договорённость проекта). */
  private readonly msg = inject(NzMessageService);

  tab = 0;
  readonly groups = signal<RearrGroup[]>([]);
  readonly targets = signal<string[]>([]);
  readonly loading = signal(false);
  readonly applying = signal(false);
  readonly groupBy = signal<'parent_index' | 'collective_train'>('parent_index');
  readonly selected = signal<Set<string>>(new Set());
  readonly expanded = signal<Set<string>>(new Set());
  readonly target = signal<string>('');
  readonly portDialog = signal(false);
  readonly portName = signal('');

  ngOnInit(): void {
    void this.load();
  }

  setGroupBy(v: 'parent_index' | 'collective_train'): void {
    this.groupBy.set(v);
    void this.load();
  }

  async load(): Promise<void> {
    this.loading.set(true);
    this.selected.set(new Set());
    try {
      const data = this.tab === 0
        ? await this.api.getRearrangementGroups(this.groupBy())
        : await this.api.getRedirectionGroups();
      this.groups.set(data.groups ?? []);
      this.targets.set(data.targets ?? []);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.loading.set(false);
    }
  }

  onTabChange(): void {
    this.expanded.set(new Set());
    void this.load();
  }

  // ── Выбор ──────────────────────────────────────────────────────────────
  private groupIds(g: RearrGroup): string[] {
    return g.sub_groups.flatMap((sg) => sg.vagons.map((v) => v.id));
  }

  groupChecked(g: RearrGroup): boolean {
    const ids = this.groupIds(g);
    return ids.length > 0 && ids.every((id) => this.selected().has(id));
  }
  groupIndeterminate(g: RearrGroup): boolean {
    const ids = this.groupIds(g);
    const n = ids.filter((id) => this.selected().has(id)).length;
    return n > 0 && n < ids.length;
  }
  toggleGroup(g: RearrGroup, checked: boolean): void {
    const next = new Set(this.selected());
    for (const id of this.groupIds(g)) checked ? next.add(id) : next.delete(id);
    this.selected.set(next);
  }

  subChecked(sg: RearrSubGroup): boolean {
    return sg.vagons.length > 0 && sg.vagons.every((v) => this.selected().has(v.id));
  }
  subIndeterminate(sg: RearrSubGroup): boolean {
    const n = sg.vagons.filter((v) => this.selected().has(v.id)).length;
    return n > 0 && n < sg.vagons.length;
  }
  toggleSub(sg: RearrSubGroup, checked: boolean): void {
    const next = new Set(this.selected());
    for (const v of sg.vagons) checked ? next.add(v.id) : next.delete(v.id);
    this.selected.set(next);
  }

  toggleVagon(id: string): void {
    const next = new Set(this.selected());
    next.has(id) ? next.delete(id) : next.add(id);
    this.selected.set(next);
  }

  toggleExpand(key: string): void {
    const next = new Set(this.expanded());
    next.has(key) ? next.delete(key) : next.add(key);
    this.expanded.set(next);
  }

  // ── Применение ─────────────────────────────────────────────────────────
  async applyRearrange(): Promise<void> {
    this.applying.set(true);
    try {
      const res = await this.api.applyRearrangement([...this.selected()], this.target());
      this.msg.success(`Переставлено вагонов: ${res.updated}. Ход и прогноз пересчитаны.`);
      await this.load();
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.applying.set(false);
    }
  }

  async applyRedirect(kind: 'own' | 'ext' | 'cancel', target: string): Promise<void> {
    this.applying.set(true);
    try {
      const res = await this.api.applyRedirection([...this.selected()], kind, target);
      const what = kind === 'cancel' ? 'Отменена переадресация' : 'Переадресовано';
      this.msg.success(`${what}: ${res.updated} ваг. Ход и прогноз пересчитаны.`);
      this.portDialog.set(false);
      this.portName.set('');
      await this.load();
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.applying.set(false);
    }
  }

  applyExternal(): void {
    void this.applyRedirect('ext', this.portName().trim());
  }
}
