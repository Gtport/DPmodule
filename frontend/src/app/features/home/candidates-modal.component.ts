import { Component, OnInit, computed, inject, input, output, signal } from '@angular/core';
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
import { DICTS } from '../../core/auth/roles';
import { todayMsk } from '../../shared/msk-date';
import { TimeBase, TimeBaseService, mskDateInBase, shiftDateIfEvening } from '../../shared/time-base.service';
import { ArrivalsApiService, CandidateGroup, TerminalTarget } from './arrivals-api.service';
import { VagonTrailModalComponent } from './vagon-trail-modal.component';
import { MissingApiService, MissingGroup, MissingSubgroup, MissingVagon } from './missing-api.service';

/**
 * Перемещаемая модалка «Кандидаты на прибытие — <станция>» (разворот одноимённой
 * карточки): всё, что вот-вот станет «прибывшим» у терминалов станции, в двух
 * секциях. Сверху — живые кандидаты (статус 9: на станции назначения, АСУ не
 * дала дату) с «Подтвердить…»/«Скрыть»; ниже — пропавшие из дислокации (статус
 * 8: исчезли из выгрузки в незавершённом рейсе — из-за дискретности выгрузок
 * вагон мог прибыть, выгрузиться и выпасть из потока; диспетчер восстанавливает
 * факты руками: прибытие обязательно, выгрузка — опционально той же формой).
 *
 * Выбор целей у пропавших — как в «Истории прибывших»: клик по чипу вагона,
 * ПКМ по поезду/составу/вагону автовыделяет состав. Подтверждённые вагоны
 * уходят из кандидатов и появляются в «Истории прибывших» за указанную дату;
 * вернувшийся в дислокацию вагон снимается из пропавших автоматически.
 */
@Component({
  selector: 'app-candidates-modal',
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
          Кандидаты на прибытие — {{ station() }} ({{ total() }})
        </div>
      </ng-template>
      <ng-container *nzModalContent>
        <div class="bar">
          <span class="mut">Терминалы: {{ terminalNames().join(', ') }}</span>
          @if (selected().size) {
            <span class="sel-cnt">выбрано: {{ selected().size }}</span>
            @if (canEdit()) {
              <button nz-button nzType="primary" nzSize="small" (click)="openConfirmSelected()">
                Подтвердить прибытие…
              </button>
              <button nz-button nzSize="small" nz-tooltip
                      nzTooltipTitle="Скрыть из списка: запись остаётся, вернётся с новыми данными или уйдёт по сроку"
                      (click)="dismissSelected()">Скрыть</button>
            }
            @if (canDelete()) {
              <button nz-button nzDanger nzSize="small" (click)="openDeleteSelected()">Удалить…</button>
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
          <!-- Живые кандидаты (статус 9): подтверждение/отклонение оператором -->
          @if (cands().length) {
            <div class="cands">
              <div class="cands-title">
                <span nz-icon nzType="question-circle"></span>
                <b>Ждут подтверждения ({{ cands().length }})</b>
                <span class="mut">на станции назначения, АСУ не дала дату — подтвердите или скройте</span>
              </div>
              @for (c of cands(); track c.key) {
                <div class="cand">
                  <span class="num b">{{ c.index || '—' }}</span>
                  <span class="mut">{{ c.station_nach }}</span>
                  <span class="mut">({{ c.vagon_count }})</span>
                  <span class="cand-sost ell" [title]="candSostav(c)">{{ candSostav(c) }}</span>
                  <span class="mut nowrap">оп. {{ fmt(c.time_op) }}</span>
                  <span class="spacer"></span>
                  @if (canEdit()) {
                    <button nz-button nzType="primary" nzSize="small" (click)="openConfirmCand(c)">Подтвердить…</button>
                    <button nz-button nzSize="small" nz-tooltip
                            nzTooltipTitle="Скрыть до новых данных (вагоны остаются кандидатами)"
                            (click)="dismiss(c)">Скрыть</button>
                  }
                </div>
              }
            </div>
          }

          <!-- Пропавшие из дислокации (статус 8): последняя известная позиция -->
          <div class="miss-title">
            <b>Пропавшие из дислокации ({{ missingCount() }})</b>
            <span class="mut">исчезли из выгрузки в незавершённом рейсе; вероятно, прибыли — подтвердите факт</span>
          </div>
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
                    <td class="ell" [title]="stationOper(g)">{{ stationOper(g) }}</td>
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
                      <td class="c acts">
                        <button nz-button nzSize="small" nz-tooltip
                                nzTooltipTitle="Подтвердить прибытие всего состава"
                                (click)="openConfirmGroup(g)">Подтвердить…</button>
                        <button nz-button nzType="text" nzSize="small" nz-tooltip
                                nzTooltipTitle="Скрыть состав из списка (запись остаётся)"
                                (click)="dismissGroup(g)">
                          <span nz-icon nzType="eye-invisible"></span>
                        </button>
                        @if (canDelete()) {
                          <button nz-button nzType="text" nzSize="small" nzDanger nz-tooltip
                                  nzTooltipTitle="Удалить состав: рейс будет помечен «недоехавший»"
                                  (click)="openDeleteGroup(g)">
                            <span nz-icon nzType="delete"></span>
                          </button>
                        }
                      </td>
                    }
                  </tr>
                } @empty {
                  <tr><td [attr.colspan]="canEdit() ? 8 : 7" class="empty">Пропавших нет</td></tr>
                }
              </tbody>
            </table>
          </div>
        </nz-spin>

        @if (canEdit()) {
          <p class="hint">Клик по вагону — выбор; действия — кнопками на поезде или ПКМ по
            поезду/составу/вагону. «Скрыть» — временно (запись вернётся с новыми данными или уйдёт
            по сроку){{ canDelete() ? '; «Удалить» — насовсем, рейс помечается «недоехавший»' : '' }}.
            ПКМ по вагону также даёт историю движения. Вернувшийся в дислокацию вагон уходит из
            пропавших автоматически.</p>
        } @else {
          <p class="hint">Клик по вагону — история движения. Времена — московские.</p>
        }
      </ng-container>
    </nz-modal>

    <!-- ПКМ: операции по выбранным вагонам (секция пропавших) -->
    <nz-dropdown-menu #menu="nzDropdownMenu">
      <ul nz-menu>
        @if (ctxVagon(); as v) {
          <li nz-menu-item (click)="openTrail(v)">История движения вагона {{ v.vagon }}…</li>
        }
        @if (canEdit()) {
          @if (ctxVagon()) { <li nz-menu-divider></li> }
          <li nz-menu-item (click)="openConfirmSelected()">Подтвердить прибытие…</li>
          <li nz-menu-item (click)="dismissSelected()">Скрыть (до новых данных)</li>
        }
        @if (canDelete()) {
          <li nz-menu-item nzDanger (click)="openDeleteSelected()">Удалить (пометить «недоехавший»)…</li>
        }
      </ul>
    </nz-dropdown-menu>

    @if (trailVagon(); as tv) {
      <app-vagon-trail-modal [vagonId]="tv.id" [vagon]="tv.vagon" (closed)="trailVagon.set(null)" />
    }

    <!-- Диалог «Удалить из кандидатов» (senior/admin): действие необратимое для
         записи-8, поэтому с подтверждением и предпросмотром вагонов. -->
    <nz-modal [nzVisible]="deleteOpen()" [nzTitle]="delTtl" nzWidth="440px"
              (nzOnCancel)="deleteOpen.set(false)" (nzOnOk)="saveDelete()"
              nzOkText="Удалить" nzOkDanger [nzOkLoading]="applying()">
      <ng-template #delTtl>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          Удалить из кандидатов — {{ ctxGroup()?.index || 'выбранные вагоны' }}
        </div>
      </ng-template>
      <ng-container *nzModalContent>
        <div class="frm">
          <p class="mut">Вагонов: {{ selectedVagons().length }}. Записи уйдут из кандидатов
            насовсем, а рейсы в истории будут помечены «недоехавший» — не «в пути», вне
            «не выгруж.» и отчёта просрочки (фильтр «недоехавшие» — на экране истории).
            Вернувшийся в дислокацию вагон снимет пометку сам. Действие пишется в журнал.</p>
          <div class="sel-chips">
            @for (v of selectedVagons(); track v) { <span class="chip">{{ v }}</span> }
          </div>
        </div>
      </ng-container>
    </nz-modal>

    <!-- Диалог «Подтвердить прибытие»: общий для кандидатов-9 (без выгрузки)
         и пропавших-8 (прибытие обязательно, выгрузка опционально) -->
    <nz-modal [nzVisible]="confirmOpen()" [nzTitle]="cfTtl" nzWidth="440px"
              (nzOnCancel)="confirmOpen.set(false)" (nzOnOk)="saveConfirm()"
              nzOkText="Подтвердить" [nzOkDisabled]="!confirmValid()" [nzOkLoading]="applying()">
      <ng-template #cfTtl>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          Подтвердить прибытие — {{ confirmTitle() }}
        </div>
      </ng-template>
      <ng-container *nzModalContent>
        <div class="frm">
          <label>Шкала времени
            <span class="dt tbx">
              <nz-radio-group nzSize="small" nzButtonStyle="solid"
                              [ngModel]="tb.base()" (ngModelChange)="onBaseChange($event)">
                <label nz-radio-button nzValue="jd">ЖД</label>
                <label nz-radio-button nzValue="msk">МСК</label>
              </nz-radio-group>
              @if (tb.base() === 'msk') { <span class="mut">реальное московское время</span> }
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
          @if (cfMode() === 'missing') {
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
                <input nz-input list="cand-terminals" [ngModel]="unPlace()" (ngModelChange)="unPlace.set($event)"
                       placeholder="пусто — терминал назначения" />
                <datalist id="cand-terminals">
                  @for (t of terminalNames(); track t) { <option [value]="t"></option> }
                </datalist>
              </label>
            }
          }
          <p class="mut">Вагонов: {{ confirmVagons().length }}. Времена — в шкале
            {{ tb.base() === 'jd' ? 'ЖД' : 'МСК' }} (пересчёт сделает сервер). Вагоны уйдут из
            кандидатов и появятся в «Истории прибывших» за указанную дату{{ cfUnload() ? ' как выгруженные' : '' }}.</p>
          <div class="sel-chips">
            @for (v of confirmVagons(); track v) { <span class="chip">{{ v }}</span> }
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
    /* Живые кандидаты — жёлтая секция (перенос вида из «Истории прибывших»). */
    .cands { background: var(--color-warning-bg); border: 1px solid var(--color-warning);
             border-radius: var(--radius-md); padding: var(--space-xs) var(--space-sm);
             margin-bottom: var(--space-sm); display: flex; flex-direction: column; gap: 2px; }
    .cands-title { display: flex; align-items: center; gap: var(--space-sm); font-size: var(--font-size-sm); }
    .cand { display: flex; align-items: center; gap: var(--space-sm); font-size: var(--font-size-sm);
            padding: 2px 0; min-width: 0; }
    .cand .b { font-weight: 600; }
    .cand-sost { flex: 1 1 auto; min-width: 0; }
    .nowrap { white-space: nowrap; }
    .miss-title { display: flex; align-items: baseline; gap: var(--space-sm);
                  font-size: var(--font-size-sm); margin-bottom: var(--space-xs); }
    .dp-tbl td { vertical-align: top; }
    .c-idx { width: 130px; } .c-op { width: 130px; } .c-dt { width: 100px; }
    .c-days { width: 52px; } .c-act { width: 176px; }
    .acts { white-space: nowrap; }
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
    /* Только строки формы (> label): внутренние label кнопок nz-radio-button
       колонкой раскладывать нельзя — ломается ряд, скругление и цвет. */
    .frm > label { display: flex; flex-direction: column; gap: 2px; font-size: var(--font-size-sm);
                   color: var(--color-text-secondary); }
    .frm > label[nz-checkbox] { flex-direction: row; align-items: center; }
    .dt { display: flex; gap: var(--space-sm); }
    /* Переключатель ЖД/МСК: активная кнопка — белый текст, у группы скруглены
       только внешние углы. */
    .tbx { align-items: center; }
    .tbx .ant-radio-button-wrapper:first-child { border-radius: var(--radius-sm) 0 0 var(--radius-sm); }
    .tbx .ant-radio-button-wrapper:last-child { border-radius: 0 var(--radius-sm) var(--radius-sm) 0; }
    .tbx .ant-radio-button-wrapper-checked { color: var(--color-bg-surface); }
    .date { height: 26px; padding: 0 6px; border: 1px solid var(--color-border, #d9d9d9);
            border-radius: var(--radius-sm); font-size: var(--font-size-sm); color: inherit; background: transparent; }
    /* Предпросмотр затронутых вагонов (защита от батча «не тем вагонам»). */
    .sel-chips { display: flex; flex-wrap: wrap; gap: 3px; max-height: 96px; overflow: auto;
                 font-size: var(--font-size-sm); }
  `],
})
export class CandidatesModalComponent implements OnInit {
  private readonly api = inject(MissingApiService);
  private readonly arrivalsApi = inject(ArrivalsApiService);
  private readonly msg = inject(NzMessageService);
  private readonly ctxMenu = inject(NzContextMenuService);
  private readonly auth = inject(AuthService);
  /** Подтверждение/скрытие — порог operator; клиенту только просмотр и история движения. */
  readonly canEdit = this.auth.canEdit;
  /** «Удалить» (пометка «недоехавший» в истории) — senior-operator и admin. */
  readonly canDelete = computed(() => this.auth.allows(DICTS));
  /** Шкала ввода времени (ЖД/МСК) — общая для всех диалогов правок. */
  readonly tb = inject(TimeBaseService);

  /** Станция (заголовок окна) и её терминалы (фильтр naznach обеих секций). */
  readonly station = input.required<string>();
  readonly terminals = input.required<TerminalTarget[]>();

  readonly closed = output<void>();
  /** Списки изменились (подтверждение/скрытие) — карточке пора обновиться. */
  readonly changed = output<void>();

  readonly loading = signal(false);
  readonly applying = signal(false);
  /** Живые кандидаты (статус 9) — верхняя жёлтая секция. */
  readonly cands = signal<CandidateGroup[]>([]);
  /** Пропавшие (статус 8) — таблица. */
  readonly groups = signal<MissingGroup[]>([]);
  readonly search = signal('');
  /** Развёрнутые подгруппы: ключ `group.key::sub.key`. */
  readonly open = signal<Set<string>>(new Set());
  /** Выбранные пропавшие вагоны (id рейсов) — цель подтверждения. */
  readonly selected = signal<Set<string>>(new Set());
  readonly ctxGroup = signal<MissingGroup | null>(null);
  readonly ctxVagon = signal<MissingVagon | null>(null);
  readonly trailVagon = signal<MissingVagon | null>(null);

  // Диалог «Удалить из кандидатов» (пропавшие, senior/admin).
  readonly deleteOpen = signal(false);
  // Диалог «Подтвердить прибытие»: режим — живой кандидат (9) или пропавший (8).
  readonly confirmOpen = signal(false);
  readonly cfMode = signal<'cand' | 'missing'>('missing');
  readonly cfCand = signal<CandidateGroup | null>(null);
  readonly cfIndex = signal('');
  readonly cfD = signal('');
  readonly cfT = signal('');
  readonly cfUnload = signal(false);
  readonly unD = signal('');
  readonly unT = signal('');
  readonly unPlace = signal('');

  readonly terminalNames = computed(() => this.terminals().map((t) => t.name));
  readonly missingCount = computed(() => this.groups().reduce((n, g) => n + g.vagon_count, 0));
  readonly total = computed(() =>
    this.missingCount() + this.cands().reduce((n, c) => n + c.vagon_count, 0));
  readonly confirmValid = computed(() =>
    !!this.cfD() && !!this.cfT() && (!this.cfUnload() || (!!this.unD() && !!this.unT())));
  readonly confirmTitle = computed(() =>
    this.cfMode() === 'cand' ? (this.cfCand()?.index || '—') : (this.ctxGroup()?.index || 'выбранные вагоны'));
  /** Номера выбранных вагонов — предпросмотр в диалоге. */
  readonly selectedVagons = computed(() => {
    const sel = this.selected();
    const out: string[] = [];
    for (const g of this.groups())
      for (const sg of g.sub_groups)
        for (const v of sg.vagons) if (sel.has(v.id)) out.push(v.vagon);
    return out;
  });
  /** Цели открытого диалога: состав кандидата (9) либо выбранные пропавшие (8). */
  readonly confirmVagons = computed(() => {
    const c = this.cfCand();
    if (this.cfMode() === 'cand' && c) {
      return c.sub_groups.flatMap((sg) => sg.vagons.map((v) => v.vagon));
    }
    return this.selectedVagons();
  });

  ngOnInit(): void {
    void this.load();
    void this.tb.init();
  }

  async load(): Promise<void> {
    this.loading.set(true);
    try {
      const names = this.terminalNames();
      const [cands, groups] = await Promise.all([
        this.arrivalsApi.getCandidates(names),
        this.api.getMissingGroups(names),
      ]);
      this.cands.set(cands ?? []);
      this.groups.set(groups ?? []);
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

  // ── Живые кандидаты (статус 9): подтверждение / отклонение ───────────────
  candSostav(c: CandidateGroup): string {
    return c.sub_groups.map((sg) => sg.display).join(' · ') || '—';
  }

  private candVagonIds(c: CandidateGroup): string[] {
    return c.sub_groups.flatMap((sg) => sg.vagons.map((v) => v.id));
  }

  openConfirmCand(c: CandidateGroup): void {
    this.cfMode.set('cand');
    this.cfCand.set(c);
    this.openConfirmDialog(c.index, c.time_op);
  }

  async dismiss(c: CandidateGroup): Promise<void> {
    try {
      const res = await this.arrivalsApi.dismissCandidates(this.candVagonIds(c));
      this.msg.info(`Скрыто кандидатов: ${res.updated} ваг. (до новых данных АСУ).`);
      await this.load();
      this.changed.emit();
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    }
  }

  // ── Пропавшие (статус 8): скрытие и удаление ─────────────────────────────
  /** id выбранных пропавших; пусто — подсказка (цели действий бара и ПКМ). */
  private selectedIdsOrHint(): string[] | null {
    if (!this.selected().size) {
      this.msg.info('Сначала выберите вагоны (клик по вагону или ПКМ по поезду/составу).');
      return null;
    }
    return [...this.selected()];
  }

  /** «Скрыть» выбранных: запись-8 остаётся, из списков уходит (до возврата вагона/TTL). */
  async dismissSelected(): Promise<void> {
    const ids = this.selectedIdsOrHint();
    if (!ids) return;
    try {
      const res = await this.api.dismissMissing(ids);
      this.msg.info(`Скрыто пропавших: ${res.updated} ваг. (вернутся с новыми данными или уйдут по сроку).`);
      await this.load();
      this.changed.emit();
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    }
  }

  dismissGroup(g: MissingGroup): void {
    this.selected.set(new Set(g.sub_groups.flatMap((sg) => sg.vagons.map((v) => v.id))));
    void this.dismissSelected();
  }

  openDeleteSelected(): void {
    if (!this.selectedIdsOrHint()) return;
    this.deleteOpen.set(true);
  }

  openDeleteGroup(g: MissingGroup): void {
    this.ctxGroup.set(g);
    this.selected.set(new Set(g.sub_groups.flatMap((sg) => sg.vagons.map((v) => v.id))));
    this.deleteOpen.set(true);
  }

  /** «Удалить» выбранных: запись-8 насовсем + пометка «недоехавший» в истории. */
  async saveDelete(): Promise<void> {
    this.applying.set(true);
    try {
      const res = await this.api.deleteMissing([...this.selected()]);
      this.msg.success(`Удалено из кандидатов: ${res.updated} ваг. — в истории помечены «недоехавший».`);
      this.deleteOpen.set(false);
      await this.load();
      this.changed.emit();
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.applying.set(false);
    }
  }

  // ── Пропавшие (статус 8): подтверждение прибытия ─────────────────────────
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
    this.cfMode.set('missing');
    this.cfCand.set(null);
    const g = this.ctxGroup();
    this.openConfirmDialog(g?.index ?? '', g?.time_op ?? null);
  }

  /** Общий дефолт диалога: индекс поезда и время последней операции (МСК-штамп
   *  показываем в текущей шкале), поля выгрузки чистые. */
  private openConfirmDialog(index: string, timeOp: string | null): void {
    this.cfIndex.set(index);
    const d = this.datePart(timeOp) || todayMsk();
    const t = this.timePart(timeOp) || '00:00';
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
    this.applying.set(true);
    try {
      const dt = `${this.cfD()}T${this.cfT()}:00`;
      const c = this.cfCand();
      if (this.cfMode() === 'cand' && c) {
        const res = await this.arrivalsApi.confirmArrival(
          this.candVagonIds(c), dt, this.cfIndex().trim(), this.tb.base());
        this.msg.success(`Прибытие подтверждено: ${res.updated} ваг. Поезд ушёл в прибывшие.`);
      } else {
        // Дефолт выгрузки — момент прибытия (вагон пропал уже после него).
        const dateVigr = this.cfUnload() ?
          `${this.unD() || this.cfD()}T${this.unT() || this.cfT()}:00` : '';
        const res = await this.api.confirmMissing(
          [...this.selected()], dt,
          dateVigr, this.cfUnload() ? this.unPlace().trim() : '',
          this.cfIndex().trim(), this.tb.base());
        this.msg.success(`Прибытие подтверждено: ${res.updated} ваг. — смотрите «Историю прибывших».`);
      }
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
  stationOper(g: MissingGroup): string {
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
