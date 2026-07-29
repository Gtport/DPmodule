import { Component, ElementRef, OnInit, computed, inject, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { DragDropModule } from '@angular/cdk/drag-drop';
import { toBlob } from 'html-to-image';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzSelectModule } from 'ng-zorro-antd/select';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { yesterdayMsk } from '../../shared/msk-date';
import { ArrivalsApiService, TerminalTarget } from '../home/arrivals-api.service';
import { CargoWorkApiService, CargoWorkDay, CargoWorkLoad } from '../home/cargo-work-api.service';
import { MaxApiService } from './max-api.service';

/**
 * Перемещаемая модалка «Выгрузка за день» (перенос gtport CargoReport):
 * компактная СКРИН-ФОРМА суточного листа для отправки картинкой в MAX —
 * показатели строками, линии груза колонками (у ГУТ-2 — Уголь/Металл/Чугун
 * из справочника port_cargo_line, в gtport колонки были зашиты). Данные —
 * те же, что в «Грузовой работе» (`GET /cargo-work/{date}/{terminal}`),
 * только чтение: правится лист в «Грузовой работе» на главной.
 *
 * Блок «Погрузка» (Факт/План/Остаток) — только линии с ненулевыми цифрами,
 * как в gtport. PNG — html-to-image; «В MAX» — по маршруту формы `vygruzka`
 * (терминал → его чат, как gtport слал at→at/ut→ut/gut→gut).
 */
@Component({
  selector: 'app-vygruzka-day-modal',
  imports: [
    FormsModule, DragDropModule, NzButtonModule, NzIconModule, NzModalModule,
    NzSelectModule, NzSpinModule, NzTooltipModule,
  ],
  template: `
    <nz-modal [nzVisible]="true" [nzTitle]="ttl" [nzFooter]="null" nzWidth="680px"
              [nzMask]="false" (nzOnCancel)="closed.emit()">
      <ng-template #ttl>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          Выгрузка за день
        </div>
      </ng-template>

      <ng-container *nzModalContent>
        <div class="filters">
          <nz-select class="term" nzSize="small" [ngModel]="terminal()" (ngModelChange)="pickTerminal($event)">
            @for (t of terminals(); track t.name) { <nz-option [nzValue]="t.name" [nzLabel]="t.name"></nz-option> }
          </nz-select>
          <label class="fl">Дата <input type="date" class="date" [ngModel]="date()" (ngModelChange)="pickDate($event)" /></label>
          <button nz-button nzType="primary" nzSize="small" [nzLoading]="loading()" (click)="load()">
            <span nz-icon nzType="reload"></span> Обновить
          </button>
          <span class="spacer"></span>
          <button nz-button nzSize="small" [nzLoading]="sending()" (click)="sendToMax()" [disabled]="!day()"
                  nz-tooltip nzTooltipTitle="Отправить картинкой в чат терминала">
            <span nz-icon nzType="send"></span> В MAX
          </button>
          <button nz-button nzType="text" nzSize="small" (click)="exportPng()" [disabled]="!day()"
                  nz-tooltip nzTooltipTitle="Сохранить как картинку">
            <span nz-icon nzType="camera"></span>
          </button>
        </div>

        @if (loading()) {
          <div class="center"><nz-spin nzSimple></nz-spin></div>
        } @else if (day(); as d) {
          <div class="snap" id="vygruzka-day">
            <div class="phead" [style.background]="d.color">Выгрузка {{ terminal() }} {{ fmtDate(date()) }}</div>
            <table class="dp-tbl">
              @if (d.lines.length > 1) {
                <thead><tr><th class="c-lbl"></th>
                  @for (l of d.lines; track l.cargo_key) { <th class="c-v">{{ l.label }}</th> }
                </tr></thead>
              }
              <tbody>
                <tr><td class="lbl">Остаток на 18:00 факт/станция</td>
                  @for (l of d.lines; track l.cargo_key) { <td class="c num">{{ l.ost_18 }} / {{ l.ost_st }}</td> }
                </tr>
                <tr><td class="lbl">Прибыло</td>
                  @for (l of d.lines; track l.cargo_key) { <td class="c num">{{ l.prib }}</td> }
                </tr>
                <tr><td class="lbl">Обр полез/полн</td>
                  @for (l of d.lines; track l.cargo_key) { <td class="c num">{{ l.useful_formation }} / {{ l.total_formation }}</td> }
                </tr>
                <tr><td class="lbl">План выгрузки</td>
                  @for (l of d.lines; track l.cargo_key) { <td class="c num">{{ l.plan }}</td> }
                </tr>
                <tr><td class="lbl">Выгрузка</td>
                  @for (l of d.lines; track l.cargo_key) { <td class="c num b">{{ l.vigr_fact }} / {{ l.vigr_stan }}</td> }
                </tr>
                <tr><td class="lbl">Остаток</td>
                  @for (l of d.lines; track l.cargo_key) { <td class="c num">{{ l.ost }}</td> }
                </tr>
                <tr><td class="lbl">Эффективность</td>
                  @for (l of d.lines; track l.cargo_key) {
                    <td class="c num b" [class.eff-ok]="l.effectiv >= 100" [class.eff-bad]="l.effectiv > 0 && l.effectiv < 100">
                      {{ l.effectiv }}%
                    </td>
                  }
                </tr>
                <tr><td class="lbl">Перепоказ</td>
                  @for (l of d.lines; track l.cargo_key) { <td class="c num">{{ l.perepokaz }}</td> }
                </tr>
                <tr><td class="lbl">Простой порта</td>
                  @for (l of d.lines; track l.cargo_key) {
                    <td class="c num" [class.dt-bad]="l.downtime && l.downtime !== '0:00'">{{ l.downtime || '0:00' }}</td>
                  }
                </tr>
              </tbody>
            </table>

            @if (loadRows().length) {
              <table class="dp-tbl loadtbl">
                <thead><tr><th class="c-lbl">Погрузка</th><th class="c-v">Факт</th><th class="c-v">План</th><th class="c-v">Остаток</th></tr></thead>
                <tbody>
                  @for (l of loadRows(); track l.cargo_key) {
                    <tr>
                      <td class="lbl">{{ l.label }}</td>
                      <td class="c num">{{ l.load_fact }}</td>
                      <td class="c num">{{ l.plan }}</td>
                      <td class="c num">{{ l.ost }}</td>
                    </tr>
                  }
                </tbody>
              </table>
            }
          </div>
          <p class="hint">Только просмотр — лист правится в «Грузовой работе» на главной. «Выгрузка» — факт порта / по станции.</p>
        } @else {
          <div class="empty">Нет данных за выбранные сутки</div>
        }
      </ng-container>
    </nz-modal>
  `,
  styles: [`
    .ttl { cursor: move; user-select: none; }
    .filters { display: flex; align-items: center; gap: var(--space-sm); margin-bottom: var(--space-sm); flex-wrap: wrap; }
    .term { width: 130px; }
    .fl { font-size: var(--font-size-sm); color: var(--color-text-secondary); display: inline-flex; align-items: center; gap: 4px; }
    .date { padding: 3px 6px; border: 1px solid var(--color-border); border-radius: var(--radius-sm); }
    .spacer { flex: 1 1 auto; }
    .center { display: flex; justify-content: center; padding: var(--space-lg); }
    .snap { display: inline-block; background: var(--color-bg-surface); padding: var(--space-sm); }
    .phead { padding: 4px 10px; font-weight: 600; border: 1px solid var(--color-border); border-bottom: 0; }
    .c { text-align: center; white-space: nowrap; }
    .num { font-variant-numeric: tabular-nums; }
    .b { font-weight: 600; }
    .lbl { white-space: nowrap; }
    .c-lbl { width: 200px; } .c-v { width: 120px; text-align: center; }
    .eff-ok { color: var(--color-success-text, #237804); }
    .eff-bad { color: var(--color-danger-text); }
    .dt-bad { color: var(--color-danger-text); font-weight: 600; }
    .loadtbl { margin-top: var(--space-sm); }
    .empty { text-align: center; color: var(--color-text-secondary); padding: var(--space-md); }
    .hint { margin: var(--space-xs) 0 0; color: var(--color-text-muted); font-size: var(--font-size-sm); }
  `],
})
export class VygruzkaDayModalComponent implements OnInit {
  private readonly api = inject(CargoWorkApiService);
  private readonly arrivals = inject(ArrivalsApiService);
  private readonly max = inject(MaxApiService);
  private readonly msg = inject(NzMessageService);
  private readonly host = inject(ElementRef<HTMLElement>);

  readonly closed = output<void>();

  readonly loading = signal(false);
  readonly sending = signal(false);
  readonly terminals = signal<TerminalTarget[]>([]);
  readonly terminal = signal('');
  readonly date = signal(yesterdayMsk());
  readonly day = signal<CargoWorkDay | null>(null);

  /** Строки погрузки с хотя бы одной ненулевой цифрой (как в gtport). */
  readonly loadRows = computed<CargoWorkLoad[]>(() =>
    (this.day()?.load ?? []).filter((l) => l.load_fact || l.plan || l.ost));

  ngOnInit(): void {
    void this.init();
  }

  private async init(): Promise<void> {
    try {
      const ts = await this.arrivals.getTerminals();
      this.terminals.set(ts);
      if (ts.length && !this.terminal()) this.terminal.set(ts[0].name);
    } catch { /* реестр не критичен */ }
    await this.load();
  }

  pickTerminal(t: string): void {
    this.terminal.set(t);
    void this.load();
  }

  pickDate(d: string): void {
    this.date.set(d);
    void this.load();
  }

  async load(): Promise<void> {
    if (!this.terminal()) return;
    this.loading.set(true);
    try {
      this.day.set(await this.api.getDay(this.date(), this.terminal()));
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
      this.day.set(null);
    } finally {
      this.loading.set(false);
    }
  }

  /** «2026-07-28» → «28.07.26». */
  fmtDate(d: string): string {
    return `${d.slice(8, 10)}.${d.slice(5, 7)}.${d.slice(2, 4)}`;
  }

  private async png(): Promise<Blob> {
    const el = this.host.nativeElement.querySelector('#vygruzka-day') as HTMLElement | null;
    if (!el) throw new Error('форма не найдена');
    const blob = await toBlob(el, { pixelRatio: 2, backgroundColor: '#ffffff' });
    if (!blob) throw new Error('не удалось отрисовать картинку');
    return blob;
  }

  async exportPng(): Promise<void> {
    try {
      const blob = await this.png();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `Выгрузка_${this.terminal()}_${this.fmtDate(this.date())}.png`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    }
  }

  async sendToMax(): Promise<void> {
    this.sending.set(true);
    try {
      const blob = await this.png();
      const res = await this.max.sendImage('vygruzka', this.terminal(), blob,
        `Выгрузка_${this.terminal()}_${this.fmtDate(this.date())}.png`,
        `Выгрузка ${this.terminal()} ${this.fmtDate(this.date())}`);
      if (res.chats === 0) {
        this.msg.warning('Нет настроенного маршрута рассылки (форма «vygruzka»)');
      } else if (Object.keys(res.failed).length) {
        this.msg.warning(`Отправлено в ${res.sent.length}, не ушло — ${Object.keys(res.failed).join(', ')}`);
      } else {
        this.msg.success(`Отправлено в чаты (${res.sent.join(', ')})`);
      }
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.sending.set(false);
    }
  }
}
