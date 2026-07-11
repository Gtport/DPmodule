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
    NzSpinModule,
  ],
  template: `
    <div class="page">
      <nz-card class="card">
        <div class="controls">
          <span class="lbl">Станция:</span>
          <nz-select [ngModel]="selectedCode()" (ngModelChange)="onCodeChange($event)" style="width: 200px">
            @for (o of stationOptions; track o.code) {
              <nz-option [nzValue]="o.code" [nzLabel]="o.label" />
            }
          </nz-select>

          <span class="lbl">Загрузка:</span>
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
            <button nz-button [nzLoading]="busyUpload()">
              <span nz-icon nzType="upload"></span>
              Загрузить план
            </button>
          </nz-upload>

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
                    <th class="port-col" [title]="label">{{ shortLabel(label) }}</th>
                  }
                  <th nzWidth="60px">Кол-во</th>
                  <th nzWidth="280px">Состав</th>
                  <th nzWidth="160px">Примечание</th>
                </tr>
              </thead>
              <tbody>
                @for (n of rows(); track n.ord) {
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
    .port-col { text-align: center; font-size: var(--font-size-sm); width: 56px; min-width: 56px; white-space: normal; overflow-wrap: anywhere; line-height: 1.15; vertical-align: bottom; }
    tr.ostatok td { background: var(--color-bg-subtle); font-weight: 500; }
  `],
})
export class PlanComponent implements OnInit {
  private readonly api = inject(PlanApiService);

  readonly stationOptions = STATION_OPTIONS;
  readonly selectedCode = signal(STATION_OPTIONS[0].code);
  readonly selectedLabel = signal(STATION_OPTIONS[0].label);
  readonly selectedPlanId = signal<number | null>(null);

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

  /**
   * Короткая метка столбца терминала для узкой шапки. Полное имя — в подсказке (title).
   * (1) убирает организационно-правовые формы и кавычки (generic, без хардкода портов);
   * (2) сокращает род груза в хвосте: уголь → без пометки, чугун→Ч, металлы→М, прочие→ПР;
   * (3) аббревиатура терминала из TERM_ABBR (НАХОДКИНСКИЙ МТП→НМТП) — временно на фронте,
   *     до настроечной таблицы с «кратким именем» терминала.
   * Пример: «АО "НАХОДКИНСКИЙ МТП" Каменный уголь» → «НМТП».
   */
  private static readonly ORG_FORMS = new Set([
    'ОАО', 'ПАО', 'ЗАО', 'АО', 'ООО', 'КГУП', 'ФГУП', 'ГУП', 'МУП', 'ИП', 'АК', 'КОМПАНИЯ',
  ]);
  private static readonly CARGO_ABBR: { re: RegExp; ab: string }[] = [
    { re: /\s*(каменный\s+)?уголь\s*$/i, ab: '' }, // уголь — груз не указываем (только терминал)
    { re: /\s*чугун[а-яё]*\s*$/i, ab: 'Ч' },
    { re: /\s*(чёрные|черные|цветные)?\s*металл[а-яё]*\s*$/i, ab: 'М' },
    { re: /\s*прочие(\s+грузы)?\s*$/i, ab: 'ПР' },
  ];
  /** Краткие имена терминалов (порт-специфика; временно тут, позже — в настроечной таблице). */
  private static readonly TERM_ABBR: Record<string, string> = {
    'НАХОДКИНСКИЙ МТП': 'НМТП',
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
    const term = PlanComponent.TERM_ABBR[s.toUpperCase()] ?? s;
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
