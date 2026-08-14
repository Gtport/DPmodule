import {
  AfterViewInit, Component, ElementRef, OnDestroy, ViewChild,
  computed, effect, inject, signal,
} from '@angular/core';
import { DragDropModule } from '@angular/cdk/drag-drop';
import { FormsModule } from '@angular/forms';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzDrawerModule } from 'ng-zorro-antd/drawer';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzMessageService } from 'ng-zorro-antd/message';
import * as L from 'leaflet';
import 'leaflet.markercluster';
import { apiErrorMessage } from '../../core/api/api-error';
import { environment } from '../../../environments/environment';
import { TimeBaseService } from '../../shared/time-base.service';
import { MapGroup, MapSubgroup, MapTarget, MapWagon, MapsApiService } from './maps-api.service';

/** Режим показа: все / ходовые (есть прогноз) / отцепки (нет прогноза). */
type RunMode = 'all' | 'running' | 'detached';

/** Статусы DPmodule (docs/STATUSES.md) — краткие подписи для попапа. */
const STATUS_LABELS: Record<number, string> = {
  0: 'на отправлении (Б/И)', 1: 'на станции отправления', 2: 'в пути',
  4: 'долгий простой', 5: 'брошен', 6: 'порожний в пути',
  9: 'кандидат в прибывшие', 10: 'прибыл', 12: 'выгружен',
};
/** Цвет маркера по статусу — запасной, когда marka.color по группе не единогласен. */
const STATUS_COLORS: Record<number, string> = {
  0: '#1890ff', 1: '#1890ff', 2: '#1890ff',
  4: '#faad14', 5: '#ff4d4f', 6: '#8c8c8c',
  9: '#73d13d', 10: '#52c41a', 12: '#52c41a',
};

/** Палитра пометок диспетчера (info_2) — 8 узнаваемых цветов ant design. */
const MARK_PALETTE = ['#f5222d', '#fa8c16', '#fadb14', '#52c41a', '#13c2c2', '#1677ff', '#722ed1', '#eb2f96'];

/** Прозрачный PNG: промах кэша тайлов (404) рисуется пустым квадратом. */
const BLANK_TILE = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=';

/** Стартовые центр и зум — дословно из gtport Maps.tsx; «Сброс» возвращает к ним. */
const MAP_CENTER: L.LatLngExpression = [51.7558, 110.6173];
const MAP_ZOOM = 5;

/**
 * Экран «Карта» — дислокация на карте (перенос концепции gtport Maps.tsx).
 * Маркер = группа «поезд на станции» (ключ trainKey — как у «Поездов в
 * движении»), кластеризация Leaflet, подложка — свои тайлы из кэша БД
 * (зумы 4–8, промах = прозрачный квадрат — карта живёт и без подложки).
 *
 * Отходы от gtport (решения владельца 06.08.2026):
 * - порога «min 20 вагонов» нет — отцепки видны, плотность решают кластеры;
 * - режим все/ходовые/отцепки — канон prog_jd (как «Поезда в движении»);
 * - терминалы чипов — из реестра ports (унификация), не хардкод;
 * - вагоны группы грузятся по требованию (drawer), не в основном ответе;
 * - пометка диспетчера: цвет + текст → info_1/info_2 снимка, переживает
 *   пересборку (carry-over), значок «!» цветом пометки на маркере + текст
 *   в попапе; фильтр «!» показывает только помеченные группы.
 *
 * Решения владельца 07.08.2026:
 * - панель фильтров лежит ПОВЕРХ карты (как в gtport) — всегда в видимой
 *   области; кнопка «Сброс» возвращает фильтры, поиск и вид карты
 *   (зум/центр) к дефолту, НО сохраняет выбор терминалов;
 * - время прибытия в попапе: план ИЛИ прогноз ИЛИ «ход+расчёт» (см.
 *   popupHtml), шкала ЖД/МСК из профиля, комментарий риска по mistake;
 * - дефолт: режим «Ходовые» + выбран только первый терминал;
 * - слой «Дороги» — полигоны ж/д дорог (roads.geojson из public, файлы дал
 *   смежный проект; подписи/цвета — из их map_config, описания ВНУТРИ
 *   geojson перепутаны и не используются), отключается чипом;
 * - «Развернуть» — все наложенные маркеры раскладываются веером вокруг
 *   станции (аналог spiderfy, но для всех кластеров сразу; кластеризация
 *   в этом режиме выключена, позиции пересчитываются при смене зума);
 * - у всех кнопок панели единое поведение (hover — голубые рамка и текст,
 *   активная — жирный), без цветных заливок; «Приб» перечёркнут по
 *   диагонали, пока прибывшие скрыты; кнопок масштаба нет (зум колесом);
 * - вагоны группы: + станция отправления, отправитель, дата отправления,
 *   срок доставки, выгрузка в Excel на клиенте;
 * - экран целиком выключается конфигом бэка (map.enabled: false) — роутов
 *   и пункта меню нет (флаг map_enabled в GET /settings/ui).
 */
@Component({
  selector: 'app-maps',
  imports: [
    DragDropModule, FormsModule, NzButtonModule, NzDrawerModule, NzIconModule,
    NzInputModule, NzModalModule, NzTagModule, NzTooltipModule,
  ],
  template: `
    <div class="page">
      <!-- ── Карта; панель фильтров лежит поверх неё (как в gtport) ────── -->
      <div class="map-wrap">
        <div #mapEl class="map"></div>
        <div #ovlEl class="ovl">
          <div class="bar">
            <span class="fblock">
              @for (t of targets(); track t.name) {
                <nz-tag [class.on]="selNaznach().has(t.name)"
                        [style.background-color]="selNaznach().has(t.name) ? (t.color || undefined) : undefined"
                        (click)="toggleNaznach(t.name)">{{ t.name }}</nz-tag>
              }
            </span>
            <span class="fblock">
              <nz-tag [class.on]="runMode() === 'running'" (click)="runMode.set('running')">Ходовые</nz-tag>
              <nz-tag [class.on]="runMode() === 'detached'" (click)="runMode.set('detached')">Отцепки</nz-tag>
              <nz-tag [class.on]="runMode() === 'all'" (click)="runMode.set('all')">Все</nz-tag>
            </span>
            <nz-tag [class.on]="planOnly()" (click)="planOnly.set(!planOnly())"
                    nz-tooltip="Только поезда с планом подвода">План</nz-tag>
            <nz-tag [class.on]="broshenOnly()" (click)="broshenOnly.set(!broshenOnly())">Брош</nz-tag>
            <nz-tag [class.on]="markedOnly()" (click)="markedOnly.set(!markedOnly())"
                    nz-tooltip="Только группы с пометкой диспетчера">!</nz-tag>
            <nz-tag [class.strike]="hideArrived()" [class.on]="!hideArrived()"
                    (click)="hideArrived.set(!hideArrived())"
                    nz-tooltip="Прибывшие: по умолчанию скрыты (стоят на станции назначения и загромождают карту), клик — показать">
              Приб
            </nz-tag>
            <input nz-input nzSize="small" class="w-index" placeholder="индекс"
                   [ngModel]="indexSearch()" (ngModelChange)="onSearch($event)" />
            <button nz-button nzSize="small" (click)="stationsOpen.set(!stationsOpen())"
                    [class.on]="selStations().size">
              Станции @if (selStations().size) { ({{ selStations().size }}) }
            </button>
            <button nz-button nzSize="small" [class.on]="expandAll()"
                    (click)="expandAll.set(!expandAll())"
                    nz-tooltip="Разложить наложенные друг на друга поезда вокруг станций">
              {{ expandAll() ? 'Свернуть' : 'Развернуть' }}
            </button>
            <nz-tag [class.on]="roadsOn()" (click)="roadsOn.set(!roadsOn())"
                    nz-tooltip="Полигоны железных дорог">Дороги</nz-tag>
            <button nz-button nzSize="small" (click)="resetFilters()"
                    nz-tooltip="Вернуть фильтры и поиск к исходным">Сброс</button>
            <span class="mut cnt">
              <b>{{ shown().length }}</b>/{{ groups().length + noCoords().length }} гр. · {{ vagonsTotal() }} ваг.
            </span>
            @if (selected().size) {
              <button nz-button nzSize="small" nzType="primary" (click)="openMark([...selected()])">
                <span nz-icon nzType="bg-colors"></span> Пометка ({{ selected().size }})
              </button>
              <button nz-button nzSize="small" (click)="clearSelection()">снять выбор</button>
            }
            @if (noCoords().length) {
              <button nz-button nzSize="small" (click)="noCoordsOpen.set(true)"
                      nz-tooltip="Станция не имеет координат в справочнике — группы не видны на карте">
                <span nz-icon nzType="environment"></span> Не на карте: {{ noCoords().length }}
              </button>
            }
            <button nz-button nzSize="small" (click)="load()" [nzLoading]="loading()" nz-tooltip="Обновить (авто — раз в 5 минут)">
              <span nz-icon nzType="sync"></span>
            </button>
            <span class="qm" nz-tooltip="клик по маркеру — карточка группы · Ctrl+клик — выбор нескольких для пометки">?</span>
          </div>

          <!-- ── Панель станций погрузки (как в gtport: поиск + чекбоксы) ── -->
          @if (stationsOpen()) {
            <div class="st-panel">
              <input nz-input nzSize="small" class="w-search" placeholder="найти станцию…"
                     [ngModel]="stationQuery()" (ngModelChange)="stationQuery.set($event)" />
              <button nz-button nzSize="small" nzType="text" (click)="clearStations()"
                      [disabled]="!selStations().size">сбросить</button>
              <div class="st-list">
                @for (s of stationOptions(); track s) {
                  <nz-tag [class.on]="selStations().has(s)" (click)="toggleStation(s)">{{ s }}</nz-tag>
                } @empty {
                  <span class="mut">ничего не нашлось</span>
                }
              </div>
            </div>
          }
        </div>
      </div>

      <!-- ── «Не на карте»: группы без координат станции ───────────────── -->
      <nz-drawer [nzVisible]="noCoordsOpen()" nzPlacement="right" [nzWidth]="420"
                 nzTitle="Не на карте: станция без координат" (nzOnClose)="noCoordsOpen.set(false)">
        <ng-container *nzDrawerContent>
          <div class="mut" style="margin-bottom:8px">
            Координаты станции заполняются в Админе (справочник stations) — после
            «Обновить справочники» группа появится на карте.
          </div>
          @for (g of noCoordsShown(); track g.key) {
            <div class="nc-row">
              <b>{{ g.index }}</b>
              <span>{{ g.station_oper }}</span>
              <span class="mut">{{ g.vagon_count }} ваг.</span>
              @if (g.prog_jd) { <span class="run">ходовой</span> } @else { <span class="det">отцепка</span> }
              <button nz-button nzSize="small" nzType="text" (click)="openWagons(g.key)">вагоны</button>
            </div>
          } @empty {
            <div class="mut">под текущие фильтры ничего не попало</div>
          }
        </ng-container>
      </nz-drawer>

      <!-- ── Вагоны группы (drill-down по требованию) ──────────────────── -->
      <nz-drawer [nzVisible]="wagonsKey() !== null" nzPlacement="right" [nzWidth]="1050"
                 [nzTitle]="'Вагоны группы ' + (wagonsKey() ?? '')" (nzOnClose)="wagonsKey.set(null)">
        <ng-container *nzDrawerContent>
          @if (wagonsLoading()) {
            <div class="mut">загрузка…</div>
          } @else {
            <div style="margin-bottom:8px">
              <button nz-button nzSize="small" [disabled]="!wagons().length" (click)="exportWagons()">
                <span nz-icon nzType="download"></span> Excel
              </button>
            </div>
            <div class="dp-tbl-wrap">
              <table class="dp-tbl">
                <thead>
                  <tr>
                    <th>№</th><th>Вагон</th><th>Накладная</th><th>Марка груза</th>
                    <th>Вес, т</th><th>Собственник</th><th>Статус</th><th>Получ.</th><th>Назн.</th>
                    <th>Ст. отправления</th><th>Отправитель</th>
                    <th nz-tooltip nzTooltipTitle="Начало рейса по ЛК — та же дата, что «Дата погрузки» на прочих экранах">Погружен</th>
                    <th nz-tooltip nzTooltipTitle="Фактическое отправление со станции по АСОУП — может быть на часы и сутки позже погрузки">Отправление</th>
                    <th>Срок дост.</th>
                  </tr>
                </thead>
                <tbody>
                  @for (v of wagons(); track v.id) {
                    <tr>
                      <td>{{ v.npp_vag ?? '' }}</td>
                      <td><b>{{ v.vagon }}</b></td>
                      <td>{{ v.invoice }}</td>
                      <td>{{ v.freight }}</td>
                      <td>{{ v.ves ?? '' }}</td>
                      <td>{{ v.owner }}</td>
                      <td>{{ statusLabel(v.status) }}</td>
                      <td>{{ v.gruzpol_s }}</td>
                      <td>{{ v.naznach }}</td>
                      <td>{{ v.station_nach }}</td>
                      <td>{{ v.gruzotpr }}</td>
                      <td>{{ fmtDT(v.date_nach) }}</td>
                      <td>{{ fmtDT(v.date_otpr) }}</td>
                      <td>{{ fmtDT(v.date_dostav) }}</td>
                    </tr>
                  }
                </tbody>
              </table>
            </div>
          }
        </ng-container>
      </nz-drawer>

      <!-- ── Пометка групп (перемещаемая модалка — канон проекта) ─────────── -->
      <nz-modal [nzVisible]="markKeys() !== null" [nzTitle]="markTitleTpl" [nzFooter]="null"
                [nzWidth]="420" (nzOnCancel)="markKeys.set(null)">
        <ng-template #markTitleTpl>
          <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
            Пометка: {{ markKeys()?.length }} групп(а)
          </div>
        </ng-template>
        <ng-container *nzModalContent>
          <div class="pal">
            @for (c of palette; track c) {
              <span class="sw" [style.background]="c" [class.sel]="markColor() === c"
                    (click)="markColor.set(markColor() === c ? '' : c)"></span>
            }
          </div>
          <input nz-input placeholder="текст пометки (до 80 символов)" maxlength="80"
                 [ngModel]="markText()" (ngModelChange)="markText.set($event)" />
          <div class="mrk-btns">
            <button nz-button nzType="primary" [nzLoading]="marking()"
                    [disabled]="!markText() && !markColor()" (click)="applyMark(false)">
              Применить
            </button>
            <button nz-button nzDanger [nzLoading]="marking()" (click)="applyMark(true)">
              Снять пометку
            </button>
          </div>
          <div class="mut" style="margin-top:8px">
            Пометка пишется в свободное поле дислокации всем вагонам группы,
            переживает обновления из АСУ/ЛК и снимается кнопкой «Снять пометку».
          </div>
        </ng-container>
      </nz-modal>
    </div>
  `,
  styles: [`
    .page { display: flex; flex-direction: column; height: calc(100vh - 96px); }
    .map-wrap { position: relative; flex: 1; min-height: 320px; }
    .map { position: absolute; inset: 0; border: 1px solid var(--border); border-radius: 6px; background: #f6f6f4; }
    /* Панель поверх карты: контейнер не ловит клики (карта под ним живёт),
       ловят только сами панели. z-index 1000 — уровень контролов Leaflet.
       Ширина — по содержимому (align-items), а не на всю карту. */
    .ovl { position: absolute; top: 8px; left: 8px; z-index: 1000;
           max-width: calc(100% - 16px); align-items: flex-start;
           display: flex; flex-direction: column; gap: 6px; pointer-events: none; }
    .ovl > * { pointer-events: auto; max-width: 100%; }
    .bar { display: flex; align-items: center; gap: 6px; flex-wrap: wrap;
           background: rgba(255,255,255,.95); border: 1px solid var(--border);
           border-radius: 8px; padding: 6px 8px; box-shadow: 0 2px 8px rgba(0,0,0,.18); }
    .mut { color: var(--text-tertiary); }
    .cnt { white-space: nowrap; }
    .qm { width: 18px; height: 18px; border-radius: 50%; border: 1px solid var(--border);
          display: inline-flex; align-items: center; justify-content: center;
          color: var(--text-tertiary); cursor: help; font-size: 12px; flex: none; }
    .fblock { display: inline-flex; gap: 0; }
    /* Единое поведение всех кнопок и фильтров панели (уточнение владельца
       10.08.2026): hover — тонкая голубая рамка и голубой текст; активная —
       та же тонкая рамка, но текст ЖИРНЫЙ (второго слоя рамки нет). Правила
       охватывают и чипы станций в .st-list — панель станций живёт вне .bar.
       Терминалы при этом несут фирменную заливку (ports.color) через
       [style.background-color]. Шрифт чипов = шрифту кнопок (14px), иначе
       «Дороги» рядом с кнопками выглядел мельче. */
    nz-tag { cursor: pointer; user-select: none; font-size: 14px; line-height: 22px; }
    .bar nz-tag:hover, .bar button:hover, .st-list nz-tag:hover {
      border-color: var(--color-primary); color: var(--color-primary);
    }
    .bar nz-tag.on, .bar button.on, .st-list nz-tag.on {
      border-color: var(--color-primary);
      color: var(--color-primary); font-weight: 600;
    }
    /* «Приб» скрыт — тонкое диагональное перечёркивание; клик снимает. */
    nz-tag.strike { position: relative; }
    nz-tag.strike::after {
      content: ''; position: absolute; inset: 0; pointer-events: none;
      background: linear-gradient(to bottom right,
        transparent 48.5%, currentColor 49.5%, currentColor 50.5%, transparent 51.5%);
    }
    .nc-row { display: flex; align-items: center; gap: 8px; padding: 4px 0; border-bottom: 1px solid var(--border); }
    .run { color: #52c41a; } .det { color: #fa8c16; }
    .pal { display: flex; gap: 8px; margin-bottom: 10px; }
    .sw { width: 26px; height: 26px; border-radius: 6px; cursor: pointer; border: 2px solid transparent; }
    .sw.sel { border-color: var(--text-primary); box-shadow: 0 0 0 2px #fff inset; }
    .mrk-btns { display: flex; gap: 8px; margin-top: 12px; }
    .ttl { cursor: move; }

    /* Маркеры и кластеры Leaflet живут вне Angular-шаблона — стили глобальные
       для этого компонента через ::ng-deep недоступны, поэтому классы ниже
       прописаны в html маркеров инлайном, а кластерам — здесь: */
    :host ::ng-deep .dp-cluster div {
      width: 40px; height: 40px; border-radius: 50%; background: #1677ff;
      color: #fff; display: flex; align-items: center; justify-content: center;
      font-weight: 600; border: 2px solid #fff; box-shadow: 0 1px 4px rgba(0,0,0,.4);
    }
    :host ::ng-deep .dp-cluster div.red { background: #ff4d4f; }
    :host ::ng-deep .dp-pop { font-size: 12px; min-width: 220px; }
    :host ::ng-deep .dp-pop .btns { display: flex; gap: 6px; margin-top: 8px; }
    :host ::ng-deep .dp-pop button {
      cursor: pointer; border: 1px solid #d9d9d9; background: #fff;
      border-radius: 4px; padding: 2px 8px; font-size: 12px;
    }
    :host ::ng-deep .dp-pop button:hover { border-color: #1677ff; color: #1677ff; }
    :host ::ng-deep .dp-pop .cargo-list { max-height: 180px; overflow-y: auto; margin-top: 4px; }
    :host ::ng-deep .dp-pop .cargo { background: #fafafa; border-radius: 4px; padding: 3px 6px; margin-bottom: 4px; }
    :host ::ng-deep .dp-pop .mut2 { color: #999; }
    .w-search { width: 170px; }
    .w-index { width: 84px; }
    .st-panel { display: flex; align-items: flex-start; gap: 8px; padding: 6px 8px;
                border: 1px solid var(--border); border-radius: 8px;
                background: rgba(255,255,255,.97); box-shadow: 0 2px 8px rgba(0,0,0,.18); }
    .st-list { display: flex; flex-wrap: wrap; gap: 2px; max-height: 96px; overflow-y: auto; flex: 1; }
  `],
})
export class MapsComponent implements AfterViewInit, OnDestroy {
  private readonly api = inject(MapsApiService);
  private readonly msg = inject(NzMessageService);
  private readonly timeBase = inject(TimeBaseService);

  @ViewChild('mapEl', { static: true }) mapEl!: ElementRef<HTMLDivElement>;
  @ViewChild('ovlEl', { static: true }) ovlEl!: ElementRef<HTMLDivElement>;

  readonly palette = MARK_PALETTE;

  readonly loading = signal(false);
  readonly groups = signal<MapGroup[]>([]);
  readonly noCoords = signal<MapGroup[]>([]);
  readonly targets = signal<MapTarget[]>([]);
  readonly vagonsTotal = signal(0);

  // Фильтры — клиентские (как «Поезда в движении»). Дефолты (решение
  // владельца 07.08.2026): «Ходовые» + только первый терминал.
  readonly runMode = signal<RunMode>('running');
  readonly selNaznach = signal<Set<string>>(new Set());
  readonly hideArrived = signal(true);
  readonly broshenOnly = signal(false);
  readonly markedOnly = signal(false);
  readonly planOnly = signal(false); // только поезда с планом подвода
  /** Слой полигонов ж/д дорог (roads.geojson) — вкл/выкл чипом «Дороги». */
  readonly roadsOn = signal(true);
  /** «Развернуть»: наложенные маркеры раскладываются веером, без кластеров. */
  readonly expandAll = signal(false);

  // Поиск по коду поезда (правило gtport: середина индекса, режим поиска
  // переопределяет прочие фильтры) и фильтр по станциям погрузки.
  readonly indexSearch = signal('');
  readonly stationsOpen = signal(false);
  readonly stationQuery = signal('');
  readonly selStations = signal<Set<string>>(new Set());

  // Выбор групп Ctrl+кликом — для пометки нескольких за раз.
  readonly selected = signal<Set<string>>(new Set());

  // Боковые панели и модалка пометки.
  readonly noCoordsOpen = signal(false);
  readonly wagonsKey = signal<string | null>(null);
  readonly wagons = signal<MapWagon[]>([]);
  readonly wagonsLoading = signal(false);
  readonly markKeys = signal<string[] | null>(null);
  readonly markText = signal('');
  readonly markColor = signal('');
  readonly marking = signal(false);

  private map?: L.Map;
  private cluster?: L.MarkerClusterGroup;
  private spread?: L.LayerGroup;   // маркеры режима «Развернуть»
  private roadsLayer?: L.GeoJSON;  // полигоны дорог
  private naznachInited = false;   // дефолт терминалов ставится один раз
  private timer?: ReturnType<typeof setInterval>;

  /** Группы под текущие фильтры — на карту. */
  readonly shown = computed(() => this.applyFilters(this.groups()));
  /** Те же фильтры для бокового списка «не на карте». */
  readonly noCoordsShown = computed(() => this.applyFilters(this.noCoords()));

  /**
   * Эталон «большого поезда» для размера меток — 90-й перцентиль составов
   * ВСЕГО снимка, без фильтров (решение владельца 14.08.2026: прежняя
   * gtport-формула «полный размер к 100 вагонам» делала у клиента с
   * короткими партиями — АНБ, до 20 ваг — все метки одинаково мелкими).
   * У угольных клиентов перцентиль ~71 — шкала почти как раньше; пол 10
   * вагонов — чтобы единственная трёхвагонная группа не рисовалась гигантом.
   */
  private readonly sizeRef = computed(() => {
    const counts = [...this.groups(), ...this.noCoords()]
      .map((g) => g.vagon_count)
      .sort((a, b) => a - b);
    if (!counts.length) return 10;
    return Math.max(10, counts[Math.floor((counts.length - 1) * 0.9)]);
  });

  constructor() {
    // Данные/фильтры изменились → перерисовать маркеры (карта уже может жить).
    effect(() => {
      const list = this.shown();
      this.selected();  // подсветка выбора — тоже повод перерисовать
      this.expandAll(); // смена режима «Развернуть» — тоже
      this.render(list);
    });
    // Чип «Дороги»: слой добавляется/убирается без перерисовки маркеров.
    effect(() => {
      const on = this.roadsOn();
      if (!this.map || !this.roadsLayer) return;
      if (on) this.roadsLayer.addTo(this.map); else this.roadsLayer.remove();
    });
  }

  ngAfterViewInit(): void {
    this.map = L.map(this.mapEl.nativeElement, {
      center: MAP_CENTER,
      zoom: MAP_ZOOM,
      minZoom: 4,
      maxZoom: 8, // сплошное покрытие кэша тайлов — зумы 4–8 (docs/MAP_TILES.md)
      maxBounds: L.latLngBounds([[35, 15], [78, 180]]),
      maxBoundsViscosity: 1.0,
      attributionControl: false,
      zoomControl: false, // без кнопок масштаба (решение владельца 07.08.2026):
      // зум колесом/жестами, а панель фильтров всё равно накрыла бы контрол
    });
    L.tileLayer(`${environment.apiBaseUrl}/v1/map/tiles/{z}/{x}/{y}`, {
      maxZoom: 8,
      errorTileUrl: BLANK_TILE, // 404 промаха кэша → прозрачный квадрат
    }).addTo(this.map);
    L.control.attribution({ prefix: false }).addAttribution('© OpenStreetMap').addTo(this.map);

    // Собственный слой gtport поверх тайлов: сеть ж/д линий + фоновые полигоны
    // (map.geojson из public, ~3 МБ, грузится отдельно от бандла). Стили — как
    // в оригинале: магистрали жирнее, прочие линии тоньше.
    void fetch('map.geojson')
      .then((r) => (r.ok ? r.json() : null))
      .then((data) => {
        if (!data || !this.map) return;
        L.geoJSON(data, { style: geoJsonStyle, interactive: false }).addTo(this.map);
      })
      .catch(() => {}); // слой декоративный: нет файла — карта живёт без него

    // Полигоны ж/д дорог — отдельная панель НАД фоновым map.geojson (иначе
    // его непрозрачные полигоны суши закрасили бы дороги: порядок отрисовки
    // SVG = порядок добавления, а грузятся файлы вразнобой) и ПОД маркерами.
    this.map.createPane('roads').style.zIndex = '450';
    void fetch('roads.geojson')
      .then((r) => (r.ok ? r.json() : null))
      .then((data) => {
        if (!data || !this.map) return;
        this.roadsLayer = L.geoJSON(data, { style: geoJsonStyle, interactive: false, pane: 'roads' });
        if (this.roadsOn()) this.roadsLayer.addTo(this.map);
      })
      .catch(() => {}); // тоже декоративный

    // markerClusterGroup берём через window.L, а не через `import * as L`:
    // esbuild фиксирует список полей namespace-импорта ДО выполнения плагина
    // leaflet.markercluster, и дописанный плагином метод в `L` не попадает
    // (в проде это «markerClusterGroup is not a function» и пустая карта).
    // UMD-сборка Leaflet кладёт свой живой объект в window.L — его плагин и патчит.
    const leafletLive = (window as unknown as { L: typeof L }).L ?? L;
    this.cluster = leafletLive.markerClusterGroup({
      // Радиус 1 — как в gtport: поезда НЕ схлопываются, пока маркеры реально
      // не легли друг на друга (одна станция); там кластер раскрывается spiderfy.
      maxClusterRadius: 1,
      spiderfyOnMaxZoom: true,
      iconCreateFunction: (cl) => {
        const broshen = cl.getAllChildMarkers().some((m) => (m as MarkerWithMeta).dpBroshen);
        return L.divIcon({
          className: 'dp-cluster',
          html: `<div${broshen ? ' class="red"' : ''}>${cl.getChildCount()}</div>`,
          iconSize: [40, 40],
        });
      },
    });
    this.map.addLayer(this.cluster);
    // Группа маркеров режима «Развернуть» (кластеры в нём не участвуют).
    this.spread = L.layerGroup().addTo(this.map);
    // Раскладка веера считается в пикселях — при смене зума пересчитать.
    this.map.on('zoomend', () => {
      if (this.expandAll()) this.render(this.shown());
    });

    // Кнопки внутри попапа рендерятся вне Angular — вешаем обработчики при
    // открытии попапа (делегирование по data-атрибутам).
    this.map.on('popupopen', (e: L.PopupEvent) => {
      const el = e.popup.getElement();
      el?.querySelectorAll<HTMLButtonElement>('button[data-act]').forEach((btn) => {
        btn.onclick = () => {
          const key = btn.dataset['key'] ?? '';
          if (btn.dataset['act'] === 'wagons') this.openWagons(key);
          else this.openMark([key]);
          this.map?.closePopup();
        };
      });
    });

    void this.load();
    // Автообновление раз в 5 минут: зум/центр карты не трогаем, открытые панели живут.
    this.timer = setInterval(() => void this.load(), 300_000);
  }

  ngOnDestroy(): void {
    if (this.timer) clearInterval(this.timer);
    this.map?.remove();
  }

  async load(): Promise<void> {
    this.loading.set(true);
    try {
      const data = await this.api.getMapData();
      this.groups.set(data.groups ?? []);
      this.noCoords.set(data.no_coords ?? []);
      this.targets.set(data.targets ?? []);
      this.vagonsTotal.set(data.vagons ?? 0);
      // По умолчанию выбран ТОЛЬКО ПЕРВЫЙ терминал (решение владельца
      // 07.08.2026, отход от канона trains «выбраны все»). Ставится один раз:
      // дальше выбор пользователя не трогаем, даже если он снял все чипы.
      if (!this.naznachInited && this.targets().length) {
        this.selNaznach.set(new Set([this.targets()[0].name]));
        this.naznachInited = true;
      }
      // Выбор чистим от исчезнувших групп (поезд уехал/переформирован).
      const alive = new Set([...this.groups(), ...this.noCoords()].map((g) => g.key));
      this.selected.update((s) => new Set([...s].filter((k) => alive.has(k))));
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.loading.set(false);
    }
  }

  // ── Фильтры ───────────────────────────────────────────────────────────────
  private applyFilters(list: MapGroup[]): MapGroup[] {
    // Режим поиска переопределяет все прочие фильтры (правило gtport).
    const q = this.indexSearch().trim().toUpperCase();
    if (q) return list.filter((g) => trainLabel(g.index).toUpperCase().startsWith(q));

    if (this.runMode() === 'running') list = list.filter((g) => !!g.prog_jd);
    else if (this.runMode() === 'detached') list = list.filter((g) => !g.prog_jd);
    if (this.hideArrived()) list = list.filter((g) => !g.arrived);
    if (this.planOnly()) list = list.filter((g) => !!g.plan_jd);
    if (this.broshenOnly()) list = list.filter((g) => g.broshen);
    if (this.markedOnly()) list = list.filter((g) => !!g.mark_color || !!g.mark_text);
    const all = this.targets().length;
    const nz = this.selNaznach();
    if (all && nz.size < all) {
      list = list.filter((g) => g.naznach_list.some((n) => nz.has(n)) || !g.naznach_list.length);
    }
    const st = this.selStations();
    if (st.size) {
      list = list.filter((g) => g.sub_groups.some((sg) => st.has(sg.station_nach)));
    }
    return list;
  }

  /** Словарь станций погрузки — из состава всех групп (как в gtport). */
  readonly stationOptions = computed(() => {
    const q = this.stationQuery().trim().toUpperCase();
    const set = new Set<string>();
    for (const g of [...this.groups(), ...this.noCoords()]) {
      for (const sg of g.sub_groups) {
        if (sg.station_nach && (!q || sg.station_nach.toUpperCase().includes(q))) {
          set.add(sg.station_nach);
        }
      }
    }
    return [...set].sort();
  });

  clearStations(): void {
    this.selStations.set(new Set());
  }

  /** «Сброс» (уточнение владельца 07.08.2026) — фильтры и поиск к исходным
   *  + зум/центр карты к стартовым, НО выбор терминалов сохраняется.
   *  Слой дорог и режим «Развернуть» — не фильтры, их сброс не трогает. */
  resetFilters(): void {
    this.runMode.set('running');
    this.hideArrived.set(true);
    this.planOnly.set(false);
    this.broshenOnly.set(false);
    this.markedOnly.set(false);
    this.indexSearch.set('');
    this.stationQuery.set('');
    this.selStations.set(new Set());
    this.map?.setView(MAP_CENTER, MAP_ZOOM);
  }

  toggleStation(name: string): void {
    this.selStations.update((s) => {
      const next = new Set(s);
      if (next.has(name)) next.delete(name); else next.add(name);
      return next;
    });
  }

  /** Поиск: одно совпадение — подлёт к нему (зум 7, как gtport), несколько —
   *  подлёт к средней точке; пустой ввод возвращает обычные фильтры. */
  onSearch(value: string): void {
    this.indexSearch.set(value);
    const found = this.shown().filter((g) => g.lat != null);
    if (!value.trim() || !found.length || !this.map) return;
    if (found.length === 1) {
      this.map.flyTo([found[0].lat!, found[0].lon!], 7);
    } else {
      const lat = found.reduce((s, g) => s + g.lat!, 0) / found.length;
      const lon = found.reduce((s, g) => s + g.lon!, 0) / found.length;
      this.map.flyTo([lat, lon], this.map.getZoom());
    }
  }

  toggleNaznach(name: string): void {
    this.selNaznach.update((s) => {
      const next = new Set(s);
      if (next.has(name)) next.delete(name); else next.add(name);
      return next;
    });
  }

  clearSelection(): void {
    this.selected.set(new Set());
  }

  // ── Маркеры ───────────────────────────────────────────────────────────────
  private render(list: MapGroup[]): void {
    if (!this.cluster || !this.spread) return;
    const sel = this.selected();
    this.cluster.clearLayers();
    this.spread.clearLayers();

    if (!this.expandAll()) {
      this.cluster.addLayers(list.map((g) => this.makeMarker(g, sel)));
      return;
    }

    // «Развернуть»: как spiderfy, но для ВСЕХ станций сразу — стоящие на
    // одной точке маркеры раскладываются веером с «ножками» к станции.
    // Радиус в пикселях, поэтому на смену зума render дёргается заново.
    const byPos = new Map<string, MapGroup[]>();
    for (const g of list) {
      const k = `${g.lat},${g.lon}`;
      const arr = byPos.get(k);
      if (arr) arr.push(g); else byPos.set(k, [g]);
    }
    for (const gs of byPos.values()) {
      if (gs.length === 1) {
        this.spread.addLayer(this.makeMarker(gs[0], sel));
        continue;
      }
      const center = L.latLng(gs[0].lat!, gs[0].lon!);
      const cp = this.map!.latLngToLayerPoint(center);
      const r = 26 + gs.length * 2;
      gs.forEach((g, i) => {
        const a = (2 * Math.PI * i) / gs.length - Math.PI / 2;
        const ll = this.map!.layerPointToLatLng(
          L.point(cp.x + r * Math.cos(a), cp.y + r * Math.sin(a)));
        this.spread!.addLayer(L.polyline([center, ll],
          { color: '#8c8c8c', weight: 1, opacity: 0.7, interactive: false }));
        this.spread!.addLayer(this.makeMarker(g, sel, ll));
      });
    }
  }

  private makeMarker(g: MapGroup, sel: Set<string>, pos?: L.LatLng): L.Marker {
    // Цвет — правило gtport: единогласный marka.color группы, иначе по статусу.
    const color = g.color || (g.status != null ? STATUS_COLORS[g.status] : '') || '#8c8c8c';
    const m = L.marker(pos ?? [g.lat!, g.lon!], {
      icon: this.markerIcon(g, color, sel.has(g.key)),
    }) as MarkerWithMeta;
    m.dpBroshen = g.broshen;
    m.on('click', (e: L.LeafletMouseEvent) => {
      if (e.originalEvent.ctrlKey || e.originalEvent.metaKey) {
        this.selected.update((s) => {
          const next = new Set(s);
          if (next.has(g.key)) next.delete(g.key); else next.add(g.key);
          return next;
        });
      } else {
        // Попап открываем сами, БЕЗ bindPopup: тот вешает на маркер свой
        // click-переключатель, и со второго клика оба обработчика гасили
        // друг друга — маркер выглядел некликабельным (жалоба 07.08.2026).
        // Панель фильтров лежит над попапами (z-index против stacking-
        // контекста карты бессилен), поэтому автосдвиг Leaflet просят
        // отвезти попап НИЖЕ панели: отступ сверху = высота панели.
        const pad = this.ovlEl.nativeElement.offsetHeight + 16;
        L.popup({
          maxWidth: 320, offset: L.point(0, -8),
          autoPanPaddingTopLeft: L.point(12, pad),
          autoPanPaddingBottomRight: L.point(12, 12),
        })
          .setLatLng(m.getLatLng())
          .setContent(this.popupHtml(g))
          .openOn(this.map!);
      }
    });
    return m;
  }

  private markerIcon(g: MapGroup, color: string, isSelected: boolean): L.DivIcon {
    // Размер круга 20…40px растёт с числом вагонов ОТНОСИТЕЛЬНО эталона
    // снимка (sizeRef): полный размер — у поездов от 90-го перцентиля.
    // В gtport знаменатель был жёстким (count/5, полный размер к 100 ваг).
    const size = Math.round(Math.max(20, Math.min(40, 20 + 20 * (g.vagon_count / this.sizeRef()))));
    const border = g.broshen ? '#ff4d4f' : '#ffffff';
    const rings: string[] = [];
    if (isSelected) rings.push('0 0 0 6px rgba(22,119,255,.45)'); // выбор Ctrl+кликом
    rings.push('0 1px 4px rgba(0,0,0,.4)');
    // Пометка диспетчера — значок «!» цветом пометки на белом кружке
    // (решение владельца 07.08.2026: кольцо было плохо различимо).
    const badge = g.mark_color
      ? `<span style="position:absolute;top:-7px;right:-7px;width:15px;height:15px;
          border-radius:50%;background:#fff;border:2px solid ${g.mark_color};
          color:${g.mark_color};font:900 11px/11px sans-serif;display:flex;
          align-items:center;justify-content:center;">!</span>`
      : '';
    // Красная точка — у поездов с предупреждением об опоздании/опережении
    // нитки (решение владельца 07.08.2026); срыв по бросанию точкой не
    // дублируем — у брошенных и так красная обводка.
    const risk = riskComment(g, '');
    const dot = risk && risk.kind !== 'bros'
      ? `<span style="position:absolute;top:-4px;left:-4px;width:11px;height:11px;
          border-radius:50%;background:#f5222d;border:1.5px solid #fff;"></span>`
      : '';
    return L.divIcon({
      className: '', // пустой — без дефолтного белого квадрата leaflet-div-icon
      html: `<div style="position:relative;width:${size}px;height:${size}px;border-radius:50%;
        background:${color};border:2px solid ${border};box-shadow:${rings.join(',')};
        color:#fff;display:flex;align-items:center;justify-content:center;
        font:600 ${size < 28 ? 10 : 12}px/1 sans-serif;">${trainLabel(g.index)}${badge}${dot}</div>`,
      iconSize: [size, size],
      iconAnchor: [size / 2, size / 2],
    });
  }

  private popupHtml(g: MapGroup): string {
    const esc = (s: string) => s.replace(/[&<>"]/g, (c) =>
      ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c] as string));
    const rows: string[] = [];
    rows.push(`<div><b>${esc(g.index)}</b> · ${g.vagon_count} ваг.` +
      (g.broshen ? ' · <span style="color:#ff4d4f">брошен</span>' : '') + `</div>`);
    rows.push(`<div>${esc(g.station_oper)}${g.doroga_oper ? ' (' + esc(g.doroga_oper) + ')' : ''}</div>`);
    if (g.oper_s) rows.push(`<div>${esc(g.oper_s)}${g.time_op ? ' · ' + this.fmtDT(g.time_op) : ''}</div>`);
    if (g.stan_nazn) rows.push(`<div>→ ${esc(g.stan_nazn)}${g.rasst != null ? ` · осталось ${g.rasst} км` : ''}</div>`);
    // Время прибытия (решение владельца 07.08.2026): есть план — ТОЛЬКО план;
    // нет плана — прогноз; отцепка без прогноза — время хода и расчёт с
    // предупреждением. Шкала — из профиля предприятия (как диалоги прибытия).
    const inJd = this.timeBase.base() !== 'msk';
    const pick = (jd: string | null, msk: string | null) => (inJd ? jd ?? msk : msk ?? jd);
    const scale = inJd ? 'ЖД' : 'МСК';
    if (g.plan_jd) {
      rows.push(`<div>📅 план (${scale}): ${this.fmtDT(pick(g.plan_jd, g.plan_msk))}</div>`);
    } else if (g.prog_jd) {
      rows.push(`<div>🔮 прогноз (${scale}): ${this.fmtDT(pick(g.prog_jd, g.prog_msk))}</div>`);
    } else {
      rows.push(`<div>⏱ ход: ${fmtHours(g.to_go)} · расчёт (${scale}): ${this.fmtDT(pick(g.rasch_jd, g.rasch_msk))}</div>`);
      rows.push(`<div style="color:#fa8c16">⚠ это только расчёт по времени хода — прогноза нет (отцепка)</div>`);
    }
    const rasch = `расчёт (${scale}): ${this.fmtDT(pick(g.rasch_jd, g.rasch_msk))}`;
    const risk = riskComment(g, rasch);
    if (risk) rows.push(`<div style="color:${risk.color};font-weight:600">${esc(risk.text)}</div>`);
    if (g.status != null) rows.push(`<div>статус: ${esc(this.statusLabel(g.status))}</div>`);
    if (g.mark_text || g.mark_color) {
      rows.push(`<div style="margin-top:4px">${g.mark_color
        ? `<span style="display:inline-block;width:10px;height:10px;border-radius:2px;background:${g.mark_color};margin-right:4px"></span>`
        : ''}${esc(g.mark_text)}</div>`);
    }
    // «Состав (N)» — скроллируемый список подгрупп с цветной полосой marka
    // (перенос gtport CargoItem, без вагонов — они по кнопке).
    if (g.sub_groups?.length) {
      const items = g.sub_groups.map((sg: MapSubgroup) => `
        <div class="cargo" style="border-left:3px solid ${sg.color || '#d9d9d9'}">
          <div><b>${esc(sg.index_main || '—')}</b> · ${esc(sg.station_nach)}</div>
          <div>${esc(sg.cargo_s || sg.cargo_group)} · ${sg.vagon_count} ваг.${sg.ves ? ' / ' + sg.ves.toFixed(1) + ' т' : ''}</div>
          <div class="mut2">${esc(sg.gruzpol_s)}${sg.naznach && sg.naznach !== sg.gruzpol_s ? ' → ' + esc(sg.naznach) : ''}${sg.client ? ' · ' + esc(sg.client) : ''}</div>
        </div>`).join('');
      rows.push(`<div style="margin-top:6px"><b>Состав (${g.sub_groups.length})</b></div>
        <div class="cargo-list">${items}</div>`);
    }
    rows.push(`<div class="btns">
      <button data-act="wagons" data-key="${esc(g.key)}">Вагоны</button>
      <button data-act="mark" data-key="${esc(g.key)}">Пометка…</button>
    </div>`);
    return `<div class="dp-pop">${rows.join('')}</div>`;
  }

  // ── Вагоны группы ─────────────────────────────────────────────────────────
  async openWagons(key: string): Promise<void> {
    this.wagonsKey.set(key);
    this.wagonsLoading.set(true);
    try {
      const data = await this.api.getWagons(key);
      this.wagons.set(data.wagons ?? []);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
      this.wagonsKey.set(null);
    } finally {
      this.wagonsLoading.set(false);
    }
  }

  /** Excel вагонов группы — на клиенте, как прочие выгрузки (xlsx-js-style). */
  async exportWagons(): Promise<void> {
    const XLSX = await import('xlsx-js-style');
    const header = ['№', 'Вагон', 'Накладная', 'Марка груза', 'Вес, т', 'Собственник',
      'Статус', 'Получ.', 'Назн.', 'Ст. отправления', 'Отправитель', 'Погружен', 'Отправление', 'Срок доставки'];
    const rows = this.wagons().map((v) => [
      v.npp_vag ?? '', v.vagon, v.invoice, v.freight, v.ves ?? '', v.owner,
      this.statusLabel(v.status), v.gruzpol_s, v.naznach,
      v.station_nach, v.gruzotpr, this.fmtDT(v.date_nach), this.fmtDT(v.date_otpr),
      this.fmtDT(v.date_dostav),
    ]);
    const ws = XLSX.utils.aoa_to_sheet([header, ...rows]);
    ws['!cols'] = [4, 10, 10, 22, 7, 16, 18, 8, 8, 18, 22, 12, 12, 12].map((wch) => ({ wch }));
    const wb = XLSX.utils.book_new();
    XLSX.utils.book_append_sheet(wb, ws, 'Вагоны');
    XLSX.writeFile(wb, `Вагоны_${(this.wagonsKey() ?? '').replace(/[^\wА-Яа-я-]+/g, '_')}.xlsx`);
  }

  // ── Пометка ───────────────────────────────────────────────────────────────
  openMark(keys: string[]): void {
    // Существующая пометка первой группы — в форму (удобно править/снимать).
    const cur = [...this.groups(), ...this.noCoords()].find((g) => g.key === keys[0]);
    this.markText.set(cur?.mark_text ?? '');
    this.markColor.set(cur?.mark_color ?? '');
    this.markKeys.set(keys);
  }

  async applyMark(clear: boolean): Promise<void> {
    const keys = this.markKeys();
    if (!keys?.length) return;
    this.marking.set(true);
    try {
      const res = await this.api.applyMark({
        keys,
        text: clear ? '' : this.markText().trim(),
        color: clear ? '' : this.markColor(),
      });
      this.msg.success(clear
        ? `Пометка снята: ${res.updated} ваг.`
        : `Помечено: ${res.updated} ваг. в ${keys.length} групп(ах)`);
      this.markKeys.set(null);
      this.clearSelection();
      await this.load();
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.marking.set(false);
    }
  }

  // ── Форматирование (МСК naive — срезами строки, без сдвигов) ─────────────
  fmtDT(ts: string | null): string {
    if (!ts || ts.length < 16) return '—';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)} ${ts.slice(11, 16)}`;
  }

  statusLabel(s: number | null): string {
    return s == null ? '—' : (STATUS_LABELS[s] ?? `статус ${s}`);
  }
}

/** Маркер с нашей меткой «в группе есть брошенный» — для иконки кластера. */
type MarkerWithMeta = L.Marker & { dpBroshen?: boolean };

/**
 * Подпись маркера — середина индекса (номер поезда), правило gtport
 * getNameFromIndex: «Б…» → Б/И, «С…» → С/Ф, иначе символы 6–8 («9370-101-9857»
 * → «101»); короткий индекс — как есть.
 */
function trainLabel(index: string): string {
  if (!index) return '';
  const first = index.charAt(0).toUpperCase();
  if (first === 'Б') return 'Б/И';
  if (first === 'С') return 'С/Ф';
  if (index.length >= 8) return index.substring(5, 8);
  return index;
}

/**
 * Комментарий к времени прибытия по mistake — логика risk() вкладки «Прогноз
 * прибытия/выгрузки» (gtport getRiskIndicator): для поезда в плане mistake < 0
 * значит «не успевает на нитку», без плана mistake > 0 — «опережает нитку».
 * Тексты конкретные (уточнение владельца 07.08.2026): «опаздывает/опережает
 * на СКОЛЬКО», для опозданий — с расчётным временем (rasch передаётся уже
 * отформатированным в шкале профиля). Б/И и сборные не оцениваются (индекс
 * не 4-3-4), прибывшим комментарий не нужен.
 */
function riskComment(g: MapGroup, rasch: string): { text: string; color: string; kind: 'bros' | 'late' | 'ahead' } | null {
  if (g.arrived) return null;
  if (!/^\d{4}-\d{3}-\d{4}$/.test(g.index)) return null;
  const inPlan = !!g.plan_jd;
  const mistake = g.mistake ?? 0;
  const togo = g.to_go ?? 0;
  const CRIT = '#c62828', WARN = '#e65100', WATCH = '#b58b00';
  if (inPlan && g.broshen) return { text: 'Нитка под угрозой срыва: поезд брошен', color: WARN, kind: 'bros' };
  if (inPlan) {
    if (mistake <= -1) {
      return { text: `ПЛАН СЛЕТИТ: опаздывает на ${fmtDays(-mistake)} — ${rasch}`, color: CRIT, kind: 'late' };
    }
    if (mistake < -0.3) {
      return { text: `Опаздывает на ${fmtDays(-mistake)} — ${rasch}`, color: WARN, kind: 'late' };
    }
    return null;
  }
  if (mistake > 1) {
    if (togo <= 24) {
      return { text: `Опережает нитку на ${fmtDays(mistake)} у порта — придержат или бросят`, color: CRIT, kind: 'ahead' };
    }
    const ratio = togo > 0 ? mistake / (togo / 24) : Infinity;
    if (ratio > 0.5) {
      return { text: `Опережает нитку на ${fmtDays(mistake)} — вероятно бросание в пути`, color: WARN, kind: 'ahead' };
    }
    return { text: `Опережает нитку на ${fmtDays(mistake)}`, color: WATCH, kind: 'ahead' };
  }
  return null;
}

/** Длительность в сутках → человеку: до суток — часами, дальше — сутками. */
function fmtDays(d: number): string {
  return d < 1 ? `${Math.round(d * 24)} ч` : `${d.toFixed(1)} сут`;
}

/** Часы хода человеку: до суток — часами, дальше — сутками с десятыми. */
function fmtHours(h: number | null): string {
  if (h == null) return '—';
  return h < 24 ? `${Math.round(h)} ч` : `${(h / 24).toFixed(1)} сут`;
}

/** Стили собственного слоя (map.geojson) — дословно из gtport GeoJsonLayer. */
function geoJsonStyle(feature?: GeoJSON.Feature): L.PathOptions {
  const props = (feature?.properties ?? {}) as Record<string, unknown>;
  const type = feature?.geometry?.type;
  if (type === 'LineString' || type === 'MultiLineString') {
    return {
      color: '#1B1B1C',
      weight: props['class'] === 'mainlines' ? 1 : 0.7,
      opacity: 0.8,
      lineCap: 'round',
      lineJoin: 'round',
      fill: false,
    };
  }
  if (type === 'Polygon' || type === 'MultiPolygon') {
    return {
      fillColor: (props['fill'] as string) || '#f2efe9',
      weight: (props['stroke-width'] as number) ?? 0,
      opacity: (props['stroke-opacity'] as number) ?? 1,
      color: (props['stroke'] as string) || (props['fill'] as string) || '#f2efe9',
      fillOpacity: (props['fill-opacity'] as number) ?? 1,
    };
  }
  return { fillColor: '#f2efe9', weight: 0, opacity: 1, color: '#f2efe9', fillOpacity: 1 };
}
