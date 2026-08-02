import { Component, OnInit, computed, inject, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { DragDropModule } from '@angular/cdk/drag-drop';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzCheckboxModule } from 'ng-zorro-antd/checkbox';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzRadioModule } from 'ng-zorro-antd/radio';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzDropDownModule, NzContextMenuService, NzDropdownMenuComponent } from 'ng-zorro-antd/dropdown';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { AuthService } from '../../core/auth/auth.service';
import { todayMsk } from '../../shared/msk-date';
import { TimeBase, TimeBaseService, mskDateInBase, shiftDateIfEvening } from '../../shared/time-base.service';
import { ArrivalsApiService } from '../home/arrivals-api.service';
import { VagonTrailModalComponent } from '../home/vagon-trail-modal.component';
import { MissingApiService, MissingGroup, MissingSubgroup, MissingVagon } from './missing-api.service';

/**
 * Перемещаемая модалка «Пропавшие вагоны» (записи-8): агрегированный вид
 * поезд → подгруппа → вагоны по образцу «Истории прибывших» и действие
 * «Подтвердить прибытие» — из-за дискретности выгрузок дислокации вагон мог
 * прибыть, выгрузиться и выпасть из потока без статусов; диспетчер
 * восстанавливает факты руками (прибытие обязательно, выгрузка — опционально
 * той же формой). Подтверждённый вагон уходит из пропавших и появляется в
 * «Истории прибывших» за указанную дату.
 *
 * Выбор целей — как в истории прибывших: клик по чипу вагона, ПКМ по
 * поезду/составу/вагону автовыделяет состав; «Подтвердить…» есть и кнопкой на
 * строке поезда. Доноры перегруза (статус 6) остаются в старой общей модалке.
 */
@Component({
  selector: 'app-missing-modal',
  imports: [
    FormsModule, DragDropModule, NzButtonModule, NzCheckboxModule, NzIconModule,
    NzInputModule, NzModalModule, NzRadioModule, NzSpinModule, NzTooltipModule,
    NzDropDownModule, VagonTrailModalComponent,
  ],
  template: `
    <nz-modal [nzVisible]="true" [nzTitle]="ttl" [nzFooter]="null" nzWidth="1150px"
              [nzMask]="false" (nzOnCancel)="closed.emit()">
      <ng-template #ttl>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          Пропавшие вагоны ({{ total() }})
        </div>
      </ng-template>
      <ng-container *nzModalContent>
        <div class="bar">
          <span class="mut">Исчезли из выгрузки до завершения рейса; показана последняя известная позиция.</span>
          @if (selected().size) {
            <span class="sel-cnt">выбрано: {{ selected().size }}</span>
            @if (canEdit()) {
              <button nz-button nzType="primary" nzSize="small" (click)="openConfirmSelected()">
                Подтвердить прибытие…
              </button>
            }
            <button nz-button nzSize="small" (click)="clearSelection()">Сбросить</button>
          }
          <span class="spacer"></span>
          <input nz-input nzSize="small" class="search" placeholder="№ вагона"
                 [ngModel]="search()" (ngModelChange)="onSearch($event)" />
          <button nz-button nzType="text" nzSize="small" nz-tooltip nzTooltipTitle="Обновить" (click)="load()">
            <span nz-icon nzType="reload"></span>
          </button>
          <button nz-button nzType="text" nzSize="small" nz-tooltip nzTooltipTitle="Свернуть все вагоны"
                  (click)="collapseAll()">
            <span nz-icon nzType="eye-invisible"></span>
          </button>
        </div>

        <nz-spin [nzSpinning]="loading()">
          <div class="dp-tbl-wrap">
            <table class="dp-tbl">
              <thead>
                <tr>
                  <th class="c-idx">Индекс</th>
                  <th>Станция посл. операции</th>
                  <th class="c-op">Операция</th>
                  <th class="c-dt">Время оп.</th>
                  <th class="c-dt">Пропал</th>
                  <th class="c-days">Дней</th>
                  <th>Состав</th>
                  @if (canEdit()) { <th class="c-act"></th> }
                </tr>
              </thead>
              <tbody>
                @for (g of filteredGroups(); track g.key) {
                  <tr [class.stale]="g.days_missing >= 3" (contextmenu)="openGroupMenu($event, g, menu)">
                    <td class="num idx" [title]="g.index">{{ g.index || '—' }}</td>
                    <td class="ell" [title]="station(g)">{{ station(g) }}</td>
                    <td class="ell" [title]="g.oper_s">{{ g.oper_s || '—' }}</td>
                    <td class="c">{{ fmt(g.time_op) }}</td>
                    <td class="c">{{ fmt(g.missing_since) }}</td>
                    <td class="c days">{{ g.days_missing }}</td>
                    <td class="c-sost">
                      @for (sg of g.sub_groups; track sg.key) {
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
                                    [title]="v.cargo_s || 'порожний'"
                                    (click)="canEdit() ? toggleVagon(v.id) : openTrail(v)"
                                    (contextmenu)="openVagonMenu($event, g, v, menu)">
                                {{ v.vagon }}
                              </span>
                            }
                          </div>
                        }
                      }
                    </td>
                    @if (canEdit()) {
                      <td class="c">
                        <button nz-button nzSize="small" nz-tooltip
                                nzTooltipTitle="Подтвердить прибытие всего состава"
                                (click)="openConfirmGroup(g)">Подтвердить…</button>
                      </td>
                    }
                  </tr>
                } @empty {
                  <tr><td [attr.colspan]="canEdit() ? 8 : 7" class="empty">Список пуст</td></tr>
                }
              </tbody>
            </table>
          </div>
        </nz-spin>

        @if (canEdit()) {
          <p class="hint">Клик по вагону — выбор; «Подтвердить прибытие» — кнопкой на поезде или ПКМ
            по поезду/составу/вагону. ПКМ по вагону также даёт историю движения. Времена — московские.</p>
        } @else {
          <p class="hint">Клик по вагону — история движения. Времена — московские.</p>
        }
      </ng-container>
    </nz-modal>

    <!-- ПКМ: операции по выбранным вагонам -->
    <nz-dropdown-menu #menu="nzDropdownMenu">
      <ul nz-menu>
        @if (ctxVagon(); as v) {
          <li nz-menu-item (click)="openTrail(v)">История движения вагона {{ v.vagon }}…</li>
        }
        @if (canEdit()) {
          @if (ctxVagon()) { <li nz-menu-divider></li> }
          <li nz-menu-item (click)="openConfirmSelected()">Подтвердить прибытие…</li>
        }
      </ul>
    </nz-dropdown-menu>

    @if (trailVagon(); as tv) {
      <app-vagon-trail-modal [vagonId]="tv.id" [vagon]="tv.vagon" (closed)="trailVagon.set(null)" />
    }

    <!-- Диалог «Подтвердить прибытие» (прибытие обязательно, выгрузка опционально) -->
    <nz-modal [nzVisible]="confirmOpen()" [nzTitle]="cfTtl" nzWidth="440px"
              (nzOnCancel)="confirmOpen.set(false)" (nzOnOk)="saveConfirm()"
              nzOkText="Подтвердить" [nzOkDisabled]="!confirmValid()" [nzOkLoading]="applying()">
      <ng-template #cfTtl>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          Подтвердить прибытие — {{ ctxGroup()?.index || 'выбранные вагоны' }}
        </div>
      </ng-template>
      <ng-container *nzModalContent>
        <div class="frm">
          <label>Шкала времени
            <span class="dt">
              <nz-radio-group nzSize="small" nzButtonStyle="solid"
                              [ngModel]="tb.base()" (ngModelChange)="onBaseChange($event)">
                <label nz-radio-button nzValue="jd">ЖД</label>
                <label nz-radio-button nzValue="msk">МСК</label>
              </nz-radio-group>
              <span class="mut">{{ tb.base() === 'jd' ? 'час ≥ 18 — следующие сутки' : 'реальное московское' }}</span>
            </span>
          </label>
          <label>Индекс поезда
            <input nz-input [ngModel]="cfIndex()" (ngModelChange)="cfIndex.set($event)" placeholder="ХХХХ-ХХХ-ХХХХ" />
          </label>
          <label>Фактическое прибытие
            <span class="dt">
              <input class="date" type="date" [ngModel]="cfD()" (ngModelChange)="cfD.set($event)" />
              <input class="date" type="time" [ngModel]="cfT()" (ngModelChange)="cfT.set($event)" />
            </span>
          </label>
          <label nz-checkbox [ngModel]="cfUnload()" (ngModelChange)="cfUnload.set($event)">
            Выгружен (указать выгрузку сразу)
          </label>
          @if (cfUnload()) {
            <label>Дата и время выгрузки
              <span class="dt">
                <input class="date" type="date" [ngModel]="unD()" (ngModelChange)="unD.set($event)" />
                <input class="date" type="time" [ngModel]="unT()" (ngModelChange)="unT.set($event)" />
              </span>
            </label>
            <label>Место выгрузки
              <input nz-input list="missing-terminals" [ngModel]="unPlace()" (ngModelChange)="unPlace.set($event)"
                     placeholder="пусто — терминал назначения" />
              <datalist id="missing-terminals">
                @for (t of terminalNames(); track t) { <option [value]="t"></option> }
              </datalist>
            </label>
          }
          <p class="mut">Вагонов: {{ selectedVagons().length }}. Времена — в шкале
            {{ tb.base() === 'jd' ? 'ЖД' : 'МСК' }} (пересчёт сделает сервер). Вагоны уйдут из
            пропавших и появятся в «Истории прибывших» за указанную дату{{ cfUnload() ? ' как выгруженные' : '' }}.
            Если вагон вернётся в дислокацию, свежие данные АСУ будут вернее ручных.</p>
          <div class="sel-chips">
            @for (v of selectedVagons(); track v) { <span class="chip">{{ v }}</span> }
          </div>
        </div>
      </ng-container>
    </nz-modal>
  `,
  styles: [`
    .ttl { cursor: move; user-select: none; }
    .bar { display: flex; align-items: center; gap: var(--space-sm); flex-wrap: wrap;
           margin-bottom: var(--space-sm); font-size: var(--font-size-sm); }
    .spacer { flex: 1 1 auto; }
    .sel-cnt { color: var(--color-primary); font-weight: 600; }
    .search { width: 140px; }
    .mut { color: var(--color-text-muted); }
    .dp-tbl td { vertical-align: top; }
    .c-idx { width: 130px; } .c-op { width: 130px; } .c-dt { width: 100px; }
    .c-days { width: 52px; } .c-act { width: 118px; }
    .num { font-variant-numeric: tabular-nums; }
    .idx, .ell { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .c { text-align: center; font-variant-numeric: tabular-nums; white-space: nowrap; }
    .days { font-weight: 600; }
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
    /* Давно пропавшие (3+ суток) — жёлтая подсветка, как в старом плоском списке. */
    tr.stale > td { background: var(--color-warning-bg, #fffbe6); }
    .empty { text-align: center; color: var(--color-text-secondary); padding: var(--space-md); }
    .hint { margin: var(--space-xs) 0 0; color: var(--color-text-muted); font-size: var(--font-size-sm); }
    .frm { display: flex; flex-direction: column; gap: var(--space-sm); }
    .frm label { display: flex; flex-direction: column; gap: 2px; font-size: var(--font-size-sm);
                 color: var(--color-text-secondary); }
    .frm label[nz-checkbox] { flex-direction: row; align-items: center; }
    .dt { display: flex; gap: var(--space-sm); }
    .date { height: 26px; padding: 0 6px; border: 1px solid var(--color-border, #d9d9d9);
            border-radius: var(--radius-sm); font-size: var(--font-size-sm); color: inherit; background: transparent; }
    /* Предпросмотр затронутых вагонов (защита от батча «не тем вагонам»). */
    .sel-chips { display: flex; flex-wrap: wrap; gap: 3px; max-height: 96px; overflow: auto;
                 font-size: var(--font-size-sm); }
  `],
})
export class MissingModalComponent implements OnInit {
  private readonly api = inject(MissingApiService);
  private readonly arrivalsApi = inject(ArrivalsApiService);
  private readonly msg = inject(NzMessageService);
  private readonly ctxMenu = inject(NzContextMenuService);
  /** Подтверждение — порог operator; клиенту только просмотр и история движения. */
  readonly canEdit = inject(AuthService).canEdit;
  /** Шкала ввода времени (ЖД/МСК) — общая для всех диалогов правок. */
  readonly tb = inject(TimeBaseService);

  readonly closed = output<void>();
  /** Списки изменились (подтверждение) — карточке «Информация» пора обновить счётчики. */
  readonly changed = output<void>();

  readonly loading = signal(false);
  readonly applying = signal(false);
  readonly groups = signal<MissingGroup[]>([]);
  readonly search = signal('');
  /** Развёрнутые подгруппы: ключ `group.key::sub.key`. */
  readonly open = signal<Set<string>>(new Set());
  /** Выбранные вагоны (id рейсов) — цель подтверждения. */
  readonly selected = signal<Set<string>>(new Set());
  readonly ctxGroup = signal<MissingGroup | null>(null);
  readonly ctxVagon = signal<MissingVagon | null>(null);
  readonly trailVagon = signal<MissingVagon | null>(null);
  /** Подсказки терминалов для места выгрузки (реестр ports). */
  readonly terminalNames = signal<string[]>([]);

  // Диалог «Подтвердить прибытие».
  readonly confirmOpen = signal(false);
  readonly cfIndex = signal('');
  readonly cfD = signal('');
  readonly cfT = signal('');
  readonly cfUnload = signal(false);
  readonly unD = signal('');
  readonly unT = signal('');
  readonly unPlace = signal('');

  readonly total = computed(() => this.groups().reduce((n, g) => n + g.vagon_count, 0));
  readonly confirmValid = computed(() =>
    !!this.cfD() && !!this.cfT() && (!this.cfUnload() || (!!this.unD() && !!this.unT())));
  /** Номера выбранных вагонов — предпросмотр в диалоге. */
  readonly selectedVagons = computed(() => {
    const sel = this.selected();
    const out: string[] = [];
    for (const g of this.groups())
      for (const sg of g.sub_groups)
        for (const v of sg.vagons) if (sel.has(v.id)) out.push(v.vagon);
    return out;
  });

  ngOnInit(): void {
    void this.load();
    void this.tb.init();
    void this.arrivalsApi.getTerminals()
      .then((ts) => this.terminalNames.set((ts ?? []).map((t) => t.name)))
      .catch(() => undefined); // подсказки не критичны — поле останется свободным вводом
  }

  async load(): Promise<void> {
    this.loading.set(true);
    try {
      this.groups.set(await this.api.getMissingGroups() ?? []);
      this.selected.set(new Set());
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.loading.set(false);
    }
  }

  readonly filteredGroups = computed(() => {
    const q = this.search().trim().toUpperCase();
    if (!q) return this.groups();
    return this.groups().filter((g) =>
      g.index.toUpperCase().includes(q) ||
      g.sub_groups.some((sg) => sg.display.toUpperCase().includes(q) ||
        sg.vagons.some((v) => v.vagon.toUpperCase().includes(q))),
    );
  });

  /** Поиск: при вводе разворачиваем всё, совпадения подсвечены (эталон прибывших). */
  onSearch(q: string): void {
    this.search.set(q);
    if (q.trim()) {
      const all = new Set<string>();
      for (const g of this.groups()) for (const sg of g.sub_groups) all.add(g.key + '::' + sg.key);
      this.open.set(all);
    }
  }

  matches(text: string): boolean {
    const q = this.search().trim().toUpperCase();
    return !!q && text.toUpperCase().includes(q);
  }

  isHit(sg: MissingSubgroup): boolean {
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

  // ── Выделение и ПКМ (паттерн «Истории прибывших») ────────────────────────
  toggleVagon(id: string): void {
    const next = new Set(this.selected());
    next.has(id) ? next.delete(id) : next.add(id);
    this.selected.set(next);
  }

  clearSelection(): void {
    this.selected.set(new Set());
  }

  /** ПКМ по цели вне текущего выбора сбрасывает его; по цели из выбора — сохраняет. */
  private selectForMenu(ids: string[]): void {
    const cur = this.selected();
    const touchesSelection = ids.some((id) => cur.has(id));
    const next = touchesSelection ? new Set(cur) : new Set<string>();
    for (const id of ids) next.add(id);
    this.selected.set(next);
  }

  openGroupMenu(ev: MouseEvent, g: MissingGroup, menu: NzDropdownMenuComponent): void {
    ev.preventDefault();
    this.ctxGroup.set(g);
    this.ctxVagon.set(null);
    this.selectForMenu(g.sub_groups.flatMap((sg) => sg.vagons.map((v) => v.id)));
    this.ctxMenu.create(ev, menu);
  }

  openSubMenu(ev: MouseEvent, g: MissingGroup, sg: MissingSubgroup, menu: NzDropdownMenuComponent): void {
    ev.preventDefault();
    ev.stopPropagation();
    this.ctxGroup.set(g);
    this.ctxVagon.set(null);
    this.selectForMenu(sg.vagons.map((v) => v.id));
    this.ctxMenu.create(ev, menu);
  }

  openVagonMenu(ev: MouseEvent, g: MissingGroup, v: MissingVagon, menu: NzDropdownMenuComponent): void {
    ev.preventDefault();
    ev.stopPropagation();
    this.ctxGroup.set(g);
    this.ctxVagon.set(v);
    this.selectForMenu([v.id]);
    this.ctxMenu.create(ev, menu);
  }

  /** История движения — по id рейса: работает и для вагона, которого нет в снимке. */
  openTrail(v: MissingVagon): void {
    this.trailVagon.set(v);
  }

  // ── Подтверждение прибытия ────────────────────────────────────────────────
  openConfirmGroup(g: MissingGroup): void {
    this.ctxGroup.set(g);
    this.selected.set(new Set(g.sub_groups.flatMap((sg) => sg.vagons.map((v) => v.id))));
    this.openConfirmSelected();
  }

  openConfirmSelected(): void {
    if (!this.selected().size) {
      this.msg.info('Сначала выберите вагоны (клик по вагону или ПКМ по поезду/составу).');
      return;
    }
    const g = this.ctxGroup();
    this.cfIndex.set(g?.index ?? '');
    // Дефолт — время последней операции (МСК-штамп): показываем в текущей шкале.
    const d = this.datePart(g?.time_op) || todayMsk();
    const t = this.timePart(g?.time_op) || '00:00';
    this.cfD.set(mskDateInBase(d, t, this.tb.base()));
    this.cfT.set(t);
    this.cfUnload.set(false);
    this.unD.set('');
    this.unT.set('');
    this.unPlace.set('');
    this.confirmOpen.set(true);
  }

  /** Переключение шкалы: значения открытого диалога пересчитываются на месте. */
  onBaseChange(nb: TimeBase): void {
    if (nb === this.tb.base()) return;
    const delta = nb === 'jd' ? 1 : -1; // msk→jd: вечер +сутки; jd→msk: −сутки
    if (this.cfD() && this.cfT()) this.cfD.set(shiftDateIfEvening(this.cfD(), this.cfT(), delta));
    if (this.unD() && this.unT()) this.unD.set(shiftDateIfEvening(this.unD(), this.unT(), delta));
    this.tb.set(nb);
  }

  async saveConfirm(): Promise<void> {
    // Дефолт выгрузки — момент прибытия (вагон пропал уже после него).
    const dateVigr = this.cfUnload() ?
      `${this.unD() || this.cfD()}T${this.unT() || this.cfT()}:00` : '';
    this.applying.set(true);
    try {
      const res = await this.api.confirmMissing(
        [...this.selected()], `${this.cfD()}T${this.cfT()}:00`,
        dateVigr, this.cfUnload() ? this.unPlace().trim() : '',
        this.cfIndex().trim(), this.tb.base());
      this.msg.success(`Прибытие подтверждено: ${res.updated} ваг. — смотрите «Историю прибывших».`);
      this.confirmOpen.set(false);
      await this.load();
      this.changed.emit();
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.applying.set(false);
    }
  }

  /** «Станция (дорога)» последней известной позиции группы. */
  station(g: MissingGroup): string {
    if (!g.station_oper) return '—';
    return g.doroga_oper ? `${g.station_oper} (${g.doroga_oper})` : g.station_oper;
  }

  /** «2026-07-15T08:49:00» → «15.07.26 08:49» (формат старой модалки). */
  fmt(ts: string | null): string {
    if (!ts || ts.length < 16) return '—';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)}.${ts.slice(2, 4)} ${ts.slice(11, 16)}`;
  }

  private datePart(ts: string | null | undefined): string {
    return ts && ts.length >= 10 ? ts.slice(0, 10) : '';
  }
  private timePart(ts: string | null | undefined): string {
    return ts && ts.length >= 16 ? ts.slice(11, 16) : '';
  }
}
