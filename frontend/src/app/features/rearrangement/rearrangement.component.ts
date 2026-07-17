import { Component, OnInit, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { NzTabsModule } from 'ng-zorro-antd/tabs';
import { NzRadioModule } from 'ng-zorro-antd/radio';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzSelectModule } from 'ng-zorro-antd/select';
import { NzSwitchModule } from 'ng-zorro-antd/switch';
import { NzDropDownModule, NzContextMenuService, NzDropdownMenuComponent } from 'ng-zorro-antd/dropdown';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import {
  RearrGroup, RearrGroups, RearrSubGroup, RearrTarget, RearrangeApiService,
} from './rearrange-api.service';
import { GroupTreeComponent, TreeContextEvent } from './group-tree.component';
import { StationsPanelComponent } from './stations-panel.component';

/** Строка сводной колонки перестановок: чужой груз, переставленный на терминал. */
interface SummaryRow {
  id: string;
  index_main: string;
  station_nach: string;
  station_oper: string;
  gruzpol_s: string;
  naznach: string;
  stan_nazn: string;
  stan_nazn_code: string;
  rasst: number | null;
  status: number | null;
  vagon_count: number;
  vagon_ids: string[];
}

/** Карточка-сводка переадресации: направление и его поезда. */
interface RedirectCard {
  title: string;
  trains: { index_main: string; count: number }[];
}

/**
 * Экран «Перестановки/Переадресация» (перенос gtport, стиль оригинала).
 *
 * Перестановки: сводные колонки «чужой груз на терминале» (сортировка по
 * расстоянию, тумблер «прибывшие», ПКМ «По назначению»), диалог «Управление
 * назначениями вагонов» (дерево + ПКМ) и панель станций с drag&drop.
 * Переадресация: карточки-сводки направлений, управление с контекстными целями
 * (терминалы ДРУГИХ станций + внешний порт) и дерево «Отправительские маршруты».
 * Правило целей: терминал «сам на себя» не предлагается; «По назначению» —
 * только когда грузополучатель ≠ назначению.
 */
@Component({
  selector: 'app-rearrangement',
  imports: [
    FormsModule, NzTabsModule, NzRadioModule, NzButtonModule, NzIconModule, NzTagModule,
    NzModalModule, NzInputModule, NzSelectModule, NzSwitchModule, NzDropDownModule,
    GroupTreeComponent, StationsPanelComponent,
  ],
  template: `
    <div class="page">
      <nz-tabs [nzSelectedIndex]="tab()" (nzSelectedIndexChange)="switchTab($event)">
        <nz-tab nzTitle="Перестановки"></nz-tab>
        <nz-tab nzTitle="Переадресация"></nz-tab>
      </nz-tabs>

      <!-- ══════════ Вкладка «Перестановки» ══════════ -->
      @if (tab() === 0) {
        <div class="split">
          <div class="left">
            <div class="bar">
              <button nz-button nzType="primary" nzSize="small" (click)="dialogOpen.set(true)">
                <span nz-icon nzType="swap"></span> Управление назначениями
              </button>
              <span class="spacer"></span>
              <span class="mut">прибывшие</span>
              <nz-switch nzSize="small" [ngModel]="showArrived()" (ngModelChange)="showArrived.set($event)"></nz-switch>
              <button nz-button nzSize="small" (click)="load()">
                <span nz-icon nzType="reload"></span>
              </button>
            </div>

            <div class="sum-cols">
              @for (col of summaryColumns(); track col.terminal) {
                <div class="sum-col">
                  <div class="col-title">{{ col.terminal }} ({{ col.rows.length }})</div>
                  @for (r of col.rows; track r.id) {
                    <div class="sum-row" [class.arrived]="r.status === 10"
                         (contextmenu)="openRearrMenu($event, r, rearrMenu)">
                      <span nz-icon nzType="train" nzTheme="fill" class="tiny"
                            [style.color]="statusColor(r.status)"></span>
                      <span class="idx">{{ r.index_main || '—' }}</span>
                      <span class="mut ell" [title]="r.station_nach">{{ r.station_nach }}</span>
                      <span class="mut">({{ r.vagon_count }})</span>
                      <span class="spacer"></span>
                      <span class="mut ell" [title]="'станция операции'">{{ r.station_oper }}</span>
                      @if (r.rasst != null) { <span class="mut">{{ r.rasst }} км</span> }
                      @if (r.status === 10) { <span class="ok">прибыл</span> }
                    </div>
                  }
                  @if (!col.rows.length) { <div class="empty">Нет чужого груза</div> }
                </div>
              }
            </div>
            <p class="hint">Показан чужой груз, переставленный на терминал (грузополучатель ≠ назначению).
              ПКМ по строке — «По назначению» и перестановка на другой терминал станции.</p>
          </div>

          <app-stations-panel [targets]="targets()"></app-stations-panel>
        </div>
      }

      <!-- ══════════ Вкладка «Переадресация» ══════════ -->
      @if (tab() === 1) {
        <div class="cards-head">
          <b>Поезда к переадресации</b>
          <span class="spacer"></span>
          <button nz-button nzSize="small" (click)="load()"><span nz-icon nzType="reload"></span></button>
        </div>
        <div class="cards">
          @for (c of redirectCards(); track c.title) {
            <div class="card">
              <div class="card-head">{{ c.title }} <nz-tag class="cnt-tag">{{ c.trains.length }}</nz-tag></div>
              @for (t of c.trains; track t.index_main) {
                <div class="card-train" (click)="search.set(t.index_main)">
                  {{ t.index_main }} <span class="mut">({{ t.count }})</span>
                </div>
              }
              @if (!c.trains.length) { <div class="empty">Нет поездов</div> }
            </div>
          }
        </div>

        <div class="bar manage">
          <b>Управление переадресацией</b>
          <input nz-input nzSize="small" class="search" placeholder="Станция или индекс"
                 [ngModel]="search()" (ngModelChange)="search.set($event)" />
          <nz-select nzSize="small" class="sel" nzAllowClear nzPlaceHolder="Станция назначения"
                     [ngModel]="stanFilter()" (ngModelChange)="stanFilter.set($event ?? '')">
            @for (s of stanNazns(); track s) { <nz-option [nzValue]="s" [nzLabel]="s"></nz-option> }
          </nz-select>
          <nz-select nzSize="small" class="sel" nzPlaceHolder="Назначение"
                     [ngModel]="redirectChoice()" (ngModelChange)="redirectChoice.set($event)">
            @for (o of redirectOptions(); track o.value) {
              <nz-option [nzValue]="o.value" [nzLabel]="o.label"></nz-option>
            }
          </nz-select>
          <button nz-button nzType="primary" nzSize="small"
                  [disabled]="!selected().size || !redirectChoice() || applying()"
                  (click)="applyRedirectChoice()">
            <span nz-icon nzType="check"></span> Применить ({{ selected().size }})
          </button>
        </div>

        <div class="routes-head" (click)="routesOpen.set(!routesOpen())">
          <span nz-icon [nzType]="routesOpen() ? 'down' : 'right'"></span>
          <b>Отправительские маршруты</b>
          <span class="spacer"></span>
          <span class="mut">групп: {{ filteredGroups().length }} · выбрано: {{ selected().size }}</span>
        </div>
        @if (routesOpen()) {
          <app-group-tree [groups]="filteredGroups()" [disableUnavailable]="true"
                          [(selected)]="selected" (ctx)="openRedirectMenu($event, redirectMenu)" />
        }
      }

      <!-- ══════════ Диалог «Управление назначениями вагонов» ══════════ -->
      <nz-modal [nzVisible]="dialogOpen()" nzTitle="Управление назначениями вагонов" nzWidth="1100px"
                (nzOnCancel)="dialogOpen.set(false)" [nzFooter]="null">
        <ng-container *nzModalContent>
          <div class="bar">
            <nz-select nzSize="small" class="sel-wide" [ngModel]="groupBy()" (ngModelChange)="setGroupBy($event)">
              <nz-option nzValue="parent_index" nzLabel="Родительский индекс"></nz-option>
              <nz-option nzValue="collective_train" nzLabel="Сборный поезд"></nz-option>
            </nz-select>
            <input nz-input nzSize="small" class="search" placeholder="Станция или индекс"
                   [ngModel]="search()" (ngModelChange)="search.set($event)" />
            <nz-select nzSize="small" class="sel" nzPlaceHolder="Назначение"
                       [ngModel]="rearrChoice()" (ngModelChange)="rearrChoice.set($event)">
              @for (t of rearrOptionsForSelection(); track t) {
                <nz-option [nzValue]="t" [nzLabel]="t"></nz-option>
              }
            </nz-select>
            <button nz-button nzType="primary" nzSize="small"
                    [disabled]="!selected().size || !rearrChoice() || applying()"
                    (click)="applyRearrange(rearrChoice(), [])">
              <span nz-icon nzType="sync"></span> Применить ({{ selected().size }})
            </button>
            <span class="spacer"></span>
            <button nz-button nzSize="small" (click)="load()"><span nz-icon nzType="reload"></span></button>
          </div>
          <div class="tree-wrap">
            <app-group-tree [groups]="filteredGroups()" [(selected)]="selected"
                            (ctx)="openRearrTreeMenu($event, rearrMenu)" />
            @if (!filteredGroups().length && !loading()) { <div class="empty">Нет данных</div> }
          </div>
        </ng-container>
      </nz-modal>

      <!-- Меню ПКМ: перестановки (цели той же станции, правило «не сам на себя») -->
      <nz-dropdown-menu #rearrMenu="nzDropdownMenu">
        <ul nz-menu>
          @for (t of ctxRearrTargets(); track t) {
            <li nz-menu-item (click)="applyRearrange(t, ctxIds())">В {{ t }}</li>
          }
          @if (ctxCanReturn()) {
            <li nz-menu-item (click)="applyByGruzpol(ctxIds())">По назначению</li>
          }
          @if (!ctxRearrTargets().length && !ctxCanReturn()) {
            <li nz-menu-item nzDisabled>Уже на своём назначении</li>
          }
        </ul>
      </nz-dropdown-menu>

      <!-- Меню ПКМ: переадресация (терминалы других станций + ВП + отмена) -->
      <nz-dropdown-menu #redirectMenu="nzDropdownMenu">
        <ul nz-menu>
          @for (t of ctxRedirectTargets(); track t.name) {
            <li nz-menu-item (click)="applyRedirect('own', t.name, ctxIds())">В {{ t.name }} ({{ t.station }})</li>
          }
          <li nz-menu-item (click)="portDialog.set(true)">Внешний порт…</li>
          @if (ctxCanReturn()) {
            <li nz-menu-item nzDanger (click)="applyRedirect('cancel', '', ctxIds())">Отменить переадресацию</li>
          }
        </ul>
      </nz-dropdown-menu>

      <!-- Диалог внешнего порта -->
      <nz-modal [nzVisible]="portDialog()" nzTitle="Внешний порт"
                (nzOnCancel)="portDialog.set(false)" (nzOnOk)="applyExternal()"
                nzOkText="Применить" [nzOkDisabled]="!portName().trim()">
        <ng-container *nzModalContent>
          <p>Вагонов выбрано: {{ ctxIds().length || selected().size }}. Назначение станет «ВП»,
             имя порта сохранится у вагона и в истории рейса.</p>
          <input nz-input placeholder="Например: Владивосток, Ванино, Восточный"
                 [ngModel]="portName()" (ngModelChange)="portName.set($event)" />
        </ng-container>
      </nz-modal>
    </div>
  `,
  styles: [`
    .page { display: flex; flex-direction: column; gap: var(--space-sm); }
    .bar { display: flex; align-items: center; gap: var(--space-sm); flex-wrap: wrap; }
    .bar.manage { padding: 6px 8px; background: var(--color-bg-container-secondary, #fafafa); border-radius: 6px; }
    .spacer { flex: 1 1 auto; }
    .mut { color: var(--color-text-secondary); font-size: var(--font-size-sm); }
    .hint { color: var(--color-text-secondary); font-size: var(--font-size-sm); margin: 0; }
    .empty { color: var(--color-text-secondary); font-size: var(--font-size-sm); text-align: center; padding: 14px 0; }
    .search { width: 180px; }
    .sel { width: 190px; }
    .sel-wide { width: 210px; }
    .split { display: grid; grid-template-columns: 1fr auto; gap: var(--space-sm); align-items: start; }
    .left { display: flex; flex-direction: column; gap: var(--space-sm); min-width: 0; }
    /* Сводные колонки перестановок (стиль gtport: узкие колонки по терминалам) */
    .sum-cols { display: grid; grid-template-columns: repeat(auto-fit, minmax(340px, 1fr)); gap: var(--space-sm); }
    .sum-col { border: 1px solid var(--color-border, #f0f0f0); border-radius: 6px; padding: 4px; max-height: 500px; overflow: auto; }
    .col-title { font-size: var(--font-size-sm); font-weight: 600; border-bottom: 1.5px solid var(--color-border, #f0f0f0);
                 padding: 2px 6px 4px; position: sticky; top: 0; background: var(--color-bg-container, #fff); z-index: 1; }
    .sum-row { display: flex; align-items: center; gap: 6px; padding: 3px 6px; font-size: var(--font-size-sm);
               border-radius: 4px; cursor: context-menu; }
    .sum-row:hover { background: var(--color-bg-container-secondary, #fafafa); }
    .sum-row.arrived { background: var(--color-success-bg, #f6ffed); }
    .idx { font-variant-numeric: tabular-nums; font-weight: 500; white-space: nowrap; }
    .ell { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .tiny { font-size: 13px; }
    .ok { color: var(--color-success, #52c41a); font-size: var(--font-size-sm); }
    /* Карточки переадресации */
    .cards-head { display: flex; align-items: center; gap: 8px; }
    .cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); gap: var(--space-sm); }
    .card { border: 1px solid var(--color-border, #f0f0f0); border-radius: 6px; padding: 6px; min-height: 90px; }
    .card-head { display: flex; align-items: center; justify-content: space-between; font-weight: 600;
                 border-bottom: 1px solid var(--color-border, #f0f0f0); padding-bottom: 4px; margin-bottom: 4px; }
    .cnt-tag { margin: 0; border-radius: 10px; }
    .card-train { padding: 2px 6px; font-size: var(--font-size-sm); font-variant-numeric: tabular-nums;
                  cursor: pointer; border-radius: 4px; }
    .card-train:hover { background: var(--color-primary-bg, #e6f4ff); }
    .routes-head { display: flex; align-items: center; gap: 8px; padding: 8px;
                   background: var(--color-bg-container-secondary, #fafafa); border-radius: 6px; cursor: pointer; }
    .tree-wrap { max-height: 60vh; overflow: auto; border: 1px solid var(--color-border, #f0f0f0); border-radius: 6px; }
  `],
})
export class RearrangementComponent implements OnInit {
  private readonly api = inject(RearrangeApiService);
  private readonly msg = inject(NzMessageService);
  private readonly ctxMenu = inject(NzContextMenuService);

  readonly tab = signal(0);
  readonly loading = signal(false);
  readonly applying = signal(false);
  readonly groups = signal<RearrGroup[]>([]);
  readonly targets = signal<RearrTarget[]>([]);
  readonly groupBy = signal<'parent_index' | 'collective_train'>('parent_index');
  readonly selected = signal<Set<string>>(new Set());
  readonly search = signal('');
  readonly stanFilter = signal('');
  readonly showArrived = signal(false);
  readonly dialogOpen = signal(false);
  readonly routesOpen = signal(true);
  readonly portDialog = signal(false);
  readonly portName = signal('');
  readonly rearrChoice = signal('');
  readonly redirectChoice = signal('');

  /** Контекст ПКМ: вагоны и опорные атрибуты элемента под курсором. */
  readonly ctxIds = signal<string[]>([]);
  readonly ctxStanCode = signal('');
  readonly ctxNaznach = signal('');
  readonly ctxHasForeign = signal(false); // в выделении есть вагоны с gruzpol_s ≠ naznach

  ngOnInit(): void {
    void this.load();
  }

  switchTab(i: number): void {
    this.tab.set(i);
    this.selected.set(new Set());
    this.search.set('');
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
      const data: RearrGroups = this.tab() === 0
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

  // ── Производные ────────────────────────────────────────────────────────
  readonly filteredGroups = computed(() => {
    const q = this.search().trim().toUpperCase();
    const st = this.stanFilter();
    return this.groups().filter((g) => {
      if (st && g.stan_nazn !== st) return false;
      if (!q) return true;
      const hay = [g.index_main, g.index, g.station_nach, g.station_oper, g.stan_nazn, g.pereadr_port]
        .filter(Boolean).join(' ').toUpperCase();
      return hay.includes(q);
    });
  });

  readonly stanNazns = computed(() => [...new Set(this.groups().map((g) => g.stan_nazn).filter(Boolean))].sort());

  /** Сводные колонки перестановок: чужой груз (gruzpol_s ≠ naznach = терминал колонки). */
  readonly summaryColumns = computed(() => {
    const rows: SummaryRow[] = [];
    for (const g of this.groups()) {
      for (const sg of g.sub_groups) {
        rows.push({
          id: `${g.key}::${sg.key}`,
          index_main: g.index_main || sg.index_main || '',
          station_nach: g.station_nach || sg.station_nach || '',
          station_oper: sg.station_oper,
          gruzpol_s: sg.gruzpol_s,
          naznach: sg.naznach,
          stan_nazn: g.stan_nazn,
          stan_nazn_code: g.stan_nazn_code,
          rasst: sg.rasst_stan_nazn,
          status: sg.status,
          vagon_count: sg.vagon_count,
          vagon_ids: sg.vagons.map((v) => v.id),
        });
      }
    }
    const visible = this.showArrived() ? rows : rows.filter((r) => r.status !== 10);
    visible.sort((a, b) => (a.rasst ?? 1e9) - (b.rasst ?? 1e9));
    return this.targets().map((t) => ({
      terminal: t.name,
      rows: visible.filter((r) => r.naznach === t.name && r.gruzpol_s !== t.name),
    }));
  });

  /** Карточки переадресации: направления «С <станции> на <станцию терминала>» + ВП. */
  readonly redirectCards = computed((): RedirectCard[] => {
    const stationOf = new Map(this.targets().map((t) => [t.name, t.station]));
    const codeOf = new Map(this.targets().map((t) => [t.name, t.station_code]));
    const buckets = new Map<string, Map<string, number>>();
    const add = (title: string, g: RearrGroup) => {
      const m = buckets.get(title) ?? new Map<string, number>();
      const key = g.index_main || '—';
      m.set(key, (m.get(key) ?? 0) + g.vagon_count);
      buckets.set(title, m);
    };
    for (const g of this.groups()) {
      if (g.pereadr_port) {
        add('Внешний порт', g);
        continue;
      }
      const termCode = g.naznach ? codeOf.get(g.naznach) : undefined;
      if (termCode && g.stan_nazn_code && termCode !== g.stan_nazn_code) {
        add(`С ${g.stan_nazn} на ${stationOf.get(g.naznach!) ?? ''}`, g);
      }
    }
    const cards: RedirectCard[] = [...buckets.entries()].map(([title, m]) => ({
      title,
      trains: [...m.entries()].map(([index_main, count]) => ({ index_main, count })).sort((a, b) => a.index_main.localeCompare(b.index_main)),
    }));
    if (!cards.some((c) => c.title === 'Внешний порт')) cards.push({ title: 'Внешний порт', trains: [] });
    cards.sort((a, b) => a.title.localeCompare(b.title));
    return cards;
  });

  /** Цели перестановки для текущего выделения: терминалы ТОЙ ЖЕ станции (по коду). */
  readonly rearrOptionsForSelection = computed(() => {
    const code = this.selectionStanCode();
    return this.targets()
      .filter((t) => !code || t.station_code === code)
      .map((t) => t.name);
  });

  /** Опции переадресации для выделения: терминалы ДРУГИХ станций + ВП + отмена. */
  readonly redirectOptions = computed(() => {
    const code = this.selectionStanCode();
    const opts = this.targets()
      .filter((t) => !code || (t.station_code && t.station_code !== code))
      .map((t) => ({ value: t.name, label: `${t.name} (${t.station})` }));
    opts.push({ value: 'ВП', label: 'Внешний порт' });
    opts.push({ value: 'ОТМЕНА', label: 'Отменить переадресацию' });
    return opts;
  });

  /** Цели ПКМ перестановок: та же станция (по коду), не «сам на себя». */
  readonly ctxRearrTargets = computed(() =>
    this.targets()
      .filter((t) => (!this.ctxStanCode() || t.station_code === this.ctxStanCode()) && t.name !== this.ctxNaznach())
      .map((t) => t.name),
  );

  /** Цели ПКМ переадресации: другие станции (по коду), не «сам на себя». */
  readonly ctxRedirectTargets = computed(() =>
    this.targets().filter(
      (t) => t.station_code && t.station_code !== this.ctxStanCode() && t.name !== this.ctxNaznach(),
    ),
  );

  /** «По назначению»: есть кому возвращаться (хоть один вагон с gruzpol_s ≠ naznach). */
  readonly ctxCanReturn = computed(() => this.ctxHasForeign());

  /** Код станции назначения первой выбранной группы (для опций select). */
  private selectionStanCode(): string {
    const sel = this.selected();
    if (!sel.size) return '';
    for (const g of this.groups()) {
      for (const sg of g.sub_groups) {
        if (sg.vagons.some((v) => sel.has(v.id))) return g.stan_nazn_code;
      }
    }
    return '';
  }

  statusColor(status: number | null): string {
    switch (status) {
      case 5: return '#ff4d4f';
      case 10: return '#52c41a';
      default: return '#1890ff';
    }
  }

  // ── ПКМ ────────────────────────────────────────────────────────────────
  openRearrMenu(ev: MouseEvent, r: SummaryRow, menu: NzDropdownMenuComponent): void {
    ev.preventDefault();
    this.ctxIds.set(r.vagon_ids);
    this.ctxStanCode.set(r.stan_nazn_code);
    this.ctxNaznach.set(r.naznach);
    this.ctxHasForeign.set(!!r.gruzpol_s && r.gruzpol_s !== r.naznach);
    this.ctxMenu.create(ev, menu);
  }

  openRearrTreeMenu(e: TreeContextEvent, menu: NzDropdownMenuComponent): void {
    this.setTreeCtx(e);
    this.ctxMenu.create(e.event, menu);
  }

  openRedirectMenu(e: TreeContextEvent, menu: NzDropdownMenuComponent): void {
    this.setTreeCtx(e);
    this.ctxMenu.create(e.event, menu);
  }

  private setTreeCtx(e: TreeContextEvent): void {
    this.ctxIds.set(e.ids);
    this.ctxStanCode.set(e.group.stan_nazn_code);
    const src = e.vagon ?? e.sub;
    this.ctxNaznach.set(src?.naznach ?? e.group.naznach ?? '');
    // «По назначению» ставит КАЖДОМУ выбранному его родной gruzpol_s (решение
    // владельца) — достаточно, чтобы хоть одному было куда возвращаться.
    const vagons = e.vagon ? [e.vagon]
      : e.sub ? e.sub.vagons
      : e.group.sub_groups.flatMap((sg) => sg.vagons);
    this.ctxHasForeign.set(vagons.some((v) => v.gruzpol_s && v.gruzpol_s !== v.naznach));
  }

  // ── Применение ─────────────────────────────────────────────────────────
  async applyRearrange(target: string, ids: string[]): Promise<void> {
    const vagonIds = ids.length ? ids : [...this.selected()];
    if (!vagonIds.length || !target) return;
    this.applying.set(true);
    try {
      const res = await this.api.applyRearrangement(vagonIds, target);
      this.notifyResult('Переставлено', res.updated, res.selected);
      await this.load();
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.applying.set(false);
    }
  }

  /** «По назначению»: каждому выбранному — его родной грузополучатель. */
  async applyByGruzpol(ids: string[]): Promise<void> {
    const vagonIds = ids.length ? ids : [...this.selected()];
    if (!vagonIds.length) return;
    this.applying.set(true);
    try {
      const res = await this.api.applyRearrangement(vagonIds, '', true);
      this.notifyResult('Возвращено по назначению', res.updated, res.selected);
      await this.load();
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.applying.set(false);
    }
  }

  async applyRedirect(kind: 'own' | 'ext' | 'cancel', target: string, ids: string[]): Promise<void> {
    const vagonIds = ids.length ? ids : [...this.selected()];
    if (!vagonIds.length) return;
    this.applying.set(true);
    try {
      const res = await this.api.applyRedirection(vagonIds, kind, target);
      this.notifyResult(kind === 'cancel' ? 'Отменена переадресация' : 'Переадресовано', res.updated, res.selected);
      this.portDialog.set(false);
      this.portName.set('');
      await this.load();
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.applying.set(false);
    }
  }

  applyRedirectChoice(): void {
    const v = this.redirectChoice();
    if (v === 'ВП') {
      this.ctxIds.set([]);
      this.portDialog.set(true);
    } else if (v === 'ОТМЕНА') {
      void this.applyRedirect('cancel', '', []);
    } else if (v) {
      void this.applyRedirect('own', v, []);
    }
  }

  applyExternal(): void {
    void this.applyRedirect('ext', this.portName().trim(), this.ctxIds());
  }

  /** Честный тост: сколько реально изменено из выбранного. */
  private notifyResult(what: string, updated: number, selectedN: number): void {
    if (updated === selectedN) {
      this.msg.success(`${what}: ${updated} ваг. Ход и прогноз пересчитаны.`);
    } else if (updated === 0) {
      this.msg.info(`Изменений нет: все ${selectedN} выбранных вагонов уже на этом назначении.`);
    } else {
      this.msg.success(`${what}: ${updated} из ${selectedN} выбранных (остальные уже там). Ход и прогноз пересчитаны.`);
    }
  }
}
