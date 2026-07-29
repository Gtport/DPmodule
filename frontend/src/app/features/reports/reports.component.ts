import { Component, OnInit, inject, signal } from '@angular/core';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { todayMsk } from '../../shared/msk-date';
import { ArrivalsApiService, TerminalTarget } from '../home/arrivals-api.service';
import { BrosModalComponent } from '../home/bros-modal.component';
import { BrosReportModalComponent } from '../home/bros-report-modal.component';
import { LoadingModalComponent } from './loading-modal.component';
import { VygruzkaDayModalComponent } from './vygruzka-day-modal.component';
import { VygruzkaModalComponent } from './vygruzka-modal.component';
import { PerestanovkaApiService } from './perestanovka-api.service';
import { PerestanovkaFactModalComponent } from './perestanovka-fact-modal.component';
import { PodhodApiService, ReportPreset } from './podhod-api.service';
import { PodhodModalComponent } from './podhod-modal.component';
import { SmsOperModalComponent } from './sms-oper-modal.component';
import { SmsPlanModalComponent } from './sms-plan-modal.component';

/**
 * Экран «Справки и отчёты» — единый каталог отчётных форм (перенос gtport
 * Tools.tsx). В оригинале тот же набор был скопирован в три вкладки («Справки»,
 * «Справки и отчёты», «Инструменты оператора → Типовые отчеты») — здесь он
 * живёт один раз; страница «Справки» влита сюда (решение владельца 29.07.2026).
 *
 * Два ряда, как в оригинале (замечание владельца 29.07.2026): верхний —
 * текущие справки для отправки в MAX, нижний — отчёты за период и выгрузки
 * по портам. Карточки на наведении приподнимаются с тенью (hover gtport).
 *
 * Верхний ряд:
 * - «Оперативка»: «Утренняя СМС с ПП», «Оперативная СМС с ПП», «Брошенные
 *   поезда» (модалка из features/home);
 * - «Подход»: кнопка на каждый терминал из реестра (`GET /dislocation/terminals`);
 * - «Подход {пресет}» — карточка на каждый пресет из report_preset («Марис»);
 * - «Погрузка/Выгрузка»: «Погрузка в адрес портов» (сводка дня, в MAX) и
 *   «Выгрузка за день» (скрин-форма, в MAX).
 * Нижний ряд:
 * - «Скачать повагонку»: полная либо по терминалу (.xlsx собирает сервер);
 * - «Перестановки»: «Перестановка на {терминал}» (.xlsx текущих перестановок,
 *   собирает сервер; в gtport направления были зашиты — здесь реестр) и
 *   «Факт. перестановки» (модалка за период из истории);
 * - «Отчёты за период»: «Погрузка по дням» (та же модалка погрузки, вид «По
 *   дням»), «Выгрузка за период», «Отчёт по брошенным» (модалка отчёта
 *   напрямую, терминал и период выбираются внутри).
 * Оставшийся блок оригинала (Отчёты НМТП) добавится по мере переноса —
 * пустых кнопок не заводим.
 */
@Component({
  selector: 'app-reports',
  imports: [
    NzButtonModule, SmsPlanModalComponent, SmsOperModalComponent,
    BrosModalComponent, BrosReportModalComponent, PodhodModalComponent,
    LoadingModalComponent, VygruzkaModalComponent, VygruzkaDayModalComponent,
    PerestanovkaFactModalComponent,
  ],
  template: `
    <div class="page">
      <div class="rows">
        <div class="row-label">Текущие справки — отправка в MAX</div>
        <div class="blocks">
          <div class="card">
            <div class="head">Оперативка</div>
            <div class="body">
              <button nz-button nzBlock (click)="planOpen.set(true)">Утренняя СМС с ПП</button>
              <button nz-button nzBlock (click)="operOpen.set(true)">Оперативная СМС с ПП</button>
              <button nz-button nzBlock (click)="brosOpen.set(true)">Брошенные поезда</button>
            </div>
          </div>

          @if (terminals().length) {
            <div class="card">
              <div class="head">Подход</div>
              <div class="body">
                @for (t of terminals(); track t.name) {
                  <button nz-button nzBlock (click)="openPodhod(t.name, '', '')">
                    <span class="dot" [style.background]="t.color"></span>{{ t.name }}
                  </button>
                }
              </div>
            </div>

            @for (p of presets(); track p.id) {
              <div class="card">
                <div class="head">Подход {{ p.name }}</div>
                <div class="body">
                  @for (t of terminals(); track t.name) {
                    <button nz-button nzBlock (click)="openPodhod(t.name, p.clients, p.name)">
                      <span class="dot" [style.background]="t.color"></span>{{ t.name }}
                    </button>
                  }
                </div>
              </div>
            }

            @if (nmtpTerminals().length) {
              <div class="card">
                <div class="head">Отчёты НМТП</div>
                <div class="body">
                  @for (t of nmtpTerminals(); track t) {
                    <button nz-button nzBlock [nzLoading]="nmtpBusy() === t" (click)="downloadNmtp(t)">
                      <span class="dot" [style.background]="terminalColor(t)"></span>Подход {{ t }}
                    </button>
                  }
                </div>
              </div>
            }

            <div class="card">
              <div class="head">Погрузка/Выгрузка</div>
              <div class="body">
                <button nz-button nzBlock (click)="loadingView.set('summary')">Погрузка в адрес портов</button>
                <button nz-button nzBlock (click)="vygruzkaDayOpen.set(true)">Выгрузка за день</button>
              </div>
            </div>
          }
        </div>

        <div class="row-label">Отчёты — за период и по портам</div>
        <div class="blocks">
          <div class="card">
            <div class="head">Скачать повагонку</div>
            <div class="body">
              <button nz-button nzBlock [nzLoading]="vagonkaBusy() === 'all'" (click)="downloadVagonka('')">
                Полная повагонка
              </button>
              @for (t of terminals(); track t.name) {
                <button nz-button nzBlock [nzLoading]="vagonkaBusy() === t.name" (click)="downloadVagonka(t.name)">
                  <span class="dot" [style.background]="t.color"></span>Повагонка {{ t.name }}
                </button>
              }
            </div>
          </div>

          @if (terminals().length) {
            <div class="card">
              <div class="head">Перестановки</div>
              <div class="body">
                @for (t of terminals(); track t.name) {
                  <button nz-button nzBlock [nzLoading]="perestanovkaBusy() === t.name"
                          (click)="downloadPerestanovka(t.name)">
                    <span class="dot" [style.background]="t.color"></span>Перестановка на {{ t.name }}
                  </button>
                }
                <button nz-button nzBlock (click)="factOpen.set(true)">Факт. перестановки</button>
              </div>
            </div>

            <div class="card">
              <div class="head">Отчёты за период</div>
              <div class="body">
                <button nz-button nzBlock (click)="loadingView.set('daily')">Погрузка по дням</button>
                <button nz-button nzBlock (click)="vygruzkaOpen.set(true)">Выгрузка за период</button>
                <button nz-button nzBlock (click)="brosReportOpen.set(true)">Отчёт по брошенным</button>
              </div>
            </div>
          }
        </div>
      </div>
    </div>

    @if (planOpen()) { <app-sms-plan-modal (closed)="planOpen.set(false)" /> }
    @if (operOpen()) { <app-sms-oper-modal (closed)="operOpen.set(false)" /> }
    @if (brosOpen()) { <app-bros-modal (closed)="brosOpen.set(false)" /> }
    @if (brosReportOpen()) { <app-bros-report-modal (closed)="brosReportOpen.set(false)" /> }
    @if (podhod(); as p) {
      <app-podhod-modal [terminal]="p.terminal" [clients]="p.clients" [presetName]="p.preset"
                        (closed)="podhod.set(null)" />
    }
    @if (loadingView(); as v) { <app-loading-modal [initialView]="v" (closed)="loadingView.set(null)" /> }
    @if (vygruzkaOpen()) { <app-vygruzka-modal (closed)="vygruzkaOpen.set(false)" /> }
    @if (vygruzkaDayOpen()) { <app-vygruzka-day-modal (closed)="vygruzkaDayOpen.set(false)" /> }
    @if (factOpen()) { <app-perestanovka-fact-modal (closed)="factOpen.set(false)" /> }
  `,
  styles: [`
    .page { padding: var(--space-lg); display: flex; justify-content: center; align-items: flex-start; }
    .rows { display: flex; flex-direction: column; gap: var(--space-sm); max-width: 1200px; }
    .row-label { color: var(--color-text-muted); font-size: var(--font-size-sm); font-weight: 600;
                 text-transform: uppercase; letter-spacing: .04em; margin-top: var(--space-sm); }
    .blocks { display: flex; flex-wrap: wrap; gap: var(--space-md); align-items: flex-start; }
    .card { width: 260px; background: var(--color-bg-surface); border: 1px solid var(--color-border);
            border-top: 6px solid var(--color-primary); border-radius: var(--radius-card);
            box-shadow: var(--shadow-card); overflow: hidden;
            transition: box-shadow .2s ease, transform .2s ease; }
    .card:hover { box-shadow: var(--shadow-card-hover); transform: translateY(-4px); }
    .head { padding: var(--space-sm) var(--space-md); background: var(--color-bg-subtle);
            border-bottom: 1px solid var(--color-border-light); font-weight: 600; }
    .body { padding: var(--space-md); display: flex; flex-direction: column; gap: var(--space-sm); }
    .dot { display: inline-block; width: 10px; height: 10px; border-radius: 50%;
           border: 1px solid var(--color-border); margin-right: 8px; vertical-align: baseline; }
  `],
})
export class ReportsComponent implements OnInit {
  private readonly arrivals = inject(ArrivalsApiService);
  private readonly podhodApi = inject(PodhodApiService);
  private readonly perestanovkaApi = inject(PerestanovkaApiService);
  private readonly msg = inject(NzMessageService);

  readonly vagonkaBusy = signal<string | null>(null);
  readonly perestanovkaBusy = signal<string | null>(null);
  readonly nmtpBusy = signal<string | null>(null);
  readonly nmtpTerminals = signal<string[]>([]);
  readonly loadingView = signal<'summary' | 'daily' | null>(null);
  readonly vygruzkaOpen = signal(false);
  readonly vygruzkaDayOpen = signal(false);
  readonly planOpen = signal(false);
  readonly operOpen = signal(false);
  readonly brosOpen = signal(false);
  readonly brosReportOpen = signal(false);
  readonly factOpen = signal(false);
  readonly podhod = signal<{ terminal: string; clients: string; preset: string } | null>(null);

  readonly terminals = signal<TerminalTarget[]>([]);
  readonly presets = signal<ReportPreset[]>([]);

  ngOnInit(): void {
    void this.loadRegistry();
  }

  private async loadRegistry(): Promise<void> {
    // Реестры не критичны: без них страница живёт с одной «Оперативкой».
    try {
      this.terminals.set(await this.arrivals.getTerminals());
    } catch { /* тост не нужен — карточка просто не покажется */ }
    try {
      this.presets.set(await this.podhodApi.presets());
    } catch { /* без пресетов — без карточек пресетов */ }
    try {
      this.nmtpTerminals.set(await this.podhodApi.nmtpTerminals());
    } catch { /* без раскладки НМТП — без карточки */ }
  }

  /** Цвет маркера терминала из реестра (для кнопок НМТП). */
  terminalColor(name: string): string {
    return this.terminals().find((t) => t.name === name)?.color || '#eee';
  }

  /** «Подход вагонов» по форме порта (НМТП) — книга .xlsx с сервера. */
  async downloadNmtp(terminal: string): Promise<void> {
    this.nmtpBusy.set(terminal);
    try {
      const blob = await this.podhodApi.nmtpExcel(terminal);
      this.saveBlob(blob, `Подход вагонов ${terminal} ${todayMsk()}.xlsx`);
    } catch (err) {
      this.msg.error(await blobErrorMessage(err));
    } finally {
      this.nmtpBusy.set(null);
    }
  }

  openPodhod(terminal: string, clients: string, preset: string): void {
    this.podhod.set({ terminal, clients, preset });
  }

  /** Скачивание повагонки (.xlsx собирает сервер; терминал пусто — весь снимок). */
  async downloadVagonka(terminal: string): Promise<void> {
    this.vagonkaBusy.set(terminal || 'all');
    try {
      const blob = await this.podhodApi.vagonkaExcel(terminal);
      this.saveBlob(blob, terminal
        ? `Повагонка ${terminal} ${todayMsk()}.xlsx`
        : `Полная повагонка ${todayMsk()}.xlsx`);
    } catch (err) {
      this.msg.error(await blobErrorMessage(err));
    } finally {
      this.vagonkaBusy.set(null);
    }
  }

  /** «Перестановка на {терминал}» — текущие перестановки книгой .xlsx с сервера. */
  async downloadPerestanovka(terminal: string): Promise<void> {
    this.perestanovkaBusy.set(terminal);
    try {
      const blob = await this.perestanovkaApi.excel(terminal);
      this.saveBlob(blob, `Перестановка на ${terminal} ${todayMsk()}.xlsx`);
    } catch (err) {
      this.msg.error(await blobErrorMessage(err));
    } finally {
      this.perestanovkaBusy.set(null);
    }
  }

  private saveBlob(blob: Blob, filename: string): void {
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    a.click();
    URL.revokeObjectURL(url);
  }
}

/**
 * Текст ошибки скачивания: при responseType 'blob' HttpClient заворачивает и
 * JSON-тело ошибки в Blob — разбираем его, чтобы показать сообщение сервера
 * («нет вагонов на перестановку…»), а не безликое «Не удалось выполнить запрос».
 */
async function blobErrorMessage(err: unknown): Promise<string> {
  const body = (err as { error?: unknown } | undefined)?.error;
  if (body instanceof Blob) {
    try {
      return apiErrorMessage({ error: JSON.parse(await body.text()) });
    } catch { /* не JSON — падаем в общий текст */ }
  }
  return apiErrorMessage(err);
}
