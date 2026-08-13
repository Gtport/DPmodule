import { Component, OnInit, WritableSignal, computed, inject, signal } from '@angular/core';
import { DragDropModule } from '@angular/cdk/drag-drop';
import { FormsModule } from '@angular/forms';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzDropDownModule, NzContextMenuService, NzDropdownMenuComponent } from 'ng-zorro-antd/dropdown';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { todayMsk } from '../../shared/msk-date';
import { VagonTrailModalComponent } from '../home/vagon-trail-modal.component';
import { Train, TrainSubgroup, TrainVagon, TrainsApiService, TrainsTarget } from './trains-api.service';

/** Режим «Управление»: все / ходовые (есть прогноз) / отцепки (нет прогноза). */
type RunMode = 'all' | 'running' | 'detached';
/** Режим «Прибытие»: все / в пути / прибывшие (на станции назначения). */
type ArrivalMode = 'all' | 'enroute' | 'arrived';

/** Статусы DPmodule (docs/STATUSES.md) — в gtport их было меньше (0,1,2,4,5,10). */
const STATUS_LABELS: Record<number, string> = {
  0: 'на отправлении (Б/И)',
  1: 'на станции отправления',
  2: 'в пути',
  4: 'долгий простой',
  5: 'брошен',
  6: 'порожний в пути',
  9: 'кандидат в прибывшие',
  10: 'прибыл',
  12: 'выгружен',
};
const STATUS_COLORS: Record<number, string> = {
  0: '#1890ff', 1: '#1890ff', 2: '#1890ff',
  4: '#faad14', 5: '#ff4d4f', 6: '#8c8c8c',
  9: '#73d13d', 10: '#52c41a', 12: '#52c41a',
};

/** Плоский результат поиска по вагонам/накладным (режим-карточки, как в gtport). */
interface VagonHit {
  train: Train;
  sub: TrainSubgroup;
  vagon: TrainVagon;
}

/**
 * Экран «Поезда в движении» — перенос gtport OperatorToolsDislocation.tsx.
 * Дерево «поезд → подгруппа отправки → вагоны» по всему снимку; фильтры-чипы
 * (управление/прибытие/терминалы/статус) и поиски (индекс, станция погрузки,
 * станция дислокации, груз, списки вагонов и накладных) — на клиенте; Excel
 * «вся таблица» и «Натурный лист» поезда; ПКМ по вагону — «История движения
 * вагона».
 *
 * Отходы от gtport (решения владельца 31.07.2026):
 * - терминалы фильтров — из реестра ports (в gtport — хардкод АЭ/ГУТ-2/УТ-1
 *   с цветами); выбраны все чипы = фильтр выключен (видны и нетерминальные
 *   назначения, например ВП — в gtport таких данных не было);
 * - статус поезда — честный агрегат по вагонам (см. service/trains.go),
 *   статусов больше (6/9/12 — docs/STATUSES.md), «прибывшие» = 9/10/12;
 * - собственник = owner (в gtport колонку заполнял род вагона), марка груза =
 *   freight_exact_name (просьба владельца), род вагона показан отдельно;
 * - «История движения вагона» по ПКМ — новая возможность (в gtport не было);
 * - поиск по станции дислокации (station_oper поезда) — новая возможность
 *   (решение владельца 14.08.2026; в gtport искали только станцию погрузки);
 * - «Детали поезда» — перемещаемая модалка (канон проекта), а не боковая
 *   панель gtport; открывается иконкой ⓘ на строке и из ПКМ.
 */
@Component({
  selector: 'app-trains',
  imports: [
    DragDropModule, FormsModule, NzButtonModule, NzIconModule, NzInputModule,
    NzModalModule, NzTagModule, NzTooltipModule, NzDropDownModule,
    VagonTrailModalComponent,
  ],
  template: `
    <div class="page">
      <!-- ── Шапка ─────────────────────────────────────────────────────── -->
      <div class="bar head">
        <b>Поезда в движении</b>
        <button nz-button nzSize="small" nzDanger (click)="resetAll()">
          <span nz-icon nzType="clear"></span> Сброс
        </button>
        <span class="spacer"></span>
        <span class="mut">
          @if (searchMode()) {
            найдено вагонов: <b>{{ hits().length }}</b>
          } @else {
            показано: <b>{{ shownTrains().length }}</b> поездов,
            <b>{{ shownVagons() }}</b> вагонов (в снимке {{ total() }} поездов)
          }
        </span>
        <button nz-button nzSize="small" (click)="exportAll()" nz-tooltip="Экспорт в Excel"
                [disabled]="searchMode() ? !hits().length : !shownTrains().length">
          <span nz-icon nzType="download"></span>
        </button>
        <button nz-button nzSize="small" (click)="load()" [nzLoading]="loading()" nz-tooltip="Обновить">
          <span nz-icon nzType="sync"></span>
        </button>
      </div>

      <!-- ── Фильтры-чипы ──────────────────────────────────────────────── -->
      <div class="filters">
        <div class="fblock">
          <div class="ftitle">Управление</div>
          <nz-tag [class.on]="runMode() === 'all'" (click)="runMode.set('all')">Все</nz-tag>
          <nz-tag [class.on]="runMode() === 'running'" (click)="runMode.set('running')">Ходовые</nz-tag>
          <nz-tag [class.on]="runMode() === 'detached'" (click)="runMode.set('detached')">Отцепки</nz-tag>
        </div>
        <div class="fblock">
          <div class="ftitle">Прибытие</div>
          <nz-tag [class.on]="arrivalMode() === 'all'" (click)="arrivalMode.set('all')">Все</nz-tag>
          <nz-tag [class.on]="arrivalMode() === 'enroute'" (click)="arrivalMode.set('enroute')">В пути</nz-tag>
          <nz-tag [class.on]="arrivalMode() === 'arrived'" (click)="arrivalMode.set('arrived')">Прибывшие</nz-tag>
        </div>
        <div class="fblock">
          <div class="ftitle">Грузополучатель</div>
          @for (t of targets(); track t.name) {
            <!-- Именно background-color: nz-tag сам биндит это свойство на хосте
                 и при первой отрисовке затирал бы шорткат background. -->
            <nz-tag [class.on]="selGruzpol().has(t.name)"
                    [style.background-color]="selGruzpol().has(t.name) ? (t.color || undefined) : undefined"
                    (click)="toggle(selGruzpol, t.name)">{{ t.name }}</nz-tag>
          }
        </div>
        <div class="fblock">
          <div class="ftitle">Назначение</div>
          @for (t of targets(); track t.name) {
            <nz-tag [class.on]="selNaznach().has(t.name)"
                    [style.background-color]="selNaznach().has(t.name) ? (t.color || undefined) : undefined"
                    (click)="toggle(selNaznach, t.name)">{{ t.name }}</nz-tag>
          }
        </div>
        <div class="fblock">
          <div class="ftitle">Показать только</div>
          <nz-tag [class.on]="withPlan()" (click)="withPlan.set(!withPlan())">В плане</nz-tag>
          <nz-tag [class.on]="broshen()" class="warn" (click)="broshen.set(!broshen())">Брошенные</nz-tag>
        </div>
      </div>

      <!-- ── Поиск ─────────────────────────────────────────────────────── -->
      <div class="bar search-bar">
        <input nz-input nzSize="small" class="w-idx" placeholder="индекс: 786 или 9379-786-9857"
               maxlength="13" [ngModel]="indexSearch()" (ngModelChange)="setIndexSearch($event)" />
        <input nz-input nzSize="small" class="w-st" placeholder="станция погрузки"
               [ngModel]="stationSearch()" (ngModelChange)="stationSearch.set($event)" />
        <input nz-input nzSize="small" class="w-st" placeholder="станция дислокации"
               [ngModel]="operStationSearch()" (ngModelChange)="operStationSearch.set($event)" />
        <input nz-input nzSize="small" class="w-st" placeholder="груз / группа груза"
               [ngModel]="cargoSearch()" (ngModelChange)="cargoSearch.set($event)" />
        <span class="grp">
          <input nz-input nzSize="small" class="w-list" placeholder="список вагонов: 62158654…"
                 [ngModel]="vagonQuery()" (ngModelChange)="vagonQuery.set($event)"
                 (keyup.enter)="runVagonSearch()" />
          <button nz-button nzType="primary" nzSize="small" [disabled]="!vagonQuery().trim()"
                  (click)="runVagonSearch()">Искать</button>
        </span>
        <span class="grp">
          <input nz-input nzSize="small" class="w-list" placeholder="список накладных: ЭЛ318859…"
                 [ngModel]="invoiceQuery()" (ngModelChange)="invoiceQuery.set($event)"
                 (keyup.enter)="runInvoiceSearch()" />
          <button nz-button nzType="primary" nzSize="small" [disabled]="!invoiceQuery().trim()"
                  (click)="runInvoiceSearch()">Искать</button>
        </span>
        @if (searchMode()) {
          <button nz-button nzSize="small" (click)="clearListSearch()">
            <span nz-icon nzType="close"></span> К списку поездов
          </button>
        }
      </div>

      <!-- ── Результаты поиска по вагонам/накладным (плоские карточки) ─── -->
      @if (searchMode()) {
        <div class="tree-wrap">
          @for (h of hits(); track h.vagon.id) {
            <div class="hit" (contextmenu)="openVagonMenu($event, h.train, h.vagon, vagonMenu)">
              <div class="row">
                <b class="blue">{{ h.vagon.vagon }}</b>
                <span class="mut">накл. {{ h.vagon.invoice || '—' }}</span>
                <nz-tag class="thin">{{ h.train.index || '—' }}</nz-tag>
                <span nz-icon nzType="environment" nzTheme="fill" class="pin"
                      [style.color]="statusColor(h.train.status)"></span>
                <span>{{ h.train.station_oper }}@if (h.train.doroga_oper) { <span class="mut"> ({{ h.train.doroga_oper }})</span> }</span>
                @if (h.train.time_op) { <span class="mut">{{ fmtDT(h.train.time_op) }}</span> }
                @if (h.train.oper_s) { <nz-tag class="thin">{{ h.train.oper_s }}</nz-tag> }
                <span class="spacer"></span>
                <span [style.color]="statusColor(h.train.status)">{{ statusLabel(h.train.status) }}</span>
              </div>
              <div class="row">
                <nz-tag class="thin">{{ h.sub.index_main || '—' }}</nz-tag>
                <span nz-icon nzType="train" nzTheme="fill" class="pin train-ic"></span>
                <span>{{ h.sub.station_nach }}</span>
                @if (h.sub.gruzotpr) { <span class="mut ell" [title]="h.sub.gruzotpr">{{ h.sub.gruzotpr }}</span> }
                <span><b>{{ h.sub.gruzpol_s || '—' }}</b> → <b>{{ h.sub.naznach || '—' }}</b></span>
                @if (h.vagon.freight) { <nz-tag class="thin">{{ h.vagon.freight }}</nz-tag> }
                @if (h.vagon.ves != null) { <span class="mut">{{ h.vagon.ves.toFixed(1) }} т</span> }
                @if (h.vagon.owner) { <span class="mut ell" [title]="'собственник'">{{ h.vagon.owner }}</span> }
              </div>
              <div class="row">
                @if (h.vagon.date_nach) { <span class="mut">погрузка: {{ fmtD(h.vagon.date_nach) }}</span> }
                @if (h.vagon.date_dostav) {
                  <span [class.overdue]="isOverdue(h.vagon.date_dostav)">
                    срок доставки: {{ fmtD(h.vagon.date_dostav) }}@if (isOverdue(h.vagon.date_dostav)) { (просрочен) }
                  </span>
                }
                @if (h.train.plan_jd) { <span class="mut">план: {{ fmtDT(h.train.plan_jd) }}</span> }
                @if (h.train.prog_jd) { <span class="mut">прогноз: {{ fmtDT(h.train.prog_jd) }}</span> }
                @if (h.train.rasch_jd) { <span class="mut">расчёт: {{ fmtDT(h.train.rasch_jd) }}</span> }
                @if (prostText(h.train)) {
                  <span [class.overdue]="prostHours(h.train) > 48" class="prost">простой: {{ prostText(h.train) }}</span>
                }
              </div>
            </div>
          }
          @if (!hits().length) { <div class="empty">Ничего не найдено — проверьте номера</div> }
        </div>
      } @else {
        <!-- ── Дерево «поезд → подгруппа → вагоны» ─────────────────────── -->
        <div class="tree-wrap">
          @for (t of shownTrains(); track t.key) {
            <div class="group">
              <div class="row g-row" (contextmenu)="openTrainMenu($event, t, trainMenu)">
                <span class="toggle" (click)="flip(t.key)">
                  <span nz-icon [nzType]="open().has(t.key) ? 'down' : 'right'"></span>
                  <span nz-icon nzType="train" nzTheme="fill" class="train-ic"
                        [style.color]="statusColor(t.status)"></span>
                  <b>{{ t.index || '—' }}</b>
                </span>
                <span nz-icon nzType="environment" nzTheme="fill" class="pin mut"></span>
                <span class="ell">{{ t.station_oper }}@if (t.doroga_oper) { <span class="mut"> ({{ t.doroga_oper }})</span> }</span>
                @if (t.rasst != null) { <span class="mut nowrap">{{ t.rasst }} км</span> }
                <nz-tag class="thin" [style.color]="statusColor(t.status)"
                        [style.borderColor]="statusColor(t.status)">{{ statusLabel(t.status) }}</nz-tag>
                @if (t.oper_s) { <span class="mut nowrap" [nz-tooltip]="t.time_op ? fmtDT(t.time_op) : ''">{{ t.oper_s }}</span> }
                @if (t.plan_jd) { <span class="mut nowrap" nz-tooltip="План подвода">📅 {{ fmtDT(t.plan_jd) }}</span> }
                @if (t.prog_jd) { <span class="mut nowrap" nz-tooltip="Прогноз (нитка порта)">⏱ {{ fmtDT(t.prog_jd) }}</span> }
                @if (!t.prog_jd && t.rasch_jd && !t.arrived) {
                  <span class="mut nowrap" nz-tooltip="Расчёт по времени хода">🧮 {{ fmtDT(t.rasch_jd) }}</span>
                }
                @if (prostText(t)) {
                  <span class="nowrap prost" [class.overdue]="prostHours(t) > 48"
                        nz-tooltip="Простой (максимум по вагонам)">{{ prostText(t) }}</span>
                }
                <span class="spacer"></span>
                <span class="mut nowrap">{{ t.vagon_count }} ваг. · {{ t.ves.toFixed(0) }} т</span>
                <span nz-icon nzType="info-circle" class="info-btn" nz-tooltip="Детали поезда"
                      (click)="detailTrain.set(t); $event.stopPropagation()"></span>
              </div>
              <div class="compose mut ell">
                @for (sg of t.sub_groups; track sg.key; let last = $last) {
                  {{ sg.index_main || '—' }} | {{ sg.station_nach }} {{ sg.gruzpol_s }} → {{ sg.naznach }} ({{ sg.vagon_count }})@if (!last) { <b> • </b> }
                }
              </div>

              @if (open().has(t.key)) {
                @for (sg of t.sub_groups; track sg.key) {
                  <div class="sub">
                    <div class="row" (click)="flip(t.key + '::' + sg.key)">
                      <span class="toggle">
                        <span nz-icon [nzType]="open().has(t.key + '::' + sg.key) ? 'down' : 'right'"></span>
                        <b>{{ sg.index_main || '—' }}</b>
                      </span>
                      <span class="dot" [style.background]="targetColor(sg.gruzpol_s)"></span>
                      <span class="ell">{{ sg.station_nach }}</span>
                      <span class="nowrap">{{ sg.gruzpol_s || '—' }} → {{ sg.naznach || '—' }}</span>
                      @if (sg.cargo_s) { <span class="mut ell">{{ sg.cargo_s }}</span> }
                      @if (sg.gruzotpr) { <span class="mut ell" [title]="'отправитель'">{{ sg.gruzotpr }}</span> }
                      @if (sg.client) { <span class="mut ell" [title]="'клиент'">{{ sg.client }}</span> }
                      @if (sg.shipments) { <span class="mut ell" [title]="'отгрузки'">📦 {{ sg.shipments }}</span> }
                      <span class="spacer"></span>
                      <span class="mut nowrap">{{ sg.vagon_count }} ваг. · {{ sg.ves.toFixed(0) }} т</span>
                    </div>
                    @if (open().has(t.key + '::' + sg.key)) {
                      @for (v of sg.vagons; track v.id) {
                        <div class="row vagon" (contextmenu)="openVagonMenu($event, t, v, vagonMenu)">
                          <span class="mut num">{{ v.npp_vag ?? '·' }}</span>
                          <b class="blue">{{ v.vagon }}</b>
                          <span class="mut">накл. {{ v.invoice || '—' }}</span>
                          @if (v.rod_vag) { <nz-tag class="thin">{{ v.rod_vag }}</nz-tag> }
                          @if (v.freight) { <span class="ell" [title]="'марка груза'">{{ v.freight }}</span> }
                          @if (v.ves != null) { <span class="mut nowrap">{{ v.ves.toFixed(1) }} т</span> }
                          @if (v.date_nach) { <span class="mut nowrap" nz-tooltip="Дата погрузки">{{ fmtD(v.date_nach) }}</span> }
                          @if (v.date_dostav) {
                            <span class="nowrap" [class.overdue]="isOverdue(v.date_dostav)" [class.ok]="!isOverdue(v.date_dostav)"
                                  nz-tooltip="Срок доставки">{{ fmtD(v.date_dostav) }}@if (isOverdue(v.date_dostav)) { !}</span>
                          }
                          <span class="spacer"></span>
                          @if (v.owner) { <span class="mut ell" [title]="'собственник: ' + v.owner">{{ v.owner }}</span> }
                        </div>
                      }
                    }
                  </div>
                }
              }
            </div>
          }
          @if (!shownTrains().length && !loading()) {
            <div class="empty">Ничего не найдено — измените фильтры или поиск</div>
          }
        </div>
      }

      <!-- ── ПКМ по поезду ─────────────────────────────────────────────── -->
      <nz-dropdown-menu #trainMenu="nzDropdownMenu">
        <ul nz-menu>
          <li nz-menu-item (click)="detailTrain.set(ctxTrain())">Детали поезда…</li>
          <li nz-menu-item (click)="exportTrain(ctxTrain())">Натурный лист (Excel)</li>
        </ul>
      </nz-dropdown-menu>

      <!-- ── ПКМ по вагону ─────────────────────────────────────────────── -->
      <nz-dropdown-menu #vagonMenu="nzDropdownMenu">
        <ul nz-menu>
          <li nz-menu-item (click)="openTrail()">История движения вагона {{ ctxVagon()?.vagon }}…</li>
          <li nz-menu-item (click)="detailTrain.set(ctxTrain())">Детали поезда…</li>
          <li nz-menu-item (click)="exportTrain(ctxTrain())">Натурный лист поезда (Excel)</li>
        </ul>
      </nz-dropdown-menu>

      <!-- ── Модалка «Детали поезда» (перенос gtport DetailPanel) ──────── -->
      <nz-modal [nzVisible]="detailTrain() !== null" [nzTitle]="dtTtl" [nzFooter]="null"
                nzWidth="480px" (nzOnCancel)="detailTrain.set(null)">
        <ng-template #dtTtl>
          <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
            Детали поезда {{ detailTrain()?.index }}
            <button nz-button nzSize="small" class="dt-export" nz-tooltip="Натурный лист (Excel)"
                    (click)="exportTrain(detailTrain())">
              <span nz-icon nzType="download"></span>
            </button>
          </div>
        </ng-template>
        <ng-container *nzModalContent>
          @if (detailTrain(); as dt) {
            <div class="dt-grid">
              <div><div class="ftitle">Индекс</div>{{ dt.index || '—' }}</div>
              <div><div class="ftitle">Станция операции</div>
                {{ dt.station_oper }}@if (dt.doroga_oper) { <span class="mut">({{ dt.doroga_oper }})</span> }</div>
              <div><div class="ftitle">Станция назначения</div>{{ dt.stan_nazn || '—' }}</div>
              <div><div class="ftitle">Расстояние</div>{{ dt.rasst ?? '—' }} км</div>
              <div><div class="ftitle">Статус</div>
                <span [style.color]="statusColor(dt.status)">{{ statusLabel(dt.status) }}</span></div>
              <div><div class="ftitle">Состав</div>{{ dt.vagon_count }} ваг. · {{ dt.ves.toFixed(1) }} т</div>
              @if (prostText(dt)) {
                <div><div class="ftitle">Макс. простой</div>
                  <span class="prost" [class.overdue]="prostHours(dt) > 48">{{ prostText(dt) }}</span></div>
              }
            </div>

            @if (dt.time_op || dt.oper_s) {
              <div class="dt-box">
                <div class="ftitle">Последняя операция</div>
                {{ dt.oper_s || '—' }}@if (dt.time_op) { <span class="mut"> · {{ fmtDT(dt.time_op) }}</span> }
              </div>
            }

            @if (dt.plan_jd || dt.rasch_jd || dt.prog_jd) {
              <div class="dt-box">
                <div class="ftitle">Даты (ЖД)</div>
                @if (dt.plan_jd) { <div>📅 План подвода: <b>{{ fmtDT(dt.plan_jd) }}</b></div> }
                @if (dt.prog_jd) { <div>⏱ Прогноз (нитка порта): <b>{{ fmtDT(dt.prog_jd) }}</b></div> }
                @if (dt.rasch_jd) { <div>🧮 Расчёт по времени хода: <b>{{ fmtDT(dt.rasch_jd) }}</b></div> }
              </div>
            }

            <div class="ftitle dt-sub">Состав ({{ dt.vagon_count }} вагонов)</div>
            @for (sg of dt.sub_groups; track sg.key) {
              <div class="dt-box">
                <b>{{ sg.index_main || '—' }}</b> | {{ sg.station_nach }}
                <div class="mut">{{ sg.gruzpol_s || '—' }} → {{ sg.naznach || '—' }} · {{ sg.vagon_count }} ваг.</div>
                @if (sg.cargo_s) { <div>Груз: {{ sg.cargo_s }} ({{ sg.ves.toFixed(1) }} т)</div> }
                @if (sg.gruzotpr) { <div class="mut">Отправитель: {{ sg.gruzotpr }}</div> }
                @if (sg.client) { <div class="mut">Клиент: {{ sg.client }}</div> }
                @if (sg.shipments) { <div class="mut">Отгрузки: {{ sg.shipments }}</div> }
              </div>
            }
          }
        </ng-container>
      </nz-modal>

      @if (trailVagon(); as tv) {
        <app-vagon-trail-modal [vagonId]="tv.id" [vagon]="tv.vagon" (closed)="trailVagon.set(null)" />
      }
    </div>
  `,
  styles: [`
    .page { display: flex; flex-direction: column; gap: var(--space-sm); }
    .bar { display: flex; align-items: center; gap: var(--space-sm); flex-wrap: wrap; }
    .bar.head { padding: var(--space-xs) var(--space-md); background: var(--color-bg-surface);
                border-radius: var(--radius-card); box-shadow: var(--shadow-card); }
    .spacer { flex: 1 1 auto; }
    .mut { color: var(--color-text-secondary); font-size: var(--font-size-sm); }
    .empty { color: var(--color-text-secondary); font-size: var(--font-size-sm); text-align: center; padding: 14px 0; }
    .nowrap { white-space: nowrap; }
    .ell { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .blue { color: var(--color-primary, #1677ff); font-variant-numeric: tabular-nums; }
    /* Фильтры-чипы */
    .filters { display: flex; align-items: flex-start; gap: var(--space-lg); flex-wrap: wrap;
               padding: var(--space-xs) var(--space-md); background: var(--color-bg-surface);
               border-radius: var(--radius-card); box-shadow: var(--shadow-card); }
    /* Подписи фильтров и границы контролов заметнее (замечание владельца
       14.08.2026: на светлом фоне терялись). */
    .ftitle { font-size: var(--font-size-sm); color: var(--color-text); font-weight: 600; margin-bottom: 2px; }
    .fblock nz-tag, .fblock .ant-tag { cursor: pointer; user-select: none; }
    .filters ::ng-deep .ant-tag, .search-bar ::ng-deep .ant-input {
      border-color: var(--color-text-muted);
    }
    nz-tag.on { border-color: var(--color-primary, #1677ff); background: var(--color-primary-bg, #e6f4ff); }
    nz-tag.warn.on { border-color: var(--color-error, #ff4d4f); background: var(--color-error-bg, #fff1f0); }
    /* Поиск */
    .search-bar { padding: var(--space-xs) var(--space-md); background: var(--color-bg-surface);
                  border-radius: var(--radius-card); box-shadow: var(--shadow-card); }
    .grp { display: inline-flex; gap: 4px; }
    .w-idx { width: 190px; } .w-st { width: 180px; } .w-list { width: 230px; }
    /* Дерево */
    .tree-wrap { max-height: 74vh; overflow: auto; background: var(--color-bg-surface);
                 border-radius: var(--radius-card); box-shadow: var(--shadow-card); }
    .group { border-bottom: 1px solid var(--color-border, #f0f0f0); }
    .row { display: flex; align-items: center; gap: 8px; padding: 4px 8px; font-size: var(--font-size-sm); min-width: 0; }
    .g-row { background: var(--color-bg-container-secondary, #fafafa); cursor: context-menu; }
    .g-row:hover { background: var(--color-primary-bg, #e6f4ff); }
    .compose { padding: 0 8px 4px 30px; font-size: var(--font-size-sm);
               background: var(--color-bg-container-secondary, #fafafa); }
    .toggle { cursor: pointer; display: inline-flex; align-items: center; gap: 6px; white-space: nowrap; }
    .train-ic { font-size: 14px; color: #1890ff; }
    .pin { font-size: 12px; }
    .dot { width: 12px; height: 12px; border-radius: 50%; border: 1px solid var(--color-border, #d9d9d9);
           flex: 0 0 auto; }
    .sub { margin-left: 26px; border-top: 1px dashed var(--color-border, #f0f0f0); }
    .sub > .row { cursor: pointer; }
    .sub > .row:hover { background: var(--color-bg-container-secondary, #fafafa); }
    .vagon { margin-left: 26px; border-top: 1px dotted var(--color-border, #f0f0f0); cursor: context-menu; }
    .vagon:hover { background: var(--color-bg-container-secondary, #fafafa); }
    .num { min-width: 18px; text-align: right; font-variant-numeric: tabular-nums; }
    nz-tag.thin, .thin { margin: 0; line-height: 16px; padding: 0 5px; font-size: var(--font-size-sm); }
    .overdue { color: var(--color-error, #ff4d4f); font-weight: 500; }
    .ok { color: var(--color-success, #52c41a); }
    .prost { color: var(--color-warning, #faad14); }
    .prost.overdue { color: var(--color-error, #ff4d4f); }
    /* Карточки результата поиска */
    .hit { border-bottom: 1px solid var(--color-warning-border, #ffe58f);
           background: var(--color-warning-bg, #fffbe6); padding: 4px 0; cursor: context-menu; }
    .hit .row { padding: 2px 10px; }
    /* Модалка «Детали поезда» */
    .ttl { cursor: move; user-select: none; }
    .dt-export { margin-left: 10px; }
    .info-btn { cursor: pointer; color: var(--color-text-secondary); font-size: 14px; }
    .info-btn:hover { color: var(--color-primary, #1677ff); }
    .dt-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 8px 16px; margin-bottom: 10px; }
    .dt-box { background: var(--color-bg-container-secondary, #fafafa); border-radius: 6px;
              padding: 6px 10px; margin-bottom: 8px; font-size: var(--font-size-sm); }
    .dt-sub { margin: 6px 0 4px; }
  `],
})
export class TrainsComponent implements OnInit {
  private readonly api = inject(TrainsApiService);
  private readonly msg = inject(NzMessageService);
  private readonly ctxMenu = inject(NzContextMenuService);

  readonly loading = signal(false);
  readonly trains = signal<Train[]>([]);
  readonly targets = signal<TrainsTarget[]>([]);
  readonly total = signal(0);
  readonly open = signal<Set<string>>(new Set());

  // Фильтры (все — клиентские, как в gtport).
  readonly runMode = signal<RunMode>('all');
  readonly arrivalMode = signal<ArrivalMode>('all');
  readonly selGruzpol = signal<Set<string>>(new Set());
  readonly selNaznach = signal<Set<string>>(new Set());
  readonly withPlan = signal(false);
  readonly broshen = signal(false);

  // Поиски. Индекс/станции/груз фильтруют дерево; вагоны/накладные — режим карточек.
  readonly indexSearch = signal('');
  readonly stationSearch = signal('');
  readonly operStationSearch = signal('');
  readonly cargoSearch = signal('');
  readonly vagonQuery = signal('');
  readonly invoiceQuery = signal('');
  readonly searchMode = signal<'vagon' | 'invoice' | null>(null);

  // Контекст ПКМ и модалка истории вагона.
  readonly ctxTrain = signal<Train | null>(null);
  readonly ctxVagon = signal<TrainVagon | null>(null);
  readonly trailVagon = signal<TrainVagon | null>(null);
  /** Поезд в модалке «Детали поезда» (перенос gtport DetailPanel). */
  readonly detailTrain = signal<Train | null>(null);

  ngOnInit(): void {
    void this.load();
  }

  async load(): Promise<void> {
    this.loading.set(true);
    try {
      const data = await this.api.getTrains();
      this.trains.set(data.trains ?? []);
      this.targets.set(data.targets ?? []);
      this.total.set(data.total ?? 0);
      // По умолчанию выбраны все терминалы (= фильтр выключен).
      if (!this.selGruzpol().size) this.selGruzpol.set(new Set(this.targets().map((t) => t.name)));
      if (!this.selNaznach().size) this.selNaznach.set(new Set(this.targets().map((t) => t.name)));
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.loading.set(false);
    }
  }

  // ── Цепочка фильтров (порядок gtport) ────────────────────────────────────
  readonly shownTrains = computed(() => {
    // (1) Точечный поиск по индексу — приоритетный, по всему снимку:
    // 3 цифры = номер поезда (середина индекса), 11 цифр = полный индекс 4-3-4
    // (дефисы при вводе не обязательны).
    const idx = this.indexSearch().replace(/\D/g, '');
    if (idx.length === 3) {
      return this.trains().filter((t) =>
        this.midDigits(t.index) === idx || t.sub_groups.some((sg) => this.midDigits(sg.index_main) === idx));
    }
    if (idx.length === 11) {
      return this.trains().filter((t) =>
        this.allDigits(t.index) === idx || t.sub_groups.some((sg) => this.allDigits(sg.index_main) === idx));
    }

    let list = this.trains();

    // (2) Управление: ходовые (есть прогноз) / отцепки.
    if (this.runMode() === 'running') list = list.filter((t) => !!t.prog_jd);
    else if (this.runMode() === 'detached') list = list.filter((t) => !t.prog_jd);

    // Прибытие: на станции назначения (9/10/12) или в пути.
    if (this.arrivalMode() === 'enroute') list = list.filter((t) => !t.arrived);
    else if (this.arrivalMode() === 'arrived') list = list.filter((t) => t.arrived);

    // Терминалы: выбраны все чипы = фильтр выключен (видны и нетерминальные
    // назначения вроде ВП); подмножество — строгий отбор по подгруппам.
    const all = this.targets().length;
    const gp = this.selGruzpol();
    if (all && gp.size < all) list = list.filter((t) => t.sub_groups.some((sg) => gp.has(sg.gruzpol_s)));
    const nz = this.selNaznach();
    if (all && nz.size < all) list = list.filter((t) => t.sub_groups.some((sg) => nz.has(sg.naznach)));

    // Статус: «В плане» / «Брошенные» — по ИЛИ, если включены.
    if (this.withPlan() || this.broshen()) {
      list = list.filter((t) => (this.withPlan() && t.has_plan) || (this.broshen() && t.broshen));
    }

    // (3) Станция погрузки — поверх фильтров.
    const st = this.stationSearch().trim().toUpperCase();
    if (st) list = list.filter((t) => t.sub_groups.some((sg) => sg.station_nach.toUpperCase().includes(st)));

    // (4) Станция дислокации (где поезд сейчас) — поле поезда, не подгрупп.
    const so = this.operStationSearch().trim().toUpperCase();
    if (so) list = list.filter((t) => t.station_oper.toUpperCase().includes(so));

    // (5) Груз — поверх станций (cargo_s / cargo_group / марка вагона).
    const cg = this.cargoSearch().trim().toUpperCase();
    if (cg) {
      list = list.filter((t) => t.sub_groups.some((sg) =>
        sg.cargo_s.toUpperCase().includes(cg) || sg.cargo_group.toUpperCase().includes(cg)
        || sg.vagons.some((v) => v.freight.toUpperCase().includes(cg))));
    }
    return list;
  });

  readonly shownVagons = computed(() => this.shownTrains().reduce((n, t) => n + t.vagon_count, 0));

  /** Плоские результаты режима поиска по вагонам/накладным (по всему снимку). */
  readonly hits = computed((): VagonHit[] => {
    const mode = this.searchMode();
    if (!mode) return [];
    const out: VagonHit[] = [];
    if (mode === 'vagon') {
      const nums = new Set(this.vagonQuery().match(/\d{8}/g) ?? []);
      if (!nums.size) return [];
      for (const t of this.trains()) for (const sg of t.sub_groups) for (const v of sg.vagons) {
        if (nums.has(v.vagon)) out.push({ train: t, sub: sg, vagon: v });
      }
    } else {
      const terms = this.invoiceQuery().toUpperCase().split(/[\s,;]+/).filter(Boolean);
      if (!terms.length) return [];
      for (const t of this.trains()) for (const sg of t.sub_groups) for (const v of sg.vagons) {
        const inv = v.invoice.toUpperCase();
        if (inv && terms.some((q) => inv.includes(q))) out.push({ train: t, sub: sg, vagon: v });
      }
    }
    return out;
  });

  // ── Обработчики ──────────────────────────────────────────────────────────
  setIndexSearch(v: string): void {
    // Цифры и дефисы: «786» либо полный «9379-786-9857» (можно без дефисов).
    this.indexSearch.set(v.replace(/[^\d-]/g, '').slice(0, 13));
  }

  runVagonSearch(): void {
    if (!this.vagonQuery().trim()) return;
    this.searchMode.set('vagon');
    const n = this.hits().length;
    n ? this.msg.success(`Найдено ${n} вагонов`) : this.msg.info('Вагоны с такими номерами не найдены');
  }

  runInvoiceSearch(): void {
    if (!this.invoiceQuery().trim()) return;
    this.searchMode.set('invoice');
    const n = this.hits().length;
    n ? this.msg.success(`Найдено ${n} вагонов`) : this.msg.info('Накладные не найдены');
  }

  clearListSearch(): void {
    this.searchMode.set(null);
    this.vagonQuery.set('');
    this.invoiceQuery.set('');
  }

  resetAll(): void {
    this.runMode.set('all');
    this.arrivalMode.set('all');
    this.selGruzpol.set(new Set(this.targets().map((t) => t.name)));
    this.selNaznach.set(new Set(this.targets().map((t) => t.name)));
    this.withPlan.set(false);
    this.broshen.set(false);
    this.indexSearch.set('');
    this.stationSearch.set('');
    this.operStationSearch.set('');
    this.cargoSearch.set('');
    this.clearListSearch();
  }

  toggle(sig: WritableSignal<Set<string>>, name: string): void {
    const next = new Set(sig());
    next.has(name) ? next.delete(name) : next.add(name);
    sig.set(next);
  }

  flip(key: string): void {
    const next = new Set(this.open());
    next.has(key) ? next.delete(key) : next.add(key);
    this.open.set(next);
  }

  openTrainMenu(ev: MouseEvent, t: Train, menu: NzDropdownMenuComponent): void {
    ev.preventDefault();
    this.ctxTrain.set(t);
    this.ctxVagon.set(null);
    this.ctxMenu.create(ev, menu);
  }

  openVagonMenu(ev: MouseEvent, t: Train, v: TrainVagon, menu: NzDropdownMenuComponent): void {
    ev.preventDefault();
    ev.stopPropagation();
    this.ctxTrain.set(t);
    this.ctxVagon.set(v);
    this.ctxMenu.create(ev, menu);
  }

  openTrail(): void {
    const v = this.ctxVagon();
    if (v) this.trailVagon.set(v);
  }

  // ── Форматирование (МСК naive — без сдвигов, срезами строки) ────────────
  /** дд.мм чч:мм */
  fmtDT(ts: string | null): string {
    if (!ts || ts.length < 16) return '—';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)} ${ts.slice(11, 16)}`;
  }

  /** дд.мм.гг */
  fmtD(ts: string | null): string {
    if (!ts || ts.length < 10) return '—';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)}.${ts.slice(2, 4)}`;
  }

  statusLabel(s: number | null): string {
    return s == null ? '—' : (STATUS_LABELS[s] ?? `статус ${s}`);
  }

  statusColor(s: number | null): string {
    return s == null ? '#8c8c8c' : (STATUS_COLORS[s] ?? '#8c8c8c');
  }

  targetColor(name: string): string {
    return this.targets().find((t) => t.name === name)?.color || 'transparent';
  }

  prostHours(t: Train): number {
    return (t.prost_dn ?? 0) * 24 + (t.prost_ch ?? 0);
  }

  prostText(t: Train): string {
    const parts: string[] = [];
    if (t.prost_dn) parts.push(`${t.prost_dn} дн`);
    if (t.prost_ch) parts.push(`${t.prost_ch} ч`);
    return parts.join(' ');
  }

  /** Срок доставки просрочен: дата (без времени) раньше сегодняшней МСК. */
  isOverdue(dateDostav: string | null): boolean {
    if (!dateDostav || dateDostav.length < 10) return false;
    return dateDostav.slice(0, 10) < todayMsk();
  }

  /** Средние 3 цифры индекса (номер поезда): «9379-786-9857» → «786». */
  private midDigits(index: string): string {
    const digits = index.replace(/\D/g, '');
    return digits.length >= 7 ? digits.slice(4, 7) : '';
  }

  /** Все цифры индекса: «9379-786-9857» → «93797869857» (для полного поиска). */
  private allDigits(index: string): string {
    return index.replace(/\D/g, '');
  }

  // ── Excel ────────────────────────────────────────────────────────────────
  /** Вся таблица (или результаты поиска) двумя листами «Поезда» + «Вагоны». */
  async exportAll(): Promise<void> {
    const XLSX = await import('xlsx-js-style');
    const wb = XLSX.utils.book_new();
    const mode = this.searchMode();
    if (!mode) {
      const rows = this.shownTrains().map((t) => ({
        'Индекс': t.index, 'Станция операции': t.station_oper, 'Дорога': t.doroga_oper,
        'Статус': this.statusLabel(t.status), 'Операция': t.oper_s,
        'Осталось, км': t.rasst ?? '', 'План': this.x(t.plan_jd), 'Прогноз': this.x(t.prog_jd),
        'Расчёт': this.x(t.rasch_jd), 'Простой': this.prostText(t),
        'Вагонов': t.vagon_count, 'Вес, т': +t.ves.toFixed(1),
        'Состав': t.sub_groups.map((sg) =>
          `${sg.index_main || '—'} | ${sg.station_nach} ${sg.gruzpol_s} → ${sg.naznach} (${sg.vagon_count})`).join('\n'),
      }));
      XLSX.utils.book_append_sheet(wb, XLSX.utils.json_to_sheet(rows), 'Поезда');
    }
    const vagonRows = (mode
      ? this.hits()
      : this.shownTrains().flatMap((t) => t.sub_groups.flatMap((sg) => sg.vagons.map((v) => ({ train: t, sub: sg, vagon: v })))))
      .map(({ train: t, sub: sg, vagon: v }) => ({
        '№': v.npp_vag ?? '', 'Вагон': v.vagon, 'Накладная': v.invoice,
        'Индекс': t.index, 'Род. индекс': sg.index_main,
        'Станция погрузки': sg.station_nach, 'Отправитель': sg.gruzotpr,
        'Марка груза': v.freight, 'Вес, т': v.ves ?? '',
        'Дата погрузки': this.fmtD(v.date_nach) === '—' ? '' : this.fmtD(v.date_nach),
        'Срок доставки': this.fmtD(v.date_dostav) === '—' ? '' : this.fmtD(v.date_dostav),
        'Получатель': sg.gruzpol_s, 'Назначение': sg.naznach,
        'Станция операции': t.station_oper, 'Операция': t.oper_s,
        'План': this.x(t.plan_jd), 'Прогноз': this.x(t.prog_jd), 'Простой': this.prostText(t),
        'Собственник': v.owner, 'Род вагона': v.rod_vag,
      }));
    XLSX.utils.book_append_sheet(wb, XLSX.utils.json_to_sheet(vagonRows), 'Вагоны');
    XLSX.writeFile(wb, mode ? 'поиск_по_дислокации.xlsx' : 'поезда_в_движении.xlsx');
  }

  /** Натурный лист одного поезда. */
  async exportTrain(t: Train | null): Promise<void> {
    if (!t) return;
    const XLSX = await import('xlsx-js-style');
    const wb = XLSX.utils.book_new();
    const rows = t.sub_groups.flatMap((sg) => sg.vagons.map((v) => ({
      '№': v.npp_vag ?? '', 'Вагон': v.vagon, 'Накладная': v.invoice,
      'Род. индекс': sg.index_main, 'Станция погрузки': sg.station_nach,
      'Отправитель': sg.gruzotpr, 'Марка груза': v.freight, 'Вес, т': v.ves ?? '',
      'Дата погрузки': this.fmtD(v.date_nach) === '—' ? '' : this.fmtD(v.date_nach),
      'Срок доставки': this.fmtD(v.date_dostav) === '—' ? '' : this.fmtD(v.date_dostav),
      'Получатель': sg.gruzpol_s, 'Назначение': sg.naznach,
      'Собственник': v.owner, 'Род вагона': v.rod_vag,
    }))).sort((a, b) => (Number(a['№']) || 1e9) - (Number(b['№']) || 1e9));
    XLSX.utils.book_append_sheet(wb, XLSX.utils.json_to_sheet(rows), 'Натурный лист');
    XLSX.writeFile(wb, `Натурный_лист_${t.index || 'поезд'}.xlsx`);
  }

  /** Дата-время для Excel (пустая строка вместо «—»). */
  private x(ts: string | null): string {
    const s = this.fmtDT(ts);
    return s === '—' ? '' : s;
  }
}
