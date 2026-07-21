import { Component, OnInit, computed, inject, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { DragDropModule } from '@angular/cdk/drag-drop';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzSliderModule } from 'ng-zorro-antd/slider';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzDropDownModule, NzContextMenuService, NzDropdownMenuComponent } from 'ng-zorro-antd/dropdown';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import {
  ArrivalGroup, ArrivalSubgroup, ArrivalsApiService, ArrivalsUpdate, ArrivalVagon, CandidateGroup, TerminalTarget,
} from './arrivals-api.service';
import { VagonTrailModalComponent } from './vagon-trail-modal.component';

/**
 * Модалка «История прибывших — <станция>» (перенос gtport HistoryTable):
 * таблица Дата / Индекс / План / Факт / Откл + колонка на каждый терминал
 * станции (подгруппы по naznach, клик — разворот вагонов). Окно перемещается
 * за заголовок (cdkDrag).
 *
 * Редактирование (перенос механики gtport, решения владельца): все операции —
 * по ВЫБРАННЫМ вагонам (клик по чипу вагона; ПКМ по группе/подгруппе/вагону
 * автовыделяет его состав): «Изменить прибытие» (индекс/план ЖД/факт, пересчёт
 * отклонения на бэке), «Отменить прибытие» (только история, снимок не трогаем),
 * «Выгрузить» (дата/место/смерзаемость), «В <терминал>» (перераспределение
 * после прибытия; терминалы станции из реестра). Экспорт в Excel — вся таблица
 * (листы «Поезда»+«Вагоны») и группа. Судовая партия не переносилась.
 */
@Component({
  selector: 'app-arrivals-history',
  imports: [
    FormsModule, DragDropModule, NzButtonModule, NzIconModule, NzInputModule,
    NzModalModule, NzSliderModule, NzSpinModule, NzTooltipModule, NzDropDownModule,
    VagonTrailModalComponent,
  ],
  template: `
    <nz-modal [nzVisible]="true" [nzTitle]="title" [nzFooter]="null" nzWidth="1250px"
              [nzMask]="false" nzWrapClassName="arrivals-wrap" (nzOnCancel)="closed.emit()">
      <ng-template #title>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          История прибывших — {{ station() }}
        </div>
      </ng-template>
      <ng-container *nzModalContent>
        <div class="bar">
          <b>Прибывшие {{ terminalNames().join('+') }}</b>
          @if (selected().size) {
            <span class="sel-cnt">выбрано: {{ selected().size }}</span>
            <button nz-button nzSize="small" (click)="clearSelection()">Сбросить</button>
          }
          <span class="spacer"></span>
          <input nz-input nzSize="small" class="search" placeholder="Поиск вагона…"
                 [ngModel]="search()" (ngModelChange)="onSearch($event)" />
          <span class="lbl">С</span>
          <input class="date" type="date" [ngModel]="from()" (ngModelChange)="from.set($event); load()" />
          <span class="lbl">По</span>
          <input class="date" type="date" [ngModel]="to()" (ngModelChange)="to.set($event); load()" />
          <button nz-button nzSize="small" (click)="resetDates()">Сегодня/Вчера</button>
          <button nz-button nzType="text" nzSize="small" nz-tooltip nzTooltipTitle="Скачать таблицу (Excel)"
                  (click)="exportAll()">
            <span nz-icon nzType="download"></span>
          </button>
          <button nz-button nzType="text" nzSize="small" nz-tooltip nzTooltipTitle="Обновить" (click)="load()">
            <span nz-icon nzType="sync"></span>
          </button>
          <button nz-button nzType="text" nzSize="small" nz-tooltip nzTooltipTitle="Свернуть все вагоны"
                  (click)="collapseAll()">
            <span nz-icon nzType="eye-invisible"></span>
          </button>
        </div>

        <!-- Кандидаты в прибывшие (статус 9): подтверждение/отклонение оператором -->
        @if (candidates().length) {
          <div class="cands">
            <div class="cands-title">
              <span nz-icon nzType="question-circle"></span>
              <b>Кандидаты на прибытие ({{ candidates().length }})</b>
              <span class="mut">АСУ не дала дату — подтвердите или скройте</span>
            </div>
            @for (c of candidates(); track c.key) {
              <div class="cand">
                <span class="num b">{{ c.index || '—' }}</span>
                <span class="mut">{{ c.station_nach }}</span>
                <span class="mut">({{ c.vagon_count }})</span>
                <span class="cand-sost ell" [title]="candSostav(c)">{{ candSostav(c) }}</span>
                <span class="mut nowrap">оп. {{ fmtDT(c.time_op) }}</span>
                <span class="spacer"></span>
                <button nz-button nzType="primary" nzSize="small" (click)="openConfirm(c)">Подтвердить…</button>
                <button nz-button nzSize="small" nz-tooltip
                        nzTooltipTitle="Скрыть до новых данных (вагоны остаются кандидатами)"
                        (click)="dismiss(c)">Скрыть</button>
              </div>
            }
          </div>
        }

        <nz-spin [nzSpinning]="loading()">
          <div class="tbl-wrap">
            <table class="tbl">
              <thead>
                <tr>
                  <th class="c-date">Дата</th>
                  <th class="c-idx">Индекс</th>
                  <th class="c-plan">План</th>
                  <th class="c-fact">Факт</th>
                  <th class="c-otkl">Откл</th>
                  @for (t of terminals(); track t.name) { <th>{{ t.name }}</th> }
                </tr>
              </thead>
              <tbody>
                @for (g of filteredGroups(); track g.key) {
                  <tr (contextmenu)="openGroupMenu($event, g, menu)">
                    <td class="c-date">{{ fmtD(g.date_prib_d) }}</td>
                    <td class="c-idx num">{{ g.index_pp || '—' }}</td>
                    <td class="c-plan">{{ fmtDT(g.plan_jd) }}</td>
                    <td class="c-fact">{{ fmtT(g.date_prib) }}</td>
                    <td class="c-otkl num" [class.late]="g.otkl.startsWith('+')"
                        [class.early]="g.otkl.startsWith('-')">{{ g.otkl || '—' }}</td>
                    @for (t of terminals(); track t.name) {
                      <td class="c-term">
                        @for (sg of subsFor(g, t.name); track sg.key) {
                          <div class="sg" (click)="toggle(g.key, sg.key)"
                               (contextmenu)="openSubMenu($event, g, sg, menu)">
                            <span nz-icon [nzType]="isOpen(g.key, sg.key) ? 'down' : 'right'" class="tw"></span>
                            <span [class.hit]="isHit(sg)">{{ sg.display }}</span>
                          </div>
                          @if (isOpen(g.key, sg.key)) {
                            <div class="vagons">
                              @for (v of sg.vagons; track v.id) {
                                <span class="chip" [class.hit]="matches(v.vagon)"
                                      [class.sel]="selected().has(v.id)"
                                      (click)="toggleVagon(v.id)"
                                      (contextmenu)="openVagonMenu($event, g, v, menu)">
                                  {{ v.vagon }}@if (v.shipments) { ({{ v.shipments }}) }
                                </span>
                              }
                            </div>
                          }
                        } @empty { <span class="mut">—</span> }
                      </td>
                    }
                  </tr>
                } @empty {
                  <tr><td [attr.colspan]="5 + terminals().length" class="empty">Нет прибывших за период</td></tr>
                }
              </tbody>
            </table>
          </div>
        </nz-spin>

        <p class="hint">Клик по вагону — выбор; ПКМ по поезду/составу/вагону — операции (применяются к выбранным вагонам).</p>
      </ng-container>
    </nz-modal>

    <!-- ПКМ: операции по выбранным вагонам -->
    <nz-dropdown-menu #menu="nzDropdownMenu">
      <ul nz-menu>
        @if (ctxVagon(); as v) {
          <li nz-menu-item (click)="openTrail(v)">История движения вагона {{ v.vagon }}…</li>
          <li nz-menu-divider></li>
        }
        <li nz-menu-item (click)="openEdit()">Изменить прибытие…</li>
        <li nz-menu-item (click)="openUnload()">Выгрузить…</li>
        @for (t of terminals(); track t.name) {
          <li nz-menu-item (click)="applyNaznach(t.name)">В {{ t.name }}</li>
        }
        <li nz-menu-item nzDanger (click)="cancelArrival()">Отменить прибытие</li>
        @if (ctxGroup(); as g) {
          <li nz-menu-divider></li>
          <li nz-menu-item (click)="exportGroup(g)">Экспорт группы в Excel</li>
        }
      </ul>
    </nz-dropdown-menu>

    <!-- «История движения вагона» (ПКМ по вагону): база → при пустой АСУ -->
    @if (trailVagon(); as tv) {
      <app-vagon-trail-modal [vagonId]="tv.id" [vagon]="tv.vagon" (closed)="trailVagon.set(null)" />
    }

    <!-- Диалог «Изменить прибытие» -->
    <nz-modal [nzVisible]="editOpen()" [nzTitle]="edTtl" nzWidth="420px"
              (nzOnCancel)="editOpen.set(false)" (nzOnOk)="saveEdit()"
              nzOkText="Сохранить" [nzOkDisabled]="!editValid()" [nzOkLoading]="applying()">
      <ng-template #edTtl>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>Изменить прибытие</div>
      </ng-template>
      <ng-container *nzModalContent>
        <div class="frm">
          <label>Индекс поезда
            <input nz-input [ngModel]="edIndex()" (ngModelChange)="edIndex.set($event)" placeholder="ХХХХ-ХХХ-ХХХХ" />
          </label>
          <label>Плановое прибытие (ЖД)
            <span class="dt">
              <input class="date" type="date" [ngModel]="edPlanD()" (ngModelChange)="edPlanD.set($event)" />
              <input class="date" type="time" [ngModel]="edPlanT()" (ngModelChange)="edPlanT.set($event)" />
            </span>
          </label>
          <label>Фактическое прибытие
            <span class="dt">
              <input class="date" type="date" [ngModel]="edFactD()" (ngModelChange)="edFactD.set($event)" />
              <input class="date" type="time" [ngModel]="edFactT()" (ngModelChange)="edFactT.set($event)" />
            </span>
          </label>
          <p class="mut">Вагонов: {{ selected().size }}. Отклонение пересчитается автоматически.</p>
        </div>
      </ng-container>
    </nz-modal>

    <!-- Диалог «Подтвердить прибытие» (кандидаты-9) -->
    <nz-modal [nzVisible]="confirmOpen()" [nzTitle]="cfTtl" nzWidth="420px"
              (nzOnCancel)="confirmOpen.set(false)" (nzOnOk)="saveConfirm()"
              nzOkText="Подтвердить" [nzOkDisabled]="!cfD() || !cfT()" [nzOkLoading]="applying()">
      <ng-template #cfTtl>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          Подтвердить прибытие — {{ cfGroup()?.index || '—' }}
        </div>
      </ng-template>
      <ng-container *nzModalContent>
        <div class="frm">
          <p>{{ cfGroup()?.station_nach }} · вагонов: {{ cfGroup()?.vagon_count }}</p>
          <label>Индекс поезда
            <input nz-input [ngModel]="cfIndex()" (ngModelChange)="cfIndex.set($event)" placeholder="ХХХХ-ХХХ-ХХХХ" />
          </label>
          <label>Фактическое прибытие
            <span class="dt">
              <input class="date" type="date" [ngModel]="cfD()" (ngModelChange)="cfD.set($event)" />
              <input class="date" type="time" [ngModel]="cfT()" (ngModelChange)="cfT.set($event)" />
            </span>
          </label>
          <p class="mut">Вагоны станут «прибыл» (статус 10), веха уйдёт в историю; отклонение от плана пересчитается.</p>
        </div>
      </ng-container>
    </nz-modal>

    <!-- Диалог «Выгрузить» -->
    <nz-modal [nzVisible]="unloadOpen()" [nzTitle]="unTtl" nzWidth="420px"
              (nzOnCancel)="unloadOpen.set(false)" (nzOnOk)="saveUnload()"
              nzOkText="Сохранить" [nzOkDisabled]="!unloadValid()" [nzOkLoading]="applying()">
      <ng-template #unTtl>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>Выгрузить</div>
      </ng-template>
      <ng-container *nzModalContent>
        <div class="frm">
          <label>Дата и время выгрузки
            <span class="dt">
              <input class="date" type="date" [ngModel]="unD()" (ngModelChange)="unD.set($event)" />
              <input class="date" type="time" [ngModel]="unT()" (ngModelChange)="unT.set($event)" />
            </span>
          </label>
          <label>Место выгрузки
            <input nz-input [ngModel]="unPlace()" (ngModelChange)="unPlace.set($event)" placeholder="Например: АЭ" />
          </label>
          <label>Смерзаемость: {{ unFrost() }}%
            <nz-slider [ngModel]="unFrost()" (ngModelChange)="unFrost.set($event)" [nzStep]="10" />
          </label>
          <p class="mut">Вагонов: {{ selected().size }}.</p>
        </div>
      </ng-container>
    </nz-modal>
  `,
  styles: [`
    .ttl { cursor: move; user-select: none; }
    .bar { display: flex; align-items: center; gap: var(--space-sm); flex-wrap: wrap; margin-bottom: var(--space-sm); }
    .spacer { flex: 1 1 auto; }
    .sel-cnt { color: var(--color-primary); font-size: var(--font-size-sm); font-weight: 600; }
    .lbl { color: var(--color-text-secondary); font-size: var(--font-size-sm); }
    .search { width: 160px; }
    .date { height: 26px; padding: 0 6px; border: 1px solid var(--color-border, #d9d9d9);
            border-radius: var(--radius-sm); font-size: var(--font-size-sm); color: inherit; background: transparent; }
    .tbl-wrap { max-height: 62vh; overflow: auto; }
    .tbl { width: 100%; border-collapse: collapse; font-size: var(--font-size-sm); }
    .tbl th { position: sticky; top: 0; background: var(--color-bg-subtle); font-weight: 600;
              padding: 4px 8px; border: 1px solid var(--color-border-light); text-align: center; z-index: 1; }
    .tbl td { padding: 3px 8px; border: 1px solid var(--color-border-light); vertical-align: top; }
    .c-date, .c-fact, .c-otkl { text-align: center; white-space: nowrap; }
    .c-plan { white-space: nowrap; text-align: center; }
    .num { font-variant-numeric: tabular-nums; }
    .c-otkl.late { color: var(--color-danger); }
    .c-otkl.early { color: var(--color-success); }
    .sg { display: flex; align-items: center; gap: 4px; cursor: pointer; border-radius: var(--radius-sm);
          padding: 1px 4px; white-space: nowrap; }
    .sg:hover { background: var(--color-bg-hover); }
    .tw { font-size: 10px; color: var(--color-text-muted); }
    .vagons { display: flex; flex-wrap: wrap; gap: 3px; padding: 2px 0 4px 18px; }
    .chip { font-variant-numeric: tabular-nums; border: 1px solid var(--color-border-dark);
            border-radius: var(--radius-sm); padding: 0 4px; cursor: pointer; }
    .chip.sel { background: var(--color-primary); border-color: var(--color-primary); color: var(--color-bg-surface); }
    .hit { background: var(--color-warning); border-radius: var(--radius-sm); }
    .chip.sel.hit { background: var(--color-primary); }
    .mut { color: var(--color-text-muted); }
    .empty { text-align: center; color: var(--color-text-secondary); padding: var(--space-md); }
    .hint { margin: var(--space-xs) 0 0; color: var(--color-text-muted); font-size: var(--font-size-sm); }
    /* Кандидаты на прибытие — жёлтая секция над таблицей. */
    .cands { background: var(--color-warning-bg); border: 1px solid var(--color-warning);
             border-radius: var(--radius-md); padding: var(--space-xs) var(--space-sm);
             margin-bottom: var(--space-sm); display: flex; flex-direction: column; gap: 2px; }
    .cands-title { display: flex; align-items: center; gap: var(--space-sm); font-size: var(--font-size-sm); }
    .cand { display: flex; align-items: center; gap: var(--space-sm); font-size: var(--font-size-sm);
            padding: 2px 0; min-width: 0; }
    .cand .b { font-weight: 600; }
    .cand-sost { flex: 1 1 auto; min-width: 0; }
    .nowrap { white-space: nowrap; }
    .ell { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .frm { display: flex; flex-direction: column; gap: var(--space-sm); }
    .frm label { display: flex; flex-direction: column; gap: 2px; font-size: var(--font-size-sm);
                 color: var(--color-text-secondary); }
    .dt { display: flex; gap: var(--space-sm); }
  `],
})
export class ArrivalsHistoryComponent implements OnInit {
  private readonly api = inject(ArrivalsApiService);
  private readonly msg = inject(NzMessageService);
  private readonly ctxMenu = inject(NzContextMenuService);

  /** Станция (заголовок окна) и её терминалы (колонки/фильтр naznach). */
  readonly station = input.required<string>();
  readonly terminals = input.required<TerminalTarget[]>();
  readonly closed = output<void>();

  readonly loading = signal(false);
  readonly applying = signal(false);
  readonly groups = signal<ArrivalGroup[]>([]);
  readonly from = signal('');
  readonly to = signal('');
  readonly search = signal('');
  /** Развёрнутые подгруппы: ключ `group.key::sub.key`. */
  readonly open = signal<Set<string>>(new Set());
  /** Выбранные вагоны (id) — цель всех операций правки. */
  readonly selected = signal<Set<string>>(new Set());
  /** Группа под курсором ПКМ (дефолты диалогов, экспорт группы). */
  readonly ctxGroup = signal<ArrivalGroup | null>(null);
  /** Вагон под курсором ПКМ (null — ПКМ был по поезду/составу). */
  readonly ctxVagon = signal<ArrivalVagon | null>(null);
  /** Вагон, для которого открыта «История движения». */
  readonly trailVagon = signal<ArrivalVagon | null>(null);

  // Диалог «Изменить прибытие».
  readonly editOpen = signal(false);
  readonly edIndex = signal('');
  readonly edPlanD = signal('');
  readonly edPlanT = signal('');
  readonly edFactD = signal('');
  readonly edFactT = signal('');
  // Кандидаты в прибывшие + диалог подтверждения.
  readonly candidates = signal<CandidateGroup[]>([]);
  readonly confirmOpen = signal(false);
  readonly cfGroup = signal<CandidateGroup | null>(null);
  readonly cfIndex = signal('');
  readonly cfD = signal('');
  readonly cfT = signal('');
  // Диалог «Выгрузить».
  readonly unloadOpen = signal(false);
  readonly unD = signal('');
  readonly unT = signal('');
  readonly unPlace = signal('');
  readonly unFrost = signal(0);

  readonly terminalNames = computed(() => this.terminals().map((t) => t.name));
  readonly editValid = computed(() =>
    !!this.edIndex().trim() && !!this.edPlanD() && !!this.edPlanT() && !!this.edFactD() && !!this.edFactT());
  readonly unloadValid = computed(() => !!this.unD() && !!this.unT() && !!this.unPlace().trim());

  ngOnInit(): void {
    void this.load();
  }

  async load(): Promise<void> {
    this.loading.set(true);
    try {
      const [res, cands] = await Promise.all([
        this.api.getArrivals(this.terminalNames(), this.from(), this.to()),
        this.api.getCandidates(this.terminalNames()),
      ]);
      this.groups.set(res.groups);
      this.candidates.set(cands ?? []);
      this.from.set(res.from);
      this.to.set(res.to);
      this.selected.set(new Set());
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.loading.set(false);
    }
  }

  resetDates(): void {
    this.from.set('');
    this.to.set('');
    void this.load();
  }

  /** Поиск: при вводе разворачиваем всё (эталон gtport), совпадения подсвечены. */
  onSearch(q: string): void {
    this.search.set(q);
    if (q.trim()) {
      const all = new Set<string>();
      for (const g of this.groups()) for (const sg of g.sub_groups) all.add(g.key + '::' + sg.key);
      this.open.set(all);
    }
  }

  readonly filteredGroups = computed(() => {
    const q = this.search().trim().toUpperCase();
    if (!q) return this.groups();
    return this.groups().filter((g) =>
      g.index_pp.toUpperCase().includes(q) ||
      g.sub_groups.some((sg) => sg.display.toUpperCase().includes(q) ||
        sg.vagons.some((v) => v.vagon.toUpperCase().includes(q))),
    );
  });

  subsFor(g: ArrivalGroup, naznach: string): ArrivalSubgroup[] {
    return g.sub_groups.filter((sg) => sg.naznach === naznach);
  }

  matches(text: string): boolean {
    const q = this.search().trim().toUpperCase();
    return !!q && text.toUpperCase().includes(q);
  }

  isHit(sg: ArrivalSubgroup): boolean {
    const q = this.search().trim().toUpperCase();
    return !!q && (sg.display.toUpperCase().includes(q) || sg.vagons.some((v) => v.vagon.toUpperCase().includes(q)));
  }

  toggle(gk: string, sk: string): void {
    const next = new Set(this.open());
    const key = gk + '::' + sk;
    next.has(key) ? next.delete(key) : next.add(key);
    this.open.set(next);
  }

  isOpen(gk: string, sk: string): boolean {
    return this.open().has(gk + '::' + sk);
  }

  collapseAll(): void {
    this.open.set(new Set());
    this.search.set('');
  }

  // ── Выделение и ПКМ ──────────────────────────────────────────────────────
  toggleVagon(id: string): void {
    const next = new Set(this.selected());
    next.has(id) ? next.delete(id) : next.add(id);
    this.selected.set(next);
  }

  clearSelection(): void {
    this.selected.set(new Set());
  }

  private addToSelection(ids: string[]): void {
    const next = new Set(this.selected());
    for (const id of ids) next.add(id);
    this.selected.set(next);
  }

  openGroupMenu(ev: MouseEvent, g: ArrivalGroup, menu: NzDropdownMenuComponent): void {
    ev.preventDefault();
    this.ctxGroup.set(g);
    this.ctxVagon.set(null);
    this.addToSelection(g.sub_groups.flatMap((sg) => sg.vagons.map((v) => v.id)));
    this.ctxMenu.create(ev, menu);
  }

  openSubMenu(ev: MouseEvent, g: ArrivalGroup, sg: ArrivalSubgroup, menu: NzDropdownMenuComponent): void {
    ev.preventDefault();
    ev.stopPropagation();
    this.ctxGroup.set(g);
    this.ctxVagon.set(null);
    this.addToSelection(sg.vagons.map((v) => v.id));
    this.ctxMenu.create(ev, menu);
  }

  openVagonMenu(ev: MouseEvent, g: ArrivalGroup, v: ArrivalVagon, menu: NzDropdownMenuComponent): void {
    ev.preventDefault();
    ev.stopPropagation();
    this.ctxGroup.set(g);
    this.ctxVagon.set(v); // ПКМ именно по вагону — только здесь есть «История движения»
    this.addToSelection([v.id]);
    this.ctxMenu.create(ev, menu);
  }

  /** История движения вагона — по рейсу (id строки истории), не по снимку. */
  openTrail(v: ArrivalVagon): void {
    this.trailVagon.set(v);
  }

  // ── Операции ─────────────────────────────────────────────────────────────
  openEdit(): void {
    if (!this.requireSelection()) return;
    const g = this.ctxGroup();
    this.edIndex.set(g?.index_pp ?? '');
    this.edPlanD.set(this.datePart(g?.plan_jd) || this.todayStr());
    this.edPlanT.set(this.timePart(g?.plan_jd) || '00:00');
    this.edFactD.set(this.datePart(g?.date_prib) || this.todayStr());
    this.edFactT.set(this.timePart(g?.date_prib) || '00:00');
    this.editOpen.set(true);
  }

  saveEdit(): void {
    void this.applyUpdate({
      index_pp: this.edIndex().trim(),
      plan_jd: `${this.edPlanD()}T${this.edPlanT()}:00`,
      date_prib: `${this.edFactD()}T${this.edFactT()}:00`,
    }, 'Прибытие изменено', () => this.editOpen.set(false));
  }

  openUnload(): void {
    if (!this.requireSelection()) return;
    const now = new Date();
    this.unD.set(this.todayStr());
    this.unT.set(`${String(now.getHours()).padStart(2, '0')}:${String(now.getMinutes()).padStart(2, '0')}`);
    this.unPlace.set(this.ctxGroup()?.sub_groups[0]?.naznach ?? '');
    this.unFrost.set(0);
    this.unloadOpen.set(true);
  }

  saveUnload(): void {
    void this.applyUpdate({
      date_vigr: `${this.unD()}T${this.unT()}:00`,
      place_vigr: this.unPlace().trim(),
      frost: this.unFrost(),
    }, 'Выгрузка проставлена', () => this.unloadOpen.set(false));
  }

  applyNaznach(term: string): void {
    if (!this.requireSelection()) return;
    void this.applyUpdate({ naznach: term }, `Назначение: ${term}`);
  }

  async cancelArrival(): Promise<void> {
    if (!this.requireSelection()) return;
    const ok = window.confirm(
      `Отменить прибытие для ${this.selected().size} ваг.? Вагоны вернутся в кандидаты ` +
      `(факт и отклонение сброшены в снимке и истории). Если дату прибытия давала АСУ, ` +
      `ближайшее обновление (~10 мин) снова отметит их прибывшими.`);
    if (!ok) return;
    this.applying.set(true);
    try {
      const res = await this.api.cancelArrival([...this.selected()]);
      this.msg.success(`Прибытие отменено: ${res.updated} ваг. — вагоны снова в кандидатах.`);
      await this.load();
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.applying.set(false);
    }
  }

  // ── Кандидаты: подтверждение / отклонение ────────────────────────────────
  candSostav(c: CandidateGroup): string {
    return c.sub_groups.map((sg) => sg.display).join(' · ') || '—';
  }

  private candVagonIds(c: CandidateGroup): string[] {
    return c.sub_groups.flatMap((sg) => sg.vagons.map((v) => v.id));
  }

  openConfirm(c: CandidateGroup): void {
    this.cfGroup.set(c);
    this.cfIndex.set(c.index);
    this.cfD.set(this.datePart(c.time_op) || this.todayStr());
    this.cfT.set(this.timePart(c.time_op) || '00:00');
    this.confirmOpen.set(true);
  }

  async saveConfirm(): Promise<void> {
    const c = this.cfGroup();
    if (!c) return;
    this.applying.set(true);
    try {
      const res = await this.api.confirmArrival(
        this.candVagonIds(c), `${this.cfD()}T${this.cfT()}:00`, this.cfIndex().trim());
      this.msg.success(`Прибытие подтверждено: ${res.updated} ваг. Поезд ушёл в прибывшие.`);
      this.confirmOpen.set(false);
      await this.load();
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.applying.set(false);
    }
  }

  async dismiss(c: CandidateGroup): Promise<void> {
    try {
      const res = await this.api.dismissCandidates(this.candVagonIds(c));
      this.msg.info(`Скрыто кандидатов: ${res.updated} ваг. (до новых данных АСУ).`);
      await this.load();
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    }
  }

  private requireSelection(): boolean {
    if (!this.selected().size) {
      this.msg.info('Сначала выберите вагоны (клик по вагону или ПКМ по поезду/составу).');
      return false;
    }
    return true;
  }

  private async applyUpdate(fields: Omit<ArrivalsUpdate, 'vagon_ids'>, what: string, onDone?: () => void): Promise<void> {
    this.applying.set(true);
    try {
      const res = await this.api.updateVagons({ vagon_ids: [...this.selected()], ...fields });
      this.msg.success(`${what}: ${res.updated} ваг.`);
      onDone?.();
      await this.load();
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.applying.set(false);
    }
  }

  // ── Экспорт в Excel (в браузере, как gtport) ─────────────────────────────
  async exportAll(): Promise<void> {
    const XLSX = await import('xlsx-js-style');
    const wb = XLSX.utils.book_new();
    const trains = this.filteredGroups().map((g) => {
      const row: Record<string, string | number> = {
        'Дата': this.fmtD(g.date_prib_d), 'Индекс поезда': g.index_pp,
        'План (ЖД)': this.fmtDT(g.plan_jd), 'Факт': this.fmtT(g.date_prib),
        'Отклонение': g.otkl, 'Всего вагонов': g.vagon_count,
      };
      for (const t of this.terminals()) {
        row[t.name] = this.subsFor(g, t.name).map((sg) => sg.display).join('; ');
      }
      return row;
    });
    XLSX.utils.book_append_sheet(wb, XLSX.utils.json_to_sheet(trains), 'Поезда');
    XLSX.utils.book_append_sheet(wb, XLSX.utils.json_to_sheet(this.vagonRows(this.filteredGroups())), 'Вагоны');
    XLSX.writeFile(wb, `История_${this.station()}_${this.from()}_${this.to()}.xlsx`);
  }

  async exportGroup(g: ArrivalGroup): Promise<void> {
    const XLSX = await import('xlsx-js-style');
    const wb = XLSX.utils.book_new();
    XLSX.utils.book_append_sheet(wb, XLSX.utils.json_to_sheet(this.vagonRows([g])), 'Вагоны');
    XLSX.writeFile(wb, `Поезд_${g.index_pp || 'без_индекса'}_${this.fmtD(g.date_prib_d)}.xlsx`);
  }

  private vagonRows(groups: ArrivalGroup[]): Record<string, string | number>[] {
    const rows: Record<string, string | number>[] = [];
    for (const g of groups) {
      for (const sg of g.sub_groups) {
        for (const v of sg.vagons) {
          rows.push({
            'Вагон': v.vagon, 'Род. индекс': sg.index_main, 'Индекс': g.index_pp,
            'Грузополучатель': sg.gruzpol_s, 'Назначение': sg.naznach,
            'Прибытие': this.fmtDT(g.date_prib), 'Группа': sg.display,
          });
        }
      }
    }
    return rows;
  }

  // ── Форматы времени (МСК naive, отдаём как пришло — без сдвигов) ──────────
  /** дд.мм.гг */
  fmtD(ts: string | null): string {
    if (!ts || ts.length < 10) return '—';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)}.${ts.slice(2, 4)}`;
  }
  /** дд.мм.гг чч:мм */
  fmtDT(ts: string | null): string {
    if (!ts || ts.length < 16) return '—';
    return `${this.fmtD(ts)} ${ts.slice(11, 16)}`;
  }
  /** чч:мм */
  fmtT(ts: string | null): string {
    if (!ts || ts.length < 16) return '—';
    return ts.slice(11, 16);
  }

  private datePart(ts: string | null | undefined): string {
    return ts && ts.length >= 10 ? ts.slice(0, 10) : '';
  }
  private timePart(ts: string | null | undefined): string {
    return ts && ts.length >= 16 ? ts.slice(11, 16) : '';
  }
  private todayStr(): string {
    return new Date().toISOString().slice(0, 10);
  }
}
