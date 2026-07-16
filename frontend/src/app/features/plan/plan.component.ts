import { Component, OnDestroy, OnInit, ViewChild, WritableSignal, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { HttpErrorResponse } from '@angular/common/http';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzCardModule } from 'ng-zorro-antd/card';
import { NzAlertModule } from 'ng-zorro-antd/alert';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { NzTableModule } from 'ng-zorro-antd/table';
import { NzSelectModule } from 'ng-zorro-antd/select';
import { NzCheckboxModule } from 'ng-zorro-antd/checkbox';
import { NzUploadModule, NzUploadFile } from 'ng-zorro-antd/upload';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { NzTabsModule } from 'ng-zorro-antd/tabs';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { apiErrorMessage } from '../../core/api/api-error';
import { PlanApiService, PlanApplyResult, PlanGrid, PlanNitka, PlanSummary, PreparePlanResult, SFCandidate, SFRow } from './plan-api.service';
import { PlanStatusPanelComponent } from './plan-status-panel.component';
import { IndexInputComponent } from './index-input.component';

/**
 * Станции плана подвода со встроенным профилем на бэке (internal/parser/plan/
 * profile.go, ResolveProfile). Появится профиль из БД — добавить строку.
 */
const STATION_OPTIONS: { code: string; label: string }[] = [
  { code: 'ma', label: 'Мыс Астафьева' },
  { code: 'nk', label: 'Находка' },
];

/**
 * Раздел «План подвода»: загрузка xlsx-плана станции (разбор + матч вагонов +
 * простановка PlanMsk) и таблица плана как в оригинале GTport — столбцы портов
 * (по данным файла), «Состав» сматченных групп, строка «Остаток на 18:00»,
 * выбор загрузки из истории. Времена — МСК (правило «час ≥ 18 → −сутки» уже в БД).
 */
@Component({
  selector: 'app-plan',
  imports: [
    FormsModule, NzButtonModule, NzCardModule, NzAlertModule, NzTagModule,
    NzTableModule, NzSelectModule, NzCheckboxModule, NzUploadModule, NzIconModule,
    NzSpinModule, NzTabsModule, NzTooltipModule, NzModalModule,
    PlanStatusPanelComponent, IndexInputComponent,
  ],
  template: `
    <div class="page">
      <app-plan-status-panel />
      <nz-card class="card">
        <nz-tabs class="plan-tabs" [nzSelectedIndex]="selectedIndex()" (nzSelectedIndexChange)="onTabChange($event)">
          @for (o of stationOptions; track o.code) {
            <nz-tab [nzTitle]="o.label"></nz-tab>
          }
        </nz-tabs>

        <div class="controls">
          <span class="lbl">План:</span>
          <nz-select
            [ngModel]="selectedPlanId()"
            (ngModelChange)="onPlanChange($event)"
            [nzDisabled]="!plans().length"
            nzPlaceHolder="—"
            style="width: 260px"
          >
            @for (p of plans(); track p.id) {
              <nz-option [nzValue]="p.id" [nzLabel]="planLabel(p)" />
            }
          </nz-select>

          <nz-upload nzAccept=".xlsx" [nzShowUploadList]="false" [nzBeforeUpload]="beforeUpload">
            <button nz-button nzType="text" nz-tooltip nzTooltipTitle="Загрузить план" [nzLoading]="busyUpload()">
              <span nz-icon nzType="upload"></span>
            </button>
          </nz-upload>

          <button
            nz-button
            nzType="text"
            nz-tooltip
            nzTooltipTitle="Пересчитать план на текущей дислокации (без повторной загрузки файла)"
            [nzLoading]="busyRecalc()"
            [disabled]="selectedPlanId() == null"
            (click)="recalc()"
          >
            <span nz-icon nzType="sync"></span>
          </button>

          <button nz-button nzType="text" nz-tooltip nzTooltipTitle="Обновить" (click)="refresh()">
            <span nz-icon nzType="reload"></span>
          </button>

          <button nz-button nzType="text" nz-tooltip nzTooltipTitle="Печать" [disabled]="!grid()" (click)="printTable()">
            <span nz-icon nzType="printer"></span>
          </button>

          <button
            nz-button
            nzType="text"
            nz-tooltip
            nzTooltipTitle="План подвода: вкладки — станции, «План» — выбор загрузки из истории, «Показать чужие» — строки и столбцы не наших терминалов. Времена — МСК."
          >
            <span nz-icon nzType="info-circle"></span>
          </button>

          <label nz-checkbox [ngModel]="showForeign()" (ngModelChange)="showForeign.set($event)">
            Показать чужие
          </label>

          <span class="spacer"></span>
          @if (grid(); as g) {
            <span class="summary">
              ниток {{ g.plan.nitki }} · сопоставлено {{ g.plan.matched }} · вагонов застолблено {{ g.plan.stamped }}
            </span>
          }
        </div>

        @if (uploadMsg()) {
          <nz-alert class="msg" nzType="success" [nzMessage]="uploadMsg()!" nzShowIcon nzCloseable />
        }
        @if (error()) {
          <nz-alert class="msg" nzType="error" [nzMessage]="error()!" nzShowIcon nzCloseable />
        }
      </nz-card>

      <nz-card class="card">
        <nz-spin [nzSpinning]="loading()">
          @if (grid(); as g) {
            <nz-table
              #t
              class="plan-tbl"
              [nzData]="rows()"
              [nzShowPagination]="false"
              [nzScroll]="{ x: 'max-content' }"
              nzSize="small"
              nzBordered
            >
              <thead>
                <tr>
                  <th nzWidth="70px">Дата</th>
                  <th nzWidth="135px">Индекс</th>
                  <th nzWidth="150px">Дислокация</th>
                  <th nzWidth="60px">План</th>
                  <th nzWidth="60px">Факт</th>
                  <th nzWidth="64px">Откл</th>
                  @for (label of portLabels(); track label) {
                    <th class="port-col" nzWidth="60px" [title]="label"><span class="vert">{{ shortLabel(label) }}</span></th>
                  }
                  <th class="qty" nzWidth="44px"><span class="vert">Кол-во</span></th>
                  <th nzWidth="440px">Состав</th>
                  <th nzWidth="160px">Примечание</th>
                </tr>
              </thead>
              <tbody>
                @for (n of rows(); track n.ord; let i = $index) {
                  @if (showDivider(i)) {
                    <tr class="divider"><td [attr.colspan]="colCount()"></td></tr>
                  }
                  <tr [class.ostatok]="n.is_ostatok">
                    <td>{{ n.is_ostatok ? '' : dmDate(n.plan_msk) }}</td>
                    <td class="idx">{{ n.index_pp || '—' }}</td>
                    <td class="small">{{ n.station_oper }}</td>
                    <td class="c">{{ hm(n.plan_msk) }}</td>
                    <td class="c">{{ hm(n.fact_msk) }}</td>
                    <td class="c">{{ n.otkl }}</td>
                    @for (label of portLabels(); track label) {
                      <td class="c">{{ portCount(n, label) }}</td>
                    }
                    <td class="c bold">{{ n.activ || '' }}</td>
                    <td class="small sostav">{{ n.sostav }}</td>
                    <td class="small">{{ n.comment }}</td>
                  </tr>
                }
              </tbody>
            </nz-table>
          } @else if (!loading()) {
            <p class="muted">План для станции «{{ selectedLabel() }}» ещё не загружен.</p>
          }
        </nz-spin>
      </nz-card>

      <!-- Диалог проверки плана: сборные формирования + нитки без вагонов (один на план) -->
      <nz-modal
        [nzVisible]="sfPrepare() !== null"
        nzTitle="Проверка плана подвода: с.ф. и нитки без вагонов"
        [nzWidth]="820"
        [nzMaskClosable]="false"
        (nzOnCancel)="sfCancel()"
        [nzFooter]="sfFooter"
      >
        <div *nzModalContent>
          @if (sfPrepare(); as prep) {
            <!-- Сборные формирования: выбрать группы ИЛИ вписать реальный индекс -->
            @if (prep.sf?.length) {
              <div class="sec-title">Сборные формирования (с.ф.)</div>
            }
            @for (row of prep.sf ?? []; track row.ord) {
              <div class="sf-block">
                <div class="sf-head">
                  <span>{{ row.index_pp }} · {{ dmDate(row.plan_msk) }} {{ hm(row.plan_msk) }}</span>
                  @if (sfPlanPorts(row); as terms) {
                    <span class="sf-terms">{{ terms }}</span>
                  }
                  <span class="sf-cnt">выбрано {{ sfSelectedWagons(row) }} ваг</span>
                </div>

                <!-- 1. Сформированные сборные (реальный индекс по префиксу): уехали или ещё стоят -->
                @if (trainCands(row).length) {
                  <div class="sub-title">Поезд-кандидат — сформирован с реальным индексом</div>
                  @for (c of trainCands(row); track c.key) {
                    <label
                      nz-checkbox
                      class="sf-cand"
                      [nzChecked]="isChecked(row.ord, c.key)"
                      [nzDisabled]="isTaken(row.ord, c.key)"
                      (nzCheckedChange)="toggleCandidate(row.ord, c.key)"
                    >
                      <span class="sf-body">
                        <b class="idx">{{ c.index }}</b> ·
                        {{ c.departed ? 'уехал — сейчас ' + c.station : 'на станции формирования' }} ·
                        {{ c.date }} · <b>{{ c.quantity }}</b> ваг · {{ c.sostav }}
                      </span>
                    </label>
                  }
                }

                <!-- 2. Группы на станции формирования -->
                @if (standingCands(row).length) {
                  <div class="sub-title">Группы на станции формирования</div>
                  @for (c of standingCands(row); track c.key) {
                    <label
                      nz-checkbox
                      class="sf-cand"
                      [nzChecked]="isChecked(row.ord, c.key)"
                      [nzDisabled]="isTaken(row.ord, c.key)"
                      (nzCheckedChange)="toggleCandidate(row.ord, c.key)"
                    >
                      <span class="sf-body">{{ c.date }} · <b>{{ c.quantity }}</b> ваг · {{ c.sostav }}</span>
                    </label>
                  }
                }
                @if (row.candidates.length === 0) {
                  <div class="muted">Кандидатов нет: ни групп на станции формирования, ни уехавших поездов.</div>
                }

                <!-- 3. Ручной ввод индекса -->
                <div class="ovr">
                  <span class="ovr-lbl">Знаете индекс? Впишите вручную:</span>
                  <app-index-input (valueChange)="onOverride(row.ord, $event)" (completed)="revalidate()" />
                </div>
              </div>
            }

            <!-- Обычные нитки без вагонов: вероятна опечатка индекса -->
            @if (prep.problems?.length) {
              <div class="sec-title">Нитки без вагонов — проверьте индекс</div>
            }
            @for (row of prep.problems ?? []; track row.ord) {
              <div class="sf-block">
                <div class="sf-head">
                  <span class="idx">{{ row.index_pp || '—' }}</span>
                  <span>· {{ dmDate(row.plan_msk) }} {{ hm(row.plan_msk) }}</span>
                  <span class="sf-terms">план {{ row.activ }} ваг</span>
                  <span class="sf-cnt">вагоны не найдены</span>
                </div>
                <div class="ovr">
                  <span class="ovr-lbl">Исправьте индекс:</span>
                  <app-index-input [value]="row.index_pp" (valueChange)="onOverride(row.ord, $event)" (completed)="revalidate()" />
                </div>
              </div>
            }

            @if (!prep.sf?.length && !prep.problems?.length) {
              <div class="muted">Все нитки сопоставлены — можно применять.</div>
            }
          }
        </div>
      </nz-modal>
      <ng-template #sfFooter>
        <span class="foot-cnt">сопоставлено {{ sfPrepare()?.matched ?? 0 }} / {{ sfPrepare()?.nitki ?? 0 }}</span>
        <button nz-button (click)="revalidate()" [nzLoading]="revalBusy()">Пересчитать</button>
        <button nz-button (click)="sfCancel()" [nzLoading]="sfBusy()">Отмена (без правок)</button>
        <button nz-button nzType="primary" (click)="sfApply()" [nzLoading]="sfBusy()">Применить</button>
      </ng-template>
    </div>
  `,
  styles: [`
    .page { display: flex; flex-direction: column; gap: var(--space-md); width: 100%; }
    .card { border-radius: var(--radius-md); box-shadow: var(--shadow-sm); }
    .controls { display: flex; align-items: center; gap: var(--space-md); flex-wrap: wrap; }
    .lbl { color: var(--color-text-secondary); font-size: var(--font-size-sm); }
    .spacer { flex: 1 1 auto; }
    .summary { color: var(--color-text-secondary); font-size: var(--font-size-sm); }
    .msg { margin-top: var(--space-md); }
    .muted { color: var(--color-text-muted); }
    .c { text-align: center; }
    .bold { font-weight: 600; }
    .idx { font-weight: 500; }
    .small { font-size: var(--font-size-sm); }
    .sostav { white-space: pre-line; }
    /* Вертикальные заголовки терминалов (как в оригинале); ширину задаёт nzWidth, шрифт — как у «План/Факт». */
    .port-col, .qty { text-align: center; }
    .vert { writing-mode: vertical-lr; text-orientation: mixed; display: inline-block; white-space: nowrap; line-height: 1; }
    /* ── Компактная таблица как в оригинале gtport ──────────────────────────
       Плотные строки (высота), серая заливка шапки (#f5f5f5), голубой разделитель. */
    .plan-tbl ::ng-deep table { font-size: 0.96rem; }
    .plan-tbl ::ng-deep .ant-table-thead > tr > th {
      padding: 2px 6px; background: #f5f5f5; font-weight: 600; line-height: 1.2; text-align: center;
    }
    .plan-tbl ::ng-deep .ant-table-tbody > tr > td { padding: 1px 6px; line-height: 1.25; }
    /* Разделитель-блок между сутками (как в оригинале: голубой, тонкий). */
    tr.divider td { padding: 0; height: 2px; background: #e8f4fd; border-left: none; border-right: none; }
    tr.ostatok td { background: var(--color-bg-subtle); font-weight: 500; }
    /* Переключатель станций — крупнее (как в gtport): 1rem, высота вкладки ~36px. */
    .plan-tabs ::ng-deep .ant-tabs-tab { font-size: 1rem; font-weight: 500; padding: 6px 12px; }
    /* Диалог с.ф. */
    .sf-block { margin-bottom: var(--space-md); padding-bottom: var(--space-sm); border-bottom: 1px solid var(--color-border, #eee); }
    .sf-head { font-weight: 600; margin-bottom: var(--space-xs); display: flex; align-items: center; gap: var(--space-sm); flex-wrap: wrap; }
    .sf-cnt { color: var(--color-text-secondary); font-size: var(--font-size-sm); font-weight: 400; }
    /* Количество вагонов по нашим терминалам (НМТП-21, АТТИС-15) в заголовке с.ф. */
    .sf-terms { padding: 0 6px; border-radius: 3px; background: #f0f7ff; color: #1677ff; font-weight: 500; }
    /* Чекбокс и его (возможно многострочный) текст — в одну строку, чекбокс сверху слева. */
    .sf-cand { display: flex; align-items: flex-start; margin: 3px 0; }
    .sf-cand ::ng-deep .ant-checkbox { top: 0.15em; }
    .sf-body { display: inline-block; }
    /* Заголовок секции диалога (с.ф. / нитки без вагонов). */
    .sec-title { font-weight: 600; margin: var(--space-sm) 0 var(--space-xs); color: var(--color-text-secondary); }
    .sub-title { font-size: var(--font-size-sm); font-weight: 500; margin: var(--space-xs) 0 2px; color: var(--color-text-secondary); }
    /* Строка ручного ввода индекса (переопределение с.ф./исправление опечатки). */
    .ovr { display: flex; align-items: center; gap: var(--space-sm); margin: var(--space-xs) 0; flex-wrap: wrap; }
    .ovr-lbl { color: var(--color-text-secondary); font-size: var(--font-size-sm); }
    /* Счётчик «сопоставлено N / M» в футере — слева, поодаль от кнопок. */
    .foot-cnt { margin-right: auto; color: var(--color-text-secondary); font-size: var(--font-size-sm); }
  `],
})
export class PlanComponent implements OnInit, OnDestroy {
  private readonly api = inject(PlanApiService);

  @ViewChild(PlanStatusPanelComponent) private statusPanel?: PlanStatusPanelComponent;

  readonly stationOptions = STATION_OPTIONS;
  readonly selectedCode = signal(STATION_OPTIONS[0].code);
  readonly selectedLabel = signal(STATION_OPTIONS[0].label);
  readonly selectedPlanId = signal<number | null>(null);
  readonly selectedIndex = computed(() =>
    this.stationOptions.findIndex((o) => o.code === this.selectedCode()),
  );

  readonly plans = signal<PlanSummary[]>([]);
  readonly grid = signal<PlanGrid | null>(null);
  readonly loading = signal(false);
  readonly busyUpload = signal(false);
  readonly busyRecalc = signal(false);
  readonly showForeign = signal(false);
  readonly uploadMsg = signal<string | null>(null);
  readonly error = signal<string | null>(null);

  /**
   * Столбцы портов в порядке первого появления. По умолчанию — только «наши» причалы
   * (`is_our`, входящие в Activ); при включённом «Показать чужие» разворачиваются и
   * столбцы чужих терминалов. Признак `is_our` приходит с бэка (PortCell); планы,
   * загруженные до появления поля, столбцов не покажут — перезалить план.
   */
  readonly portLabels = computed<string[]>(() => {
    const g = this.grid();
    if (!g) return [];
    const showAll = this.showForeign();
    const seen = new Set<string>();
    const labels: string[] = [];
    for (const n of g.nitki) {
      for (const p of n.ports ?? []) {
        if ((p.is_our || showAll) && !seen.has(p.label)) {
          seen.add(p.label);
          labels.push(p.label);
        }
      }
    }
    return labels;
  });

  /** Метки столбцов «наших» причалов — для выбора правила сокращения имени. */
  private readonly ourLabels = computed<Set<string>>(() => {
    const g = this.grid();
    const s = new Set<string>();
    if (g) {
      for (const n of g.nitki) {
        for (const p of n.ports ?? []) {
          if (p.is_our) s.add(p.label);
        }
      }
    }
    return s;
  });

  /** Строки таблицы: строка «Остаток» всегда; «чужие» (activ=0) скрыты, если не включено. */
  readonly rows = computed<PlanNitka[]>(() => {
    const g = this.grid();
    if (!g) return [];
    if (this.showForeign()) return g.nitki;
    return g.nitki.filter((n) => n.is_ostatok || n.activ > 0);
  });

  ngOnInit(): void {
    this.reload(this.selectedCode());
  }

  ngOnDestroy(): void {
    this.stopSfHeartbeat();
  }

  onCodeChange(code: string): void {
    this.selectedCode.set(code);
    this.selectedLabel.set(this.stationOptions.find((o) => o.code === code)?.label ?? code);
    this.uploadMsg.set(null);
    this.reload(code);
  }

  /** Переключение станции вкладкой. */
  onTabChange(index: number): void {
    const code = this.stationOptions[index]?.code;
    if (code) this.onCodeChange(code);
  }

  /** Обновить список загрузок текущей станции. */
  refresh(): void {
    this.reload(this.selectedCode());
  }

  /** Печать таблицы штатным диалогом браузера. */
  printTable(): void {
    window.print();
  }

  onPlanChange(id: number | null): void {
    if (id == null) return;
    this.selectedPlanId.set(id);
    this.loadGridById(id);
  }

  planLabel(p: PlanSummary): string {
    const ts = p.loaded_at;
    const when = ts && ts.length >= 16 ? `${ts.slice(8, 10)}.${ts.slice(5, 7)} ${ts.slice(11, 16)}` : '—';
    return `${when} · ${p.source_file}`;
  }

  /** Возврат false — сами шлём файл через сервис, штатный XHR nz-upload не нужен. */
  readonly beforeUpload = (file: NzUploadFile): boolean => {
    this.doUpload(file.originFileObj ?? (file as unknown as File));
    return false;
  };

  dmDate(ts: string | null): string {
    if (!ts || ts.length < 10) return '';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)}`;
  }

  hm(ts: string | null): string {
    if (!ts || ts.length < 16) return '';
    return ts.slice(11, 16);
  }

  portCount(n: PlanNitka, label: string): string {
    const c = n.ports?.find((p) => p.label === label)?.count ?? 0;
    return c > 0 ? String(c) : '';
  }

  /** Число столбцов таблицы — для colspan строки-разделителя. */
  colCount(): number {
    return 9 + this.portLabels().length; // 6 базовых + порты + Кол-во/Состав/Примечание
  }

  /** Ключ «блока суток»: у «Остатка» — отдельный, у нитки — дата плана. */
  private dateKey(n: PlanNitka): string {
    return n.is_ostatok ? 'ostatok' : this.dmDate(n.plan_msk);
  }

  /** Разделитель-блок перед строкой i при смене суток (как в оригинале gtport). */
  showDivider(i: number): boolean {
    if (i <= 0) return false;
    const r = this.rows();
    return this.dateKey(r[i]) !== this.dateKey(r[i - 1]);
  }

  /**
   * Короткая метка столбца терминала для узкой шапки. Полное имя — в подсказке (title).
   * (1) убирает организационно-правовые формы и кавычки (generic, без хардкода портов);
   * (2) сокращает род груза в хвосте: уголь → без пометки, чугун→Ч, металлы→М, прочие→ПР;
   * (3) «наш» терминал — краткое имя из TERM_ABBR (НАХОДКИНСКИЙ МТП→НМТП; временно на
   *     фронте, до настроечной таблицы); «чужой» — каждое слово длиннее 4 символов
   *     сокращаем до 3 (напр. «ЛЕСНОЙ ТЕРМИНАЛ» → «ЛЕС ТЕР»).
   * Пример: «АО "НАХОДКИНСКИЙ МТП" Каменный уголь» → «НМТП».
   */
  private static readonly ORG_FORMS = new Set([
    'ОАО', 'ПАО', 'ЗАО', 'АО', 'ООО', 'КГУП', 'ФГУП', 'ГУП', 'МУП', 'ИП', 'АК', 'КОМПАНИЯ',
  ]);
  private static readonly CARGO_ABBR: { re: RegExp; ab: string }[] = [
    { re: /\s*(каменный\s+)?уголь\s*$/i, ab: '' }, // уголь — груз не указываем (только терминал)
    { re: /\s*чугун[а-яё]*\s*$/i, ab: 'Ч' },
    { re: /\s*слябы?\s*$/i, ab: 'СЛ' },
    { re: /\s*(чёрные|черные|цветные)?\s*металл[а-яё]*\s*$/i, ab: 'М' },
    { re: /\s*прочие(\s+грузы)?\s*$/i, ab: 'ПР' },
  ];
  /** Краткие имена терминалов (порт-специфика; временно тут, позже — в настроечной таблице). */
  private static readonly TERM_ABBR: Record<string, string> = {
    'НАХОДКИНСКИЙ МТП': 'НМТП',
    'АТТИС ЭНТЕРПРАЙС': 'АТТИС',
  };
  /** Разбирает метку столбца на «база» (терминал без орг-формы/кавычек) и аббревиатуру груза. */
  private splitTermCargo(label: string): { base: string; cargo: string } {
    let base = label
      .replace(/["«»„“”'']/g, ' ')
      .split(/\s+/)
      .filter((w) => w && !PlanComponent.ORG_FORMS.has(w.toUpperCase()))
      .join(' ')
      .trim();
    let cargo = '';
    for (const c of PlanComponent.CARGO_ABBR) {
      if (c.re.test(base)) {
        base = base.replace(c.re, '').trim();
        cargo = c.ab;
        break;
      }
    }
    return { base, cargo };
  }

  /** Имя «нашего» терминала без груза (для агрегации в заголовке с.ф.): база + TERM_ABBR. */
  private ourTerminalName(label: string): string {
    const { base } = this.splitTermCargo(label);
    return PlanComponent.TERM_ABBR[base.toUpperCase()] ?? base;
  }

  shortLabel(label: string): string {
    const { base, cargo } = this.splitTermCargo(label);
    const term = this.ourLabels().has(label)
      ? PlanComponent.TERM_ABBR[base.toUpperCase()] ?? base // «наш» — краткое имя
      : base.split(/\s+/).map((w) => (w.length > 4 ? w.slice(0, 3) : w)).join(' '); // «чужой» — слова >4 → 3
    return cargo ? `${term} ${cargo}` : term;
  }

  /**
   * Плановое количество вагонов по нашим терминалам для с.ф.-строки: «НМТП-13, АТТИС-10».
   * Источник — столбцы «наших» причалов из СТРОКИ ПЛАНА (сколько запланировано), а не
   * сумма кандидатов. Агрегируем по терминалу (без груза), крупные первыми.
   */
  sfPlanPorts(row: SFRow): string {
    const totals = new Map<string, number>();
    for (const p of row.ports ?? []) {
      if (!p.is_our || p.count <= 0) continue;
      const name = this.ourTerminalName(p.label);
      totals.set(name, (totals.get(name) ?? 0) + p.count);
    }
    return [...totals.entries()]
      .sort((a, b) => b[1] - a[1])
      .map(([t, n]) => `${t}-${n}`)
      .join(', ');
  }

  /** Поезда-кандидаты (секция 1 диалога с.ф.): сформированные с реальным индексом —
   *  уехавшие со станции формирования и ещё стоящие на ней. */
  trainCands(row: SFRow): SFCandidate[] {
    return row.candidates.filter((c) => c.departed || c.formed);
  }

  /** Несобранные группы на станции формирования (секция 2 диалога с.ф.). */
  standingCands(row: SFRow): SFCandidate[] {
    return row.candidates.filter((c) => !c.departed && !c.formed);
  }

  /** Сумма вагонов выбранных групп с.ф.-строки (для «выбрано N ваг» в заголовке). */
  sfSelectedWagons(row: SFRow): number {
    const sel = new Set(this.sfSel()[row.ord] ?? []);
    let n = 0;
    for (const c of row.candidates) {
      if (sel.has(c.key)) n += c.quantity;
    }
    return n;
  }

  // ── Загрузка плана с выбором групп с.ф. + ручные правки индексов (prepare/revalidate/confirm) ──
  readonly sfPrepare = signal<PreparePlanResult | null>(null);
  readonly sfSel = signal<Record<number, string[]>>({});       // ord с.ф.-нитки → выбранные id_disl
  readonly overrides = signal<Record<number, string>>({});     // ord нитки → вписанный индекс 4-3-4
  readonly sfBusy = signal(false);
  readonly revalBusy = signal(false);                          // идёт сухой пересчёт (revalidate)
  // Как заново получить превью с тем же источником (загрузка файла ИЛИ пересчёт по id) —
  // для прозрачного восстановления, если токен истёк (410).
  private resubmit: (() => Promise<PreparePlanResult>) | null = null;
  private sfBeat: ReturnType<typeof setInterval> | null = null; // heartbeat продления токена

  /** Пока открыт диалог с.ф., каждые 5 мин продлеваем токен (TTL на бэке 30 мин). */
  private startSfHeartbeat(): void {
    this.stopSfHeartbeat();
    this.sfBeat = setInterval(() => {
      const prep = this.sfPrepare();
      if (!prep) { this.stopSfHeartbeat(); return; }
      // Ошибку (410) не гасим здесь — обработается при «Применить»/«Отмена» восстановлением.
      this.api.touch(prep.token).catch(() => {});
    }, 5 * 60 * 1000);
  }

  private stopSfHeartbeat(): void {
    if (this.sfBeat) { clearInterval(this.sfBeat); this.sfBeat = null; }
  }

  /** Закрыть диалог с.ф. — всегда доступно, не запирает пользователя. */
  private closeSfDialog(): void {
    this.stopSfHeartbeat();
    this.sfPrepare.set(null);
    this.sfSel.set({});
    this.overrides.set({});
    this.resubmit = null;
  }

  /** Записать/снять ручную правку индекса нитки (ord → индекс; пусто — снятие). */
  onOverride(ord: number, value: string): void {
    const m = { ...this.overrides() };
    if (value) m[ord] = value;
    else delete m[ord];
    this.overrides.set(m);
  }

  /** Сухой пересчёт превью с текущими правками индексов (снимок не трогаем, токен не
   *  расходуем). При истёкшем токене — прозрачно пере-подготавливаем файл и повторяем. */
  async revalidate(): Promise<void> {
    const prep = this.sfPrepare();
    if (!prep) return;
    this.revalBusy.set(true);
    this.error.set(null);
    try {
      let res: PreparePlanResult;
      try {
        res = await this.api.revalidate(prep.token, this.overrides());
      } catch (err) {
        if (this.resubmit && err instanceof HttpErrorResponse && err.status === 410) {
          const fresh = await this.resubmit();
          res = await this.api.revalidate(fresh.token, this.overrides());
        } else {
          throw err;
        }
      }
      this.sfPrepare.set(res); // обновлённые sf/problems/matched (правки сохраняются в overrides)
    } catch (err) {
      this.error.set(apiErrorMessage(err));
    } finally {
      this.revalBusy.set(false);
    }
  }

  /** Группа отмечена в этой с.ф.-строке (по уникальному ключу группы). */
  isChecked(ord: number, key: string): boolean {
    return (this.sfSel()[ord] ?? []).includes(key);
  }

  /** Группа занята другой с.ф.-строкой (без двойного назначения). */
  isTaken(ord: number, key: string): boolean {
    const sel = this.sfSel();
    for (const k of Object.keys(sel)) {
      const o = Number(k);
      if (o !== ord && sel[o].includes(key)) return true;
    }
    return false;
  }

  toggleCandidate(ord: number, key: string): void {
    const sel = { ...this.sfSel() };
    const cur = new Set(sel[ord] ?? []);
    if (cur.has(key)) cur.delete(key);
    else cur.add(key);
    sel[ord] = [...cur];
    this.sfSel.set(sel);
  }

  /** «Применить» — confirm с правками индексов и выбранными группами. */
  sfApply(): void {
    const prep = this.sfPrepare();
    if (prep) void this.applyConfirm(prep.token, this.overrides(), this.sfSel());
  }

  /** «Отмена» / закрытие — применить план как есть, без правок и с.ф. (решение владельца). */
  sfCancel(): void {
    const prep = this.sfPrepare();
    if (prep) void this.applyConfirm(prep.token, {}, {});
  }

  /** Загрузка плана из файла (двухфазно). */
  private async doUpload(file: File): Promise<void> {
    await this.runPrepareFlow(() => this.api.prepare(this.selectedCode(), file), this.busyUpload);
  }

  /** Пересчитать выбранный план на текущей дислокации — без повторной загрузки Excel:
   *  нитки берутся из сохранённой сетки (id), матч гоняется заново. Дальше — тот же диалог. */
  async recalc(): Promise<void> {
    const id = this.selectedPlanId();
    if (id == null) {
      this.error.set('Нет выбранного плана для пересчёта.');
      return;
    }
    await this.runPrepareFlow(() => this.api.recalc(id), this.busyRecalc);
  }

  /** Общий поток загрузки/пересчёта: получить превью, открыть диалог (с.ф./проблемные)
   *  либо применить сразу. source — источник превью (файл или пересчёт по id); хранится
   *  в resubmit для прозрачного восстановления, если токен истечёт (410). */
  private async runPrepareFlow(
    source: () => Promise<PreparePlanResult>,
    busy: WritableSignal<boolean>,
  ): Promise<void> {
    busy.set(true);
    this.error.set(null);
    this.uploadMsg.set(null);
    this.resubmit = source;
    try {
      const prep = await source();
      const needsDialog = (prep.sf?.length ?? 0) > 0 || (prep.problems?.length ?? 0) > 0;
      if (!needsDialog) {
        await this.applyConfirm(prep.token, {}, {}); // ни с.ф., ни проблемных — применяем сразу
      } else {
        this.sfSel.set({});
        this.overrides.set({});
        this.sfPrepare.set(prep);  // открыть диалог: выбор групп с.ф. + правки индексов
        this.startSfHeartbeat();   // продлевать токен, пока окно открыто
      }
    } catch (err) {
      this.error.set(apiErrorMessage(err));
      this.resubmit = null;
    } finally {
      busy.set(false);
    }
  }

  private async applyConfirm(
    token: string,
    overrides: Record<number, string>,
    selections: Record<number, string[]>,
  ): Promise<void> {
    this.sfBusy.set(true);
    this.error.set(null);
    try {
      let res: PlanApplyResult;
      try {
        res = await this.api.confirm(token, overrides, selections);
      } catch (err) {
        // Токен истёк/потерян (диалог висел дольше TTL или бэкенд перезапускался):
        // прозрачно пере-подготавливаем тот же файл и повторяем один раз.
        if (this.resubmit && err instanceof HttpErrorResponse && err.status === 410) {
          const prep = await this.resubmit();
          res = await this.api.confirm(prep.token, overrides, selections);
        } else {
          throw err;
        }
      }
      this.uploadMsg.set(
        `${res.filename}: ниток ${res.nitki}, сопоставлено ${res.matched}, вагонов застолблено ${res.stamped}`,
      );
      this.closeSfDialog();
      await this.reload(this.selectedCode());
      void this.statusPanel?.load(); // загрузка меняет актуальность плана — обновим панель сразу
    } catch (err) {
      this.error.set(apiErrorMessage(err));
      this.closeSfDialog(); // не запираем пользователя: окно закрывается даже при ошибке
    } finally {
      this.sfBusy.set(false);
    }
  }

  /** Перечитывает список загрузок станции и открывает самую свежую. */
  private async reload(code: string): Promise<void> {
    this.loading.set(true);
    this.error.set(null);
    try {
      const plans = await this.api.listPlans(code);
      this.plans.set(plans);
      if (plans.length === 0) {
        this.selectedPlanId.set(null);
        this.grid.set(null);
        return;
      }
      const latest = plans[0];
      this.selectedPlanId.set(latest.id);
      this.grid.set(await this.api.getById(code, latest.id));
    } catch (err) {
      this.grid.set(null);
      if (!(err instanceof HttpErrorResponse && err.status === 404)) {
        this.error.set(apiErrorMessage(err));
      }
    } finally {
      this.loading.set(false);
    }
  }

  private async loadGridById(id: number): Promise<void> {
    this.loading.set(true);
    this.error.set(null);
    try {
      this.grid.set(await this.api.getById(this.selectedCode(), id));
    } catch (err) {
      this.error.set(apiErrorMessage(err));
    } finally {
      this.loading.set(false);
    }
  }
}
