import { Component, OnInit, computed, inject, signal } from '@angular/core';
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
import { apiErrorMessage } from '../../core/api/api-error';
import { PlanApiService, PlanGrid, PlanNitka, PlanSummary } from './plan-api.service';

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
    NzSpinModule, NzTabsModule, NzTooltipModule,
  ],
  template: `
    <div class="page">
      <nz-card class="card">
        <nz-tabs nzSize="small" [nzSelectedIndex]="selectedIndex()" (nzSelectedIndexChange)="onTabChange($event)">
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
                    <td class="c bold">{{ n.wagons || '' }}</td>
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
    /* Разделитель-блок между сутками. */
    tr.divider td { padding: 0; height: 3px; background: var(--color-primary-bg, #e8f4fd); border-left: none; border-right: none; }
    tr.ostatok td { background: var(--color-bg-subtle); font-weight: 500; }
  `],
})
export class PlanComponent implements OnInit {
  private readonly api = inject(PlanApiService);

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
  shortLabel(label: string): string {
    let s = label
      .replace(/["«»„“”'']/g, ' ')
      .split(/\s+/)
      .filter((w) => w && !PlanComponent.ORG_FORMS.has(w.toUpperCase()))
      .join(' ')
      .trim();
    let cargo = '';
    for (const c of PlanComponent.CARGO_ABBR) {
      if (c.re.test(s)) {
        s = s.replace(c.re, '').trim();
        cargo = c.ab;
        break;
      }
    }
    let term: string;
    if (this.ourLabels().has(label)) {
      // «наш» терминал — краткое имя из TERM_ABBR (или как есть).
      term = PlanComponent.TERM_ABBR[s.toUpperCase()] ?? s;
    } else {
      // «чужой» терминал — каждое слово длиннее 4 символов сокращаем до 3.
      term = s.split(/\s+/).map((w) => (w.length > 4 ? w.slice(0, 3) : w)).join(' ');
    }
    return cargo ? `${term} ${cargo}` : term;
  }

  private async doUpload(file: File): Promise<void> {
    this.busyUpload.set(true);
    this.error.set(null);
    this.uploadMsg.set(null);
    try {
      const res = await this.api.upload(this.selectedCode(), file);
      this.uploadMsg.set(
        `${res.filename}: ниток ${res.nitki}, сопоставлено ${res.matched}, вагонов застолблено ${res.stamped}`,
      );
      await this.reload(this.selectedCode());
    } catch (err) {
      this.error.set(apiErrorMessage(err));
    } finally {
      this.busyUpload.set(false);
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
