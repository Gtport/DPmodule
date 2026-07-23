import { Component, ElementRef, OnInit, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzCardModule } from 'ng-zorro-antd/card';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzMessageService } from 'ng-zorro-antd/message';
import { toBlob } from 'html-to-image';
import { apiErrorMessage } from '../../core/api/api-error';
import { ArrivalsApiService, TerminalTarget } from '../home/arrivals-api.service';
import { CargoWorkApiService, CargoWorkDay, CargoWorkLine } from '../home/cargo-work-api.service';
import { NearestApiService, NearestTrain } from '../home/nearest-api.service';
import { BroadcastApiService, BroadcastResult } from './broadcast-api.service';

/** Текущая дата МСК (yyyy-MM-dd) — не зависит от пояса браузера. */
function todayMsk(): string {
  return new Date().toLocaleString('sv-SE', { timeZone: 'Europe/Moscow' }).slice(0, 10);
}

/** Строка сводки: подпись + как достать значение из линии учёта выгрузки. */
interface SummaryRow {
  label: string;
  get: (l: CargoWorkLine) => string;
}

/** Собранная форма одного терминала: реестр, сводка выгрузки, поезда подхода. */
interface TerminalForm {
  target: TerminalTarget;
  day: CargoWorkDay | null;
  trains: NearestTrain[];
}

/**
 * Экран «Рассылка» (перенос gtport SmsPlan/SmsOper): сводная карточка терминала
 * «ЖД сутки» под картинку — блок грузовой работы (остаток/прибыло/выгрузка/
 * образование/простой из учётного листа) + список поездов подхода. Картинку
 * рисует фронт (html-to-image), текст оперативки собирается тут же; рассылку в
 * чаты MAX по маршруту (форма × терминал) делает бэк (тонкий релей).
 *
 * Данные не дублируются новым бэкендом: сводка собирается из уже готовых ручек
 * /cargo-work (лист суток) и /dislocation/nearest (подход).
 */
@Component({
  selector: 'app-broadcast',
  imports: [FormsModule, NzButtonModule, NzCardModule, NzIconModule, NzSpinModule, NzTooltipModule],
  template: `
    <div class="page">
      <div class="toolbar">
        <b class="ttl">Рассылка форм</b>
        <input type="date" [ngModel]="date()" (ngModelChange)="date.set($event); reload()" class="date" />
        <span class="spacer"></span>
        <button nz-button nzType="default" nzSize="small" (click)="reload()"
                nz-tooltip nzTooltipTitle="Обновить данные">
          <span nz-icon nzType="sync"></span>
        </button>
        <button nz-button nzType="primary" nzSize="small" [nzLoading]="sending()"
                (click)="sendComposite()"
                nz-tooltip nzTooltipTitle="Сводную картинку всех терминалов — в общий чат">
          <span nz-icon nzType="send"></span> Сводка в MAX
        </button>
      </div>

      @if (loading()) {
        <div class="center"><nz-spin nzSimple></nz-spin></div>
      } @else {
        <div class="grid" #grid>
          @for (f of forms(); track f.target.name) {
            <div class="term" [id]="'term-' + f.target.name">
              <div class="term-head" [style.background]="f.target.color || '#eee'">
                {{ f.target.name }} ЖД СУТКИ · {{ fmtDate(date()) }}
              </div>

              <table class="sum">
                <thead>
                  <tr>
                    <th class="ind"></th>
                    @for (l of lines(f); track l.cargo_key) { <th>{{ l.label }}</th> }
                  </tr>
                </thead>
                <tbody>
                  @for (row of SUMMARY_ROWS; track row.label) {
                    <tr>
                      <td class="ind">{{ row.label }}</td>
                      @for (l of lines(f); track l.cargo_key) { <td>{{ row.get(l) }}</td> }
                    </tr>
                  }
                </tbody>
              </table>

              <div class="trains-ttl">Поезда в подходе</div>
              <div class="trains">
                @for (t of f.trains; track t.key) {
                  <div class="tr" [class.danger]="t.broshen">
                    <span class="num">{{ t.index || '—' }}</span>
                    <span class="dt">{{ fmtDT(t.time_jd) }}</span>
                    <span class="sost">{{ sostav(t) }}</span>
                  </div>
                } @empty {
                  <div class="empty">Нет поездов в подходе</div>
                }
              </div>

              <div class="term-actions">
                <button nz-button nzType="default" nzSize="small" (click)="exportPng(f.target.name)"
                        nz-tooltip nzTooltipTitle="Сохранить картинку">
                  <span nz-icon nzType="download"></span>
                </button>
                <button nz-button nzType="default" nzSize="small" [nzLoading]="busy() === f.target.name + ':img'"
                        (click)="sendImage(f.target.name)">
                  <span nz-icon nzType="picture"></span> Картинкой
                </button>
                <button nz-button nzType="default" nzSize="small" [nzLoading]="busy() === f.target.name + ':txt'"
                        (click)="sendText(f)">
                  <span nz-icon nzType="message"></span> Текстом
                </button>
              </div>
            </div>
          } @empty {
            <div class="empty">Нет терминалов</div>
          }
        </div>
      }
    </div>
  `,
  styles: [`
    .page { padding: var(--space-md); }
    .toolbar { display: flex; align-items: center; gap: var(--space-sm); margin-bottom: var(--space-md); }
    .ttl { font-size: var(--font-size-lg); }
    .date { padding: 3px 8px; border: 1px solid var(--color-border); border-radius: var(--radius-sm); }
    .spacer { flex: 1 1 auto; }
    .center { display: flex; justify-content: center; padding: var(--space-xl); }
    .grid { display: flex; flex-wrap: wrap; gap: var(--space-md); align-items: flex-start; }

    .term { width: 360px; background: #fff; border: 1px solid var(--color-border);
            border-radius: var(--radius-card); overflow: hidden; box-shadow: var(--shadow-card); }
    .term-head { padding: var(--space-sm); text-align: center; font-weight: 700; }

    .sum { width: 100%; border-collapse: collapse; font-size: var(--font-size-sm); }
    .sum th, .sum td { border: 1px solid var(--color-border-light); padding: 3px 8px; text-align: center; }
    .sum th { background: var(--color-bg-subtle); font-weight: 600; }
    .sum .ind { text-align: left; white-space: nowrap; font-weight: 500; }

    .trains-ttl { padding: 4px 8px; background: var(--color-bg-subtle); font-weight: 600;
                  font-size: var(--font-size-sm); border-top: 1px solid var(--color-border-light); }
    .trains { max-height: 40vh; overflow: auto; }
    .tr { display: flex; gap: var(--space-sm); padding: 2px 8px; font-size: var(--font-size-sm);
          border-bottom: 1px solid var(--color-border-light); }
    .tr .num { font-variant-numeric: tabular-nums; font-weight: 600; width: 64px; }
    .tr .dt { color: var(--color-success); width: 88px; white-space: nowrap; }
    .tr .sost { flex: 1 1 auto; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .tr.danger .num { color: var(--color-danger); }
    .empty { padding: var(--space-sm); text-align: center; color: var(--color-text-secondary);
             font-size: var(--font-size-sm); }

    .term-actions { display: flex; gap: var(--space-xs); padding: var(--space-sm);
                    border-top: 1px solid var(--color-border-light); }
  `],
})
export class BroadcastComponent implements OnInit {
  private readonly arrivalsApi = inject(ArrivalsApiService);
  private readonly cargoApi = inject(CargoWorkApiService);
  private readonly nearestApi = inject(NearestApiService);
  private readonly maxApi = inject(BroadcastApiService);
  private readonly msg = inject(NzMessageService);
  private readonly host = inject(ElementRef<HTMLElement>);

  readonly date = signal(todayMsk());
  readonly loading = signal(false);
  readonly sending = signal(false);
  readonly busy = signal(''); // '<terminal>:img' | '<terminal>:txt' — какая кнопка крутится
  readonly forms = signal<TerminalForm[]>([]);

  // Показатели сводки: авто-слой учётного листа (образование — «полезное/полное»).
  readonly SUMMARY_ROWS: SummaryRow[] = [
    { label: 'Остаток на 18:00', get: (l) => String(l.ost_18) },
    { label: 'Прибыло', get: (l) => String(l.prib) },
    { label: 'Выгрузка', get: (l) => String(l.vigr_fact) },
    { label: 'Образование (полезн./полн.)', get: (l) => `${l.useful_formation}/${l.total_formation}` },
    { label: 'Простой (прогноз)', get: (l) => l.downtime || '0:00' },
    { label: 'Остаток', get: (l) => String(l.ost) },
  ];

  ngOnInit(): void {
    void this.reload();
  }

  async reload(): Promise<void> {
    this.loading.set(true);
    try {
      const targets = await this.arrivalsApi.getTerminals();
      const forms = await Promise.all(
        targets.map(async (target): Promise<TerminalForm> => {
          const [day, trains] = await Promise.all([
            this.cargoApi.getDay(this.date(), target.name).catch(() => null),
            this.nearestApi.getNearest([target.name]).catch(() => [] as NearestTrain[]),
          ]);
          return { target, day, trains };
        }),
      );
      this.forms.set(forms);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.loading.set(false);
    }
  }

  /** Линии выгрузки терминала (без разбивки — одна строка «Всего»). */
  lines(f: TerminalForm): CargoWorkLine[] {
    return f.day?.lines?.length ? f.day.lines : [];
  }

  sostav(t: NearestTrain): string {
    return t.sub_groups.map((sg) => sg.display).join(' · ') || '—';
  }

  fmtDate(d: string): string {
    return d.length === 10 ? `${d.slice(8, 10)}.${d.slice(5, 7)}.${d.slice(0, 4)}` : d;
  }

  /** дд.мм чч:мм из LocalTime (без года — компактно). */
  fmtDT(ts: string | null): string {
    if (!ts || ts.length < 16) return '—';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)} ${ts.slice(11, 16)}`;
  }

  // ── Экспорт / рассылка ──────────────────────────────────────────────────

  private cardEl(terminal: string): HTMLElement | null {
    return this.host.nativeElement.querySelector(`#term-${CSS.escape(terminal)}`);
  }

  private gridEl(): HTMLElement | null {
    return this.host.nativeElement.querySelector('.grid');
  }

  private async png(el: HTMLElement): Promise<Blob> {
    // Списки поездов скроллятся (max-height) — на снимке обрезались бы. Временно
    // раскрываем скролл-области внутри снимаемого узла, потом возвращаем как было.
    const scrollers = Array.from(el.querySelectorAll<HTMLElement>('.trains'));
    const saved = scrollers.map((s) => ({ s, mh: s.style.maxHeight, ov: s.style.overflow }));
    for (const { s } of saved) {
      s.style.maxHeight = 'none';
      s.style.overflow = 'visible';
    }
    try {
      const blob = await toBlob(el, { pixelRatio: 2, backgroundColor: '#ffffff' });
      if (!blob) throw new Error('не удалось отрисовать картинку');
      return blob;
    } finally {
      for (const { s, mh, ov } of saved) {
        s.style.maxHeight = mh;
        s.style.overflow = ov;
      }
    }
  }

  async exportPng(terminal: string): Promise<void> {
    const el = this.cardEl(terminal);
    if (!el) return;
    try {
      const blob = await this.png(el);
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `План_${terminal}_${this.date()}.png`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    }
  }

  /** Картинка одного терминала → чаты справок (report=spravki). */
  async sendImage(terminal: string): Promise<void> {
    const el = this.cardEl(terminal);
    if (!el) return;
    this.busy.set(`${terminal}:img`);
    try {
      const blob = await this.png(el);
      const res = await this.maxApi.sendImage('spravki', terminal, blob, `План_${terminal}.png`,
        `План подвода ${terminal} ${this.fmtDate(this.date())}`);
      this.report(res, terminal);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.busy.set('');
    }
  }

  /** Сводная картинка всех терминалов → общий чат (report=plan, без терминала). */
  async sendComposite(): Promise<void> {
    const el = this.gridEl();
    if (!el) return;
    this.sending.set(true);
    try {
      const blob = await this.png(el);
      const res = await this.maxApi.sendImage('plan', '', blob, `План_подвода_${this.date()}.png`,
        `План подвода ${this.fmtDate(this.date())}`);
      this.report(res, 'сводка');
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.sending.set(false);
    }
  }

  /** Оперативка терминала текстом → оперативные чаты (report=oper). */
  async sendText(f: TerminalForm): Promise<void> {
    const text = this.buildText(f);
    if (!text) {
      this.msg.warning(`${f.target.name}: нет поездов для оперативки`);
      return;
    }
    this.busy.set(`${f.target.name}:txt`);
    try {
      const res = await this.maxApi.sendText('oper', f.target.name, text);
      this.report(res, f.target.name);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.busy.set('');
    }
  }

  /** Текст оперативки: заголовок терминала + строки поездов подхода. */
  private buildText(f: TerminalForm): string {
    if (!f.trains.length) return '';
    const head = `${f.target.name} ЖД сутки ${this.fmtDate(this.date())}`;
    const rows = f.trains.map((t) => `${t.index || '—'} ${this.fmtDT(t.time_jd)} ${this.sostav(t)}`);
    return [head, ...rows].join('\n');
  }

  /** Тост по исходу рассылки: сколько чатов, что не ушло. */
  private report(res: BroadcastResult, what: string): void {
    if (res.chats === 0) {
      this.msg.warning(`${what}: нет настроенного маршрута рассылки`);
      return;
    }
    const failed = Object.keys(res.failed);
    if (failed.length) {
      this.msg.warning(`${what}: отправлено в ${res.sent.length}, не ушло — ${failed.join(', ')}`);
    } else {
      this.msg.success(`${what}: отправлено в чаты (${res.sent.join(', ')})`);
    }
  }
}
