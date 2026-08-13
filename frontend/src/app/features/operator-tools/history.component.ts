import {
  Component, ElementRef, OnInit, effect, inject, signal, computed, viewChild,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzDatePickerModule } from 'ng-zorro-antd/date-picker';
import { NzDropDownModule, NzContextMenuService, NzDropdownMenuComponent } from 'ng-zorro-antd/dropdown';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzMessageService } from 'ng-zorro-antd/message';
import { NzPaginationModule } from 'ng-zorro-antd/pagination';
import { NzSelectModule } from 'ng-zorro-antd/select';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { apiErrorMessage } from '../../core/api/api-error';
import { blobErrorMessage } from '../../shared/blob-error';
import { todayMsk } from '../../shared/msk-date';
import { VagonTrailModalComponent } from '../home/vagon-trail-modal.component';
import { TrainsTarget } from '../trains/trains-api.service';
import {
  HistoryApiService, HistoryFilter, HistoryRow, HistorySearchRequest, HistorySort,
} from './history-api.service';

/** Статусы DPmodule (docs/STATUSES.md) — как на «Поездах в движении». */
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

/** Колонка таблицы: ключ сортировки API + подпись + минимальная ширина. */
interface HistCol {
  key: string;
  label: string;
  w: number;
}

/** 27 колонок: порядок gtport + вторая колонка выгрузки (ЖД, 14.08.2026);
 * замены DPmodule: Собст=owner, Марка=freight,
 *  ГТД=gtd_number, «Перегруз»=peregruz (в gtport «Отгружен» ← info_3). */
const COLS: HistCol[] = [
  { key: 'vagon', label: 'Вагон', w: 90 },
  { key: 'invoice_main', label: 'Накладная Р', w: 100 },
  { key: 'invoice', label: 'Накладная', w: 100 },
  { key: 'index_main', label: 'Индекс Р', w: 120 },
  { key: 'index_pp', label: 'Индекс ПП', w: 120 },
  { key: 'date_nach_d', label: 'Дата погр', w: 90 },
  { key: 'station_nach', label: 'Ст. погр', w: 120 },
  { key: 'gruzotpr', label: 'Грузоотпр', w: 140 },
  { key: 'gruzpol_s', label: 'Грузопол', w: 95 },
  { key: 'naznach', label: 'Назначение', w: 95 },
  { key: 'cargo_s', label: 'Груз', w: 140 },
  { key: 'ves', label: 'Вес', w: 70 },
  { key: 'client', label: 'Клиент', w: 120 },
  { key: 'status', label: 'Статус', w: 130 },
  { key: 'date_dostav', label: 'Срок доставки', w: 110 },
  { key: 'date_prib_d', label: 'Прибыл', w: 90 },
  { key: 'plan_jd', label: 'ПП', w: 90 },
  { key: 'delay', label: 'Просрочка', w: 85 },
  // Две колонки выгрузки (решение владельца 14.08.2026): факт — МСК-время
  // операции, ЖД — учётные сутки («час ≥ 18 → +1»). Фильтр «Дата выгрузки»
  // и счётчики Оперативки/грузовой работы считают по ЖД — вечерняя выгрузка
  // с одной колонкой выглядела ошибкой фильтра.
  { key: 'date_vigr', label: 'Выгружен (факт)', w: 110 },
  { key: 'date_vigr_d', label: 'Выгружен (ЖД)', w: 105 },
  { key: 'place_vigr', label: 'Терминал', w: 90 },
  { key: 'frost', label: 'Смерзаемость', w: 105 },
  { key: 'owner', label: 'Собст', w: 140 },
  { key: 'freight', label: 'Марка', w: 130 },
  { key: 'gtd_number', label: 'ГТД', w: 110 },
  { key: 'shipments', label: 'Судовая', w: 110 },
  { key: 'peregruz', label: 'Перегруз', w: 90 },
];

/**
 * Экран «Работа с историческими данными» — перенос gtport OperatorToolsHistory.
 * Read-only таблица по vagon_history: панель фильтров (списки вагонов и
 * накладных, три диапазона дат, станции погрузки, чипы терминалов), данные —
 * только по «Применить»; верхний горизонтальный скроллбар синхронизирован с
 * таблицей (как в оригинале — у таблицы свой горизонтальный скролл скрыт).
 *
 * Отходы от gtport (решения владельца 04.08.2026):
 * - серверные пагинация и сортировка по всему набору (в gtport — жёсткий
 *   limit 5000 без листания и клиентская сортировка загруженного куска);
 * - терминалы чипов — из реестра ports с цветами (не хардкод АЭ/ГУТ-2/УТ-1);
 * - колонки: Собст = owner, Марка = freight_exact_name, ГТД = gtd_number,
 *   «Перегруз» = peregruz; статусы — полный набор DPmodule;
 * - Excel строит сервер по всему фильтру (потолок 100 тыс. строк);
 * - ПКМ по строке — «История движения вагона» (в gtport экран был без ПКМ).
 */
@Component({
  selector: 'app-history',
  imports: [
    FormsModule, NzButtonModule, NzDatePickerModule, NzDropDownModule, NzIconModule,
    NzInputModule, NzPaginationModule, NzSelectModule, NzTagModule, NzTooltipModule,
    VagonTrailModalComponent,
  ],
  template: `
    <div class="hist">
      <!-- ── Шапка ─────────────────────────────────────────────────────── -->
      <div class="bar head">
        <b>История вагонов</b>
        <span class="spacer"></span>
        <button nz-button nzSize="small" (click)="exportExcel()" [nzLoading]="exporting()"
                [disabled]="!applied() || !total()" nz-tooltip="Экспорт в Excel — весь результат фильтра">
          <span nz-icon nzType="download"></span>
        </button>
        <button nz-button nzSize="small" (click)="showFilters.set(!showFilters())"
                [nz-tooltip]="showFilters() ? 'Скрыть расширенные фильтры' : 'Показать расширенные фильтры'">
          <span nz-icon [nzType]="showFilters() ? 'up' : 'down'"></span>
        </button>
      </div>

      <!-- ── Панель фильтров ───────────────────────────────────────────── -->
      <div class="filters">
        <div class="frow">
          <div class="fcol">
            <div class="ftitle">Список вагонов
              @if (vagonNums().length) { <nz-tag class="cnt">{{ vagonNums().length }}</nz-tag> }
            </div>
            <textarea nz-input rows="2" class="w-list" placeholder="62158654, 62158655, …"
                      [ngModel]="vagonText()" (ngModelChange)="vagonText.set($event)"></textarea>
          </div>
          <div class="fcol">
            <div class="ftitle">Список накладных
              @if (invoiceNums().length) { <nz-tag class="cnt">{{ invoiceNums().length }}</nz-tag> }
            </div>
            <textarea nz-input rows="2" class="w-list" placeholder="ЭЛ123456, ЭЛ123457, …"
                      [ngModel]="invoiceText()" (ngModelChange)="invoiceText.set($event)"></textarea>
          </div>
          <div class="fbtns">
            <button nz-button nzType="primary" (click)="apply()" [nzLoading]="loading()">
              <span nz-icon nzType="search"></span> Применить
            </button>
            <button nz-button (click)="resetAll()" nz-tooltip="Сбросить все фильтры">
              <span nz-icon nzType="clear"></span> Сброс
            </button>
          </div>
        </div>

        @if (showFilters()) {
          <div class="frow">
            <div class="fcol">
              <div class="ftitle">Дата погрузки</div>
              <nz-range-picker nzSize="small" nzFormat="dd.MM.yyyy"
                               [ngModel]="rangeNach()" (ngModelChange)="rangeNach.set($event)" />
            </div>
            <div class="fcol">
              <div class="ftitle">Дата прибытия</div>
              <nz-range-picker nzSize="small" nzFormat="dd.MM.yyyy"
                               [ngModel]="rangePrib()" (ngModelChange)="rangePrib.set($event)" />
            </div>
            <div class="fcol">
              <div class="ftitle">Дата выгрузки</div>
              <nz-range-picker nzSize="small" nzFormat="dd.MM.yyyy"
                               [ngModel]="rangeVigr()" (ngModelChange)="rangeVigr.set($event)" />
            </div>
          </div>
          <div class="frow">
            <div class="fcol grow">
              <div class="ftitle">Станция погрузки</div>
              <nz-select nzMode="multiple" nzSize="small" nzShowSearch nzAllowClear
                         class="w-stations" nzPlaceHolder="все станции"
                         [ngModel]="selStations()" (ngModelChange)="selStations.set($event)">
                @for (s of stations(); track s) { <nz-option [nzValue]="s" [nzLabel]="s" /> }
              </nz-select>
            </div>
          </div>
          <div class="frow chips">
            <div class="fcol">
              <div class="ftitle">Грузополучатель</div>
              @for (t of targets(); track t.name) {
                <!-- background-color, не background: nz-tag сам биндит это свойство
                     на хосте и затирал бы шорткат при первой отрисовке. -->
                <nz-tag [class.on]="selGruzpol().has(t.name)"
                        [style.background-color]="selGruzpol().has(t.name) ? (t.color || undefined) : undefined"
                        (click)="toggle(selGruzpol, t.name)">{{ t.name }}</nz-tag>
              }
            </div>
            <div class="fcol">
              <div class="ftitle">Назначение</div>
              @for (t of targets(); track t.name) {
                <nz-tag [class.on]="selNaznach().has(t.name)"
                        [style.background-color]="selNaznach().has(t.name) ? (t.color || undefined) : undefined"
                        (click)="toggle(selNaznach, t.name)">{{ t.name }}</nz-tag>
              }
            </div>
            <div class="fcol">
              <div class="ftitle">Место выгрузки</div>
              @for (t of targets(); track t.name) {
                <nz-tag [class.on]="selPlace().has(t.name)"
                        [style.background-color]="selPlace().has(t.name) ? (t.color || undefined) : undefined"
                        (click)="togglePlace(t.name)">{{ t.name }}</nz-tag>
              }
              <nz-tag [class.on]="notUnloaded()" class="gray" (click)="toggleNotUnloaded()">не выгруж.</nz-tag>
              <nz-tag [class.on]="onlyOverdue()" class="gray" (click)="onlyOverdue.set(!onlyOverdue())"
                      nz-tooltip nzTooltipTitle="Только рейсы с просрочкой доставки (прибыл позже нормативного срока)">просрочка</nz-tag>
            </div>
          </div>
        }
      </div>

      <!-- ── Статистика ────────────────────────────────────────────────── -->
      <div class="stat">
        @if (applied()) {
          Найдено: <b>{{ total() }}</b> записей
        } @else {
          Заполните фильтры и нажмите «Применить»
        }
      </div>

      <!-- ── Верхний скроллбар, синхронизированный с таблицей ──────────── -->
      @if (rows().length) {
        <div #topScroll class="top-scroll" (scroll)="syncFromTop()">
          <div class="top-spacer" [style.width.px]="scrollWidth()"></div>
        </div>
      }

      <!-- ── Таблица ───────────────────────────────────────────────────── -->
      <div #tblWrap class="tbl-wrap" (scroll)="syncFromTable()">
        @if (applied() && !loading() && !rows().length) {
          <div class="empty">
            <span nz-icon nzType="file-search" class="empty-ic"></span>
            <div class="empty-ttl">Ничего не найдено</div>
            <div class="mut">Попробуйте изменить параметры фильтрации</div>
          </div>
        } @else if (rows().length) {
          <table class="dp-tbl hist-tbl">
            <thead>
              <tr>
                @for (c of cols; track c.key) {
                  <th [style.min-width.px]="c.w" (click)="sortBy(c.key)"
                      [class.sorted]="sort().by === c.key">
                    {{ c.label }}
                    @if (sort().by === c.key) {
                      <span nz-icon [nzType]="sort().dir === 'asc' ? 'caret-up' : 'caret-down'"></span>
                    }
                  </th>
                }
              </tr>
            </thead>
            <tbody>
              @for (r of rows(); track r.id) {
                <tr (contextmenu)="openMenu($event, r, rowMenu)">
                  <td class="c"><b class="blue">{{ r.vagon }}</b></td>
                  <td class="c">{{ r.invoice_main }}</td>
                  <td class="c">{{ r.invoice }}</td>
                  <td class="c">{{ r.index_main }}</td>
                  <td class="c">{{ r.index_pp }}</td>
                  <td class="c">{{ fmtD(r.date_nach_d) }}</td>
                  <td>{{ r.station_nach }}</td>
                  <td class="ell" [title]="r.gruzotpr">{{ r.gruzotpr }}</td>
                  <td class="c nowrap">
                    @if (r.gruzpol_s) { <span class="dot" [style.background]="targetColor(r.gruzpol_s)"></span> }
                    {{ r.gruzpol_s }}
                  </td>
                  <td class="c">{{ r.naznach }}</td>
                  <td class="ell" [title]="r.cargo_s">{{ r.cargo_s }}</td>
                  <td class="c">{{ r.ves != null ? r.ves.toFixed(2) : '' }}</td>
                  <td class="ell" [title]="r.client">{{ r.client }}</td>
                  <td class="c nowrap">
                    <span class="dot st" [style.background]="statusColor(r)"></span>
                    <span [style.color]="statusColor(r)">{{ statusLabel(r) }}</span>
                  </td>
                  <td class="c nowrap">
                    @if (r.date_dostav) {
                      <span nz-icon [nzType]="isOverdue(r.date_dostav) ? 'warning' : 'check-circle'"
                            [class.overdue]="isOverdue(r.date_dostav)" [class.ok]="!isOverdue(r.date_dostav)"></span>
                      {{ fmtD(r.date_dostav) }}
                    }
                  </td>
                  <td class="c">{{ fmtD(r.date_prib_d) }}</td>
                  <td class="c">{{ fmtD(r.plan_jd) }}</td>
                  <td class="c" [class.overdue]="(r.delay ?? 0) > 0">
                    @if (r.delay != null && r.delay !== 0) { {{ r.delay }} дн }
                  </td>
                  <td class="c">{{ fmtD(r.date_vigr) }}</td>
                  <td class="c">{{ fmtD(r.date_vigr_d) }}</td>
                  <td class="c">
                    @if (r.place_vigr) {
                      <nz-tag class="thin term" [style.background-color]="placeColor(r.place_vigr)">{{ r.place_vigr }}</nz-tag>
                    }
                  </td>
                  <td class="c">{{ r.frost ?? '' }}</td>
                  <td class="ell" [title]="r.owner">{{ r.owner }}</td>
                  <td class="ell" [title]="r.freight">{{ r.freight }}</td>
                  <td class="c">{{ r.gtd_number }}</td>
                  <td class="c">{{ r.shipments }}</td>
                  <td class="c">{{ r.peregruz }}</td>
                </tr>
              }
            </tbody>
          </table>
        }
      </div>

      <!-- ── Пагинация ─────────────────────────────────────────────────── -->
      @if (applied() && total() > 0) {
        <div class="bar foot">
          <nz-pagination nzSize="small" nzShowSizeChanger
                         [nzPageIndex]="pageIndex()" [nzPageSize]="pageSize()" [nzTotal]="total()"
                         [nzPageSizeOptions]="[100, 500, 1000]"
                         (nzPageIndexChange)="onPage($event)" (nzPageSizeChange)="onPageSize($event)" />
          <span class="spacer"></span>
          <span class="mut">Всего: {{ total() }} записей</span>
        </div>
      }

      <!-- ── ПКМ по строке ─────────────────────────────────────────────── -->
      <nz-dropdown-menu #rowMenu="nzDropdownMenu">
        <ul nz-menu>
          <li nz-menu-item (click)="openTrail()">История движения вагона {{ ctxRow()?.vagon }}…</li>
        </ul>
      </nz-dropdown-menu>

      @if (trailRow(); as tr) {
        <app-vagon-trail-modal [vagonId]="tr.id" [vagon]="tr.vagon" (closed)="trailRow.set(null)" />
      }
    </div>
  `,
  styles: [`
    .hist { display: flex; flex-direction: column; gap: var(--space-sm); }
    .bar { display: flex; align-items: center; gap: var(--space-sm); flex-wrap: wrap; }
    .bar.head, .bar.foot { padding: var(--space-xs) var(--space-md); background: var(--color-bg-surface);
                           border-radius: var(--radius-card); box-shadow: var(--shadow-card); }
    .spacer { flex: 1 1 auto; }
    .mut { color: var(--color-text-secondary); font-size: var(--font-size-sm); }
    /* Панель фильтров */
    .filters { display: flex; flex-direction: column; gap: var(--space-sm);
               padding: var(--space-xs) var(--space-md) var(--space-sm); background: var(--color-bg-surface);
               border-radius: var(--radius-card); box-shadow: var(--shadow-card); }
    .frow { display: flex; align-items: flex-end; gap: var(--space-lg); flex-wrap: wrap; }
    .frow.chips { align-items: flex-start; }
    .fcol.grow { flex: 1 1 420px; }
    .ftitle { font-size: var(--font-size-sm); color: var(--color-text-secondary); margin-bottom: 2px; }
    .fbtns { display: flex; gap: var(--space-sm); padding-bottom: 2px; }
    .w-list { width: 300px; resize: vertical; }
    .w-stations { width: 100%; max-width: 700px; }
    nz-tag.cnt { margin-left: 6px; line-height: 14px; padding: 0 5px; font-size: var(--font-size-sm); }
    .fcol nz-tag { cursor: pointer; user-select: none; }
    nz-tag.on { border-color: var(--color-primary, #1677ff); background: var(--color-primary-bg, #e6f4ff); }
    nz-tag.gray.on { background: #e0e0e0; border-color: #bdbdbd; }
    /* Статистика */
    .stat { padding: 4px var(--space-md); background: var(--color-bg-container-secondary, #f0f0f0);
            border-radius: var(--radius-card); font-size: var(--font-size-sm); }
    /* Верхний скролл: единственный горизонтальный скроллбар таблицы (как в
       gtport — у самой таблицы скролл по X скрыт, крутит эта полоса). */
    .top-scroll { overflow-x: auto; overflow-y: hidden; height: 14px;
                  background: var(--color-bg-container-secondary, #f5f5f5); border-radius: var(--radius-card); }
    .top-spacer { height: 1px; }
    .tbl-wrap { overflow-x: hidden; overflow-y: auto; max-height: 64vh;
                background: var(--color-bg-surface); border-radius: var(--radius-card);
                box-shadow: var(--shadow-card); }
    .hist-tbl { min-width: 2000px; }
    .hist-tbl th { cursor: pointer; white-space: nowrap; user-select: none; }
    .hist-tbl th.sorted { color: var(--color-primary, #1677ff); }
    .hist-tbl td { white-space: nowrap; }
    .hist-tbl td.c { text-align: center; }
    .hist-tbl td.ell { max-width: 180px; overflow: hidden; text-overflow: ellipsis; }
    .nowrap { white-space: nowrap; }
    .blue { color: var(--color-primary, #1677ff); font-variant-numeric: tabular-nums; }
    .dot { display: inline-block; width: 10px; height: 10px; border-radius: 50%;
           border: 1px solid var(--color-border, #c8c8c8); vertical-align: -1px; }
    .dot.st { width: 8px; height: 8px; border: none; }
    nz-tag.thin, .thin { margin: 0; line-height: 16px; padding: 0 5px; font-size: var(--font-size-sm); }
    .overdue { color: var(--color-error, #ff4d4f); font-weight: 500; }
    .ok { color: var(--color-success, #52c41a); }
    .empty { text-align: center; padding: 36px 0; }
    .empty-ic { font-size: 44px; color: var(--color-text-secondary); }
    .empty-ttl { font-size: 16px; margin: 8px 0 2px; }
  `],
})
export class HistoryComponent implements OnInit {
  private readonly api = inject(HistoryApiService);
  private readonly msg = inject(NzMessageService);
  private readonly ctxMenu = inject(NzContextMenuService);

  readonly cols = COLS;

  // Словари панели фильтров (meta).
  readonly targets = signal<TrainsTarget[]>([]);
  readonly stations = signal<string[]>([]);

  // Поля фильтров (правки не влияют на выборку до «Применить»).
  readonly vagonText = signal('');
  readonly invoiceText = signal('');
  readonly rangeNach = signal<Date[] | null>(null);
  readonly rangePrib = signal<Date[] | null>(null);
  readonly rangeVigr = signal<Date[] | null>(null);
  readonly selStations = signal<string[]>([]);
  readonly selGruzpol = signal<Set<string>>(new Set());
  readonly selNaznach = signal<Set<string>>(new Set());
  readonly selPlace = signal<Set<string>>(new Set());
  readonly notUnloaded = signal(false);
  readonly onlyOverdue = signal(false);
  readonly showFilters = signal(true);

  /** Распознанные номера — как в gtport: регулярка по любому тексту (можно
   *  вставить колонку из Excel, разделители не важны). */
  readonly vagonNums = computed(() => this.vagonText().match(/\b\d{8}\b/g) ?? []);
  readonly invoiceNums = computed(() =>
    (this.invoiceText().match(/[А-ЯЁа-яё]{2}\d{6}/g) ?? []).map((s) => s.toUpperCase()));

  // Применённый фильтр (снапшот на момент «Применить») + состояние выдачи.
  readonly applied = signal<HistoryFilter | null>(null);
  readonly sort = signal<HistorySort>({ by: 'date_nach_d', dir: 'desc' });
  readonly pageIndex = signal(1);
  readonly pageSize = signal(100);
  readonly rows = signal<HistoryRow[]>([]);
  readonly total = signal(0);
  readonly loading = signal(false);
  readonly exporting = signal(false);

  // ПКМ и модалка «История движения вагона».
  readonly ctxRow = signal<HistoryRow | null>(null);
  readonly trailRow = signal<HistoryRow | null>(null);

  // Верхний скроллбар: ширина спейсера = scrollWidth таблицы.
  private readonly topScroll = viewChild<ElementRef<HTMLDivElement>>('topScroll');
  private readonly tblWrap = viewChild<ElementRef<HTMLDivElement>>('tblWrap');
  readonly scrollWidth = signal(0);

  constructor() {
    // Пересчёт ширины спейсера после отрисовки строк (scrollWidth до рендера = 0).
    effect(() => {
      this.rows();
      setTimeout(() => this.updateScrollWidth());
    });
  }

  ngOnInit(): void {
    void this.loadMeta();
  }

  private async loadMeta(): Promise<void> {
    try {
      const meta = await this.api.meta();
      this.targets.set(meta.targets ?? []);
      this.stations.set(meta.stations ?? []);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    }
  }

  // ── Фильтры ──────────────────────────────────────────────────────────────
  toggle(sig: typeof this.selGruzpol, name: string): void {
    const next = new Set(sig());
    next.has(name) ? next.delete(name) : next.add(name);
    sig.set(next);
  }

  /** Место выгрузки несовместимо с «не выгруж.» (логика gtport). */
  togglePlace(name: string): void {
    this.notUnloaded.set(false);
    this.toggle(this.selPlace, name);
  }

  toggleNotUnloaded(): void {
    const next = !this.notUnloaded();
    if (next) this.selPlace.set(new Set());
    this.notUnloaded.set(next);
  }

  /** yyyy-MM-dd локально (без toISOString — тот сдвигает сутки по UTC). */
  private ymd(d: Date): string {
    const p = (n: number) => String(n).padStart(2, '0');
    return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
  }

  private buildFilter(): HistoryFilter {
    const f: HistoryFilter = {};
    const range = (r: Date[] | null): [string, string] | null =>
      r && r.length === 2 && r[0] && r[1] ? [this.ymd(r[0]), this.ymd(r[1])] : null;
    const nach = range(this.rangeNach());
    if (nach) [f.date_nach_d_from, f.date_nach_d_to] = nach;
    const prib = range(this.rangePrib());
    if (prib) [f.date_prib_d_from, f.date_prib_d_to] = prib;
    const vigr = range(this.rangeVigr());
    if (vigr) [f.date_vigr_d_from, f.date_vigr_d_to] = vigr;
    if (this.selGruzpol().size) f.gruzpol_s = [...this.selGruzpol()];
    if (this.selNaznach().size) f.naznach = [...this.selNaznach()];
    if (this.selPlace().size) f.place_vigr = [...this.selPlace()];
    if (this.notUnloaded()) f.not_unloaded = true;
    if (this.onlyOverdue()) f.only_overdue = true;
    if (this.vagonNums().length) f.vagons = this.vagonNums();
    if (this.invoiceNums().length) f.invoices = this.invoiceNums();
    if (this.selStations().length) f.station_nach = this.selStations();
    return f;
  }

  apply(): void {
    this.applied.set(this.buildFilter());
    this.pageIndex.set(1);
    void this.fetch();
  }

  resetAll(): void {
    this.vagonText.set('');
    this.invoiceText.set('');
    this.rangeNach.set(null);
    this.rangePrib.set(null);
    this.rangeVigr.set(null);
    this.selStations.set([]);
    this.selGruzpol.set(new Set());
    this.selNaznach.set(new Set());
    this.selPlace.set(new Set());
    this.notUnloaded.set(false);
    this.onlyOverdue.set(false);
    this.applied.set(null);
    this.rows.set([]);
    this.total.set(0);
    this.pageIndex.set(1);
    this.sort.set({ by: 'date_nach_d', dir: 'desc' });
    this.msg.info('Фильтры сброшены');
  }

  // ── Выдача ───────────────────────────────────────────────────────────────
  private request(): HistorySearchRequest {
    return {
      filter: this.applied() ?? {},
      sort: this.sort(),
      page: { limit: this.pageSize(), offset: (this.pageIndex() - 1) * this.pageSize() },
    };
  }

  private async fetch(): Promise<void> {
    if (!this.applied()) return;
    this.loading.set(true);
    try {
      const resp = await this.api.search(this.request());
      this.rows.set(resp.rows ?? []);
      this.total.set(resp.total ?? 0);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.loading.set(false);
    }
  }

  /** Серверная сортировка по всему набору: новый столбец — asc (как MUI в
   *  gtport), повторный клик — переворот; страница сбрасывается на первую. */
  sortBy(key: string): void {
    if (!this.applied()) return;
    const s = this.sort();
    this.sort.set(s.by === key ? { by: key, dir: s.dir === 'asc' ? 'desc' : 'asc' } : { by: key, dir: 'asc' });
    this.pageIndex.set(1);
    void this.fetch();
  }

  onPage(index: number): void {
    this.pageIndex.set(index);
    void this.fetch();
  }

  onPageSize(size: number): void {
    this.pageSize.set(size);
    this.pageIndex.set(1);
    void this.fetch();
  }

  // ── Excel ────────────────────────────────────────────────────────────────
  async exportExcel(): Promise<void> {
    if (!this.applied()) return;
    this.exporting.set(true);
    try {
      const blob = await this.api.excel(this.request());
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      const d = new Date();
      const p = (n: number) => String(n).padStart(2, '0');
      a.download = `история_вагонов_${p(d.getDate())}.${p(d.getMonth() + 1)}.${d.getFullYear()}.xlsx`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      this.msg.error(await blobErrorMessage(err));
    } finally {
      this.exporting.set(false);
    }
  }

  // ── ПКМ → «История движения вагона» ──────────────────────────────────────
  openMenu(ev: MouseEvent, r: HistoryRow, menu: NzDropdownMenuComponent): void {
    ev.preventDefault();
    this.ctxRow.set(r);
    this.ctxMenu.create(ev, menu);
  }

  openTrail(): void {
    const r = this.ctxRow();
    if (r) this.trailRow.set(r);
  }

  // ── Верхний скроллбар ────────────────────────────────────────────────────
  syncFromTop(): void {
    const top = this.topScroll()?.nativeElement;
    const tbl = this.tblWrap()?.nativeElement;
    if (top && tbl) tbl.scrollLeft = top.scrollLeft;
  }

  syncFromTable(): void {
    const top = this.topScroll()?.nativeElement;
    const tbl = this.tblWrap()?.nativeElement;
    if (top && tbl) top.scrollLeft = tbl.scrollLeft;
  }

  private updateScrollWidth(): void {
    const tbl = this.tblWrap()?.nativeElement;
    this.scrollWidth.set(tbl ? tbl.scrollWidth : 0);
  }

  // ── Форматирование и цвета ──────────────────────────────────────────────
  /** дд.ММ.гггг (полный год — как в gtport-таблице). */
  fmtD(ts: string | null): string {
    if (!ts || ts.length < 10) return '';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)}.${ts.slice(0, 4)}`;
  }

  /** Правило gtport: место + дата выгрузки → «выгружен» поверх кода статуса. */
  statusLabel(r: HistoryRow): string {
    if (r.place_vigr && r.date_vigr) return 'выгружен';
    if (r.status == null) return 'неизвестен';
    return STATUS_LABELS[r.status] ?? `статус ${r.status}`;
  }

  statusColor(r: HistoryRow): string {
    if (r.place_vigr && r.date_vigr) return '#52c41a';
    if (r.status == null) return '#8c8c8c';
    return STATUS_COLORS[r.status] ?? '#8c8c8c';
  }

  targetColor(name: string): string {
    return this.targets().find((t) => t.name === name)?.color || 'transparent';
  }

  /** Чип терминала — полупрозрачный цвет порта (gtport: color + '40'). */
  placeColor(name: string): string | undefined {
    const c = this.targets().find((t) => t.name === name)?.color;
    return c ? c + '40' : undefined;
  }

  /** Срок доставки просрочен: дата (без времени) раньше сегодняшней МСК. */
  isOverdue(dateDostav: string | null): boolean {
    if (!dateDostav || dateDostav.length < 10) return false;
    return dateDostav.slice(0, 10) < todayMsk();
  }
}
