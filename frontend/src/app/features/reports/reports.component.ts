import { Component, OnInit, inject, signal } from '@angular/core';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { todayMsk } from '../../shared/msk-date';
import { ArrivalsApiService, TerminalTarget } from '../home/arrivals-api.service';
import { BrosModalComponent } from '../home/bros-modal.component';
import { LoadingModalComponent } from './loading-modal.component';
import { VygruzkaDayModalComponent } from './vygruzka-day-modal.component';
import { VygruzkaModalComponent } from './vygruzka-modal.component';
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
 * Карточки-блоки с кнопками, каждая открывает форму перемещаемой модалкой:
 * - «Оперативка»: «Утренняя СМС с ПП», «Оперативная СМС с ПП», «Брошенные
 *   поезда» (модалка из features/home);
 * - «Подход»: кнопка на каждый терминал из реестра (`GET /dislocation/terminals`);
 * - «Подход {пресет}» — карточка на каждый пресет из report_preset («Марис»):
 *   те же терминалы с предзаполненным фильтром клиентов. Нет пресетов — нет
 *   карточек;
 * - «Погрузка/Выгрузка»: «Погрузка в адрес портов» (loading-modal),
 *   «Выгрузка за период» (vygruzka-modal) и «Выгрузка за день»
 *   (vygruzka-day-modal — скрин-форма для отправки в MAX, перенос gtport
 *   CargoReport; правится лист в «Грузовой работе» на главной);
 * - «Скачать повагонку»: полная либо по терминалу (.xlsx собирает сервер).
 * Оставшийся блок оригинала (Отчёты НМТП) добавится по мере переноса —
 * пустых кнопок не заводим.
 */
@Component({
  selector: 'app-reports',
  imports: [
    NzButtonModule, SmsPlanModalComponent, SmsOperModalComponent,
    BrosModalComponent, PodhodModalComponent, LoadingModalComponent,
    VygruzkaModalComponent, VygruzkaDayModalComponent,
  ],
  template: `
    <div class="page">
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

          <div class="card">
            <div class="head">Погрузка/Выгрузка</div>
            <div class="body">
              <button nz-button nzBlock (click)="loadingOpen.set(true)">Погрузка в адрес портов</button>
              <button nz-button nzBlock (click)="vygruzkaOpen.set(true)">Выгрузка за период</button>
              <button nz-button nzBlock (click)="vygruzkaDayOpen.set(true)">Выгрузка за день</button>
            </div>
          </div>

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
        }
      </div>
    </div>

    @if (planOpen()) { <app-sms-plan-modal (closed)="planOpen.set(false)" /> }
    @if (operOpen()) { <app-sms-oper-modal (closed)="operOpen.set(false)" /> }
    @if (brosOpen()) { <app-bros-modal (closed)="brosOpen.set(false)" /> }
    @if (podhod(); as p) {
      <app-podhod-modal [terminal]="p.terminal" [clients]="p.clients" [presetName]="p.preset"
                        (closed)="podhod.set(null)" />
    }
    @if (loadingOpen()) { <app-loading-modal (closed)="loadingOpen.set(false)" /> }
    @if (vygruzkaOpen()) { <app-vygruzka-modal (closed)="vygruzkaOpen.set(false)" /> }
    @if (vygruzkaDayOpen()) { <app-vygruzka-day-modal (closed)="vygruzkaDayOpen.set(false)" /> }
  `,
  styles: [`
    .page { padding: var(--space-lg); display: flex; justify-content: center; align-items: flex-start; }
    .blocks { display: flex; flex-wrap: wrap; gap: var(--space-md); }
    .card { width: 260px; background: var(--color-bg-surface); border: 1px solid var(--color-border);
            border-top: 6px solid var(--color-primary); border-radius: var(--radius-card);
            box-shadow: var(--shadow-card); overflow: hidden; }
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
  private readonly msg = inject(NzMessageService);

  readonly vagonkaBusy = signal<string | null>(null);
  readonly loadingOpen = signal(false);
  readonly vygruzkaOpen = signal(false);
  readonly vygruzkaDayOpen = signal(false);
  readonly planOpen = signal(false);
  readonly operOpen = signal(false);
  readonly brosOpen = signal(false);
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
  }

  openPodhod(terminal: string, clients: string, preset: string): void {
    this.podhod.set({ terminal, clients, preset });
  }

  /** Скачивание повагонки (.xlsx собирает сервер; терминал пусто — весь снимок). */
  async downloadVagonka(terminal: string): Promise<void> {
    this.vagonkaBusy.set(terminal || 'all');
    try {
      const blob = await this.podhodApi.vagonkaExcel(terminal);
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = terminal
        ? `Повагонка ${terminal} ${todayMsk()}.xlsx`
        : `Полная повагонка ${todayMsk()}.xlsx`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.vagonkaBusy.set(null);
    }
  }
}
