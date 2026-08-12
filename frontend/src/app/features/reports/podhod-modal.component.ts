import { Component, OnInit, computed, inject, input, output, signal } from '@angular/core';
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
import { todayMsk } from '../../shared/msk-date';
import { readableTextColor } from '../../shared/readable-color';
import { ArrivalsApiService, TerminalTarget } from '../home/arrivals-api.service';
import { MaxApiService } from './max-api.service';
import { PodhodApiService, PodhodItem, PodhodReport, PodhodSubgroup } from './podhod-api.service';

/**
 * Перемещаемая модалка «Подход — терминал» (перенос gtport PortReportTable):
 * поезда, идущие на терминал, двухуровневой таблицей — группа-поезд (№, дорога,
 * станция операции, индекс, операция, план подвода — через rowspan) и её
 * подгруппы (станция погрузки, дата, отправитель, кол-во/вес, примечание).
 * Сортировка как в gtport — по № по УБЫВАНИЮ: ближайший прогноз внизу.
 *
 * Мультиселект «Клиенты» — фильтр отчёта (опции — все клиенты терминала из
 * ответа); карточка-пресет («Подход Марис») открывает модалку с предзаполненным
 * фильтром. Excel собирается на клиенте (xlsx-js-style, раскладка повторяет
 * серверный файл gtport), PNG — html-to-image, «В MAX» — картинкой по маршруту
 * формы `podhod` (пресет шлётся сводным маршрутом, terminal=''). Автообновления
 * раз в 5 минут из gtport нет (решение владельца): это модалка, не дежурный
 * экран — есть кнопка «Обновить».
 */
@Component({
  selector: 'app-podhod-modal',
  imports: [
    FormsModule, DragDropModule, NzButtonModule, NzIconModule, NzModalModule,
    NzSelectModule, NzSpinModule, NzTooltipModule,
  ],
  template: `
    <nz-modal [nzVisible]="true" [nzTitle]="ttl" [nzFooter]="null" nzWidth="1440px"
              [nzMask]="false" (nzOnCancel)="closed.emit()">
      <ng-template #ttl>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          <span class="tbadge" [style.background]="terminalColor()">{{ terminal() }}</span>
          Подход поездов @if (presetName()) { <span class="preset">{{ presetName() }}</span> }
          <span class="sub">поездов: {{ report()?.total ?? 0 }}</span>
        </div>
      </ng-template>

      <ng-container *nzModalContent>
        <div class="filters">
          <nz-select class="clients" nzSize="small" nzMode="multiple" nzPlaceHolder="Клиенты (все)"
                     [nzMaxTagCount]="2" [ngModel]="selClients()" (ngModelChange)="selClients.set($event)">
            @for (c of clientOptions(); track c) { <nz-option [nzValue]="c" [nzLabel]="c"></nz-option> }
          </nz-select>
          <button nz-button nzType="primary" nzSize="small" [nzLoading]="loading()" (click)="load()">
            <span nz-icon nzType="reload"></span> Обновить
          </button>
          <span class="spacer"></span>
          <button nz-button nzSize="small" [nzLoading]="sending()" (click)="sendToMax()" [disabled]="!rows().length"
                  nz-tooltip nzTooltipTitle="Отправить картинкой в чаты MAX">
            <span nz-icon nzType="send"></span> В MAX
          </button>
          <button nz-button nzType="text" nzSize="small" (click)="exportPng()" [disabled]="!rows().length"
                  nz-tooltip nzTooltipTitle="Сохранить как картинку">
            <span nz-icon nzType="camera"></span>
          </button>
          <button nz-button nzType="text" nzSize="small" (click)="exportExcel()" [disabled]="!rows().length"
                  nz-tooltip nzTooltipTitle="Экспорт в Excel">
            <span nz-icon nzType="download"></span>
          </button>
        </div>

        @if (loading()) {
          <div class="center"><nz-spin nzSimple></nz-spin></div>
        } @else {
          <div class="dp-tbl-wrap" style="max-height: 68vh" id="podhod-tbl">
            <table class="dp-tbl">
              <thead><tr>
                <th class="c-n">№</th><th class="c-dor">Дорога</th><th>Станция опер</th>
                <th class="c-idx">Индекс поезда</th><th class="c-op">Операция</th>
                <th>Станция погрузки</th><th class="c-dn">Погрузка</th><th>Отправитель</th>
                <th class="c-cnt">Кол-во</th><th class="c-ves">Вес</th>
                <th class="c-prim">Примечание</th><th class="c-plan">План подвода</th>
              </tr></thead>
              <tbody>
                @for (item of rows(); track item.n) {
                  @for (sg of item.subgroups; track $index; let first = $first) {
                    <tr>
                      @if (first) {
                        <td class="c num" [attr.rowspan]="item.subgroups.length">{{ item.n }}</td>
                        <td class="c" [attr.rowspan]="item.subgroups.length">{{ item.doroga_oper }}</td>
                        <td class="c grp" [attr.rowspan]="item.subgroups.length"
                            [style.color]="groupColor(item)" [class.b]="groupColor(item)">{{ item.station_oper }}</td>
                        <td class="c num grp" [attr.rowspan]="item.subgroups.length"
                            [style.color]="groupColor(item)" [class.b]="groupColor(item)">{{ item.index }}</td>
                        <td class="c" [attr.rowspan]="item.subgroups.length"
                            [class.bros]="item.oper_s === 'Брошен'">{{ opText(item.oper_s) }}</td>
                      }
                      <td class="ell" [title]="sg.station_nach">{{ sg.station_nach }}</td>
                      <td class="c">{{ fmtDDMMM(sg.date_nach) }}</td>
                      <td class="ell" [title]="sg.gruzotpr">{{ sg.gruzotpr }}</td>
                      <td class="c num">{{ sg.vagon_count }}</td>
                      <td class="c num">{{ fmtVes(sg.total_weight) }}</td>
                      <td class="c" [style.color]="primColor(sg.prim_3)">{{ sg.prim_3 }}</td>
                      @if (first) {
                        <td class="c num plan" [attr.rowspan]="item.subgroups.length">{{ fmtPlan(item.plan_msk) }}</td>
                      }
                    </tr>
                  }
                } @empty {
                  <tr><td colspan="12" class="empty">Поездов в подходе нет</td></tr>
                }
              </tbody>
            </table>
          </div>
        }
      </ng-container>
    </nz-modal>
  `,
  styles: [`
    .ttl { cursor: move; user-select: none; display: flex; align-items: center; gap: var(--space-sm); }
    .ttl .sub { color: var(--color-text-muted); font-weight: 400; font-size: var(--font-size-sm); }
    .tbadge { padding: 0 8px; border-radius: var(--radius-sm); border: 1px solid var(--color-border); font-size: var(--font-size-sm); }
    .preset { color: var(--color-primary-active); }
    .filters { display: flex; align-items: center; gap: var(--space-sm); margin-bottom: var(--space-sm); flex-wrap: wrap; }
    .clients { min-width: 320px; max-width: 560px; }
    .spacer { flex: 1 1 auto; }
    .center { display: flex; justify-content: center; padding: var(--space-lg); }
    .c { text-align: center; white-space: nowrap; }
    .num { font-variant-numeric: tabular-nums; }
    .b { font-weight: 600; }
    .grp { vertical-align: middle; }
    .bros { color: var(--color-danger-text); font-weight: 600; }
    .plan { font-weight: 600; vertical-align: middle; }
    .ell { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 220px; }
    .c-n { width: 34px; } .c-dor { width: 56px; } .c-idx { width: 120px; } .c-op { width: 64px; }
    .c-dn { width: 66px; } .c-cnt { width: 52px; } .c-ves { width: 68px; }
    .c-prim { width: 190px; } .c-plan { width: 96px; }
    .empty { text-align: center; color: var(--color-text-secondary); padding: var(--space-md); }
  `],
})
export class PodhodModalComponent implements OnInit {
  private readonly api = inject(PodhodApiService);
  private readonly arrivals = inject(ArrivalsApiService);
  private readonly max = inject(MaxApiService);
  private readonly msg = inject(NzMessageService);

  /** Терминал (ports.name_s), обязателен. */
  readonly terminal = input.required<string>();
  /** Предзаполненный фильтр клиентов пресета (через '|'); пусто — без фильтра. */
  readonly clients = input('');
  /** Имя пресета для заголовка/имени файла («Марис»); пусто — обычный подход. */
  readonly presetName = input('');
  readonly closed = output<void>();

  readonly loading = signal(false);
  readonly sending = signal(false);
  readonly report = signal<PodhodReport | null>(null);
  readonly selClients = signal<string[]>([]);
  private readonly terminals = signal<TerminalTarget[]>([]);

  /** Строки как в gtport: по № по убыванию (ближайший прогноз — внизу). */
  readonly rows = computed(() =>
    [...(this.report()?.items ?? [])].sort((a, b) => b.n - a.n));

  /** Опции мультиселекта: клиенты терминала из ответа + выбранные (пресета). */
  readonly clientOptions = computed(() => {
    const set = new Set([...(this.report()?.clients ?? []), ...this.selClients()]);
    return [...set].sort((a, b) => a.localeCompare(b, 'ru'));
  });

  readonly terminalColor = computed(() =>
    this.terminals().find((t) => t.name === this.terminal())?.color || 'transparent');

  ngOnInit(): void {
    this.selClients.set(this.presetClients());
    void this.loadTerminals();
    void this.load();
  }

  private presetClients(): string[] {
    return this.clients().split('|').map((s) => s.trim()).filter(Boolean);
  }

  private async loadTerminals(): Promise<void> {
    try {
      this.terminals.set(await this.arrivals.getTerminals());
    } catch {
      /* реестр нужен только для подкраски — не критичен */
    }
  }

  async load(): Promise<void> {
    this.loading.set(true);
    try {
      this.report.set(await this.api.report(this.terminal(), this.selClients().join('|')));
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
      this.report.set(null);
    } finally {
      this.loading.set(false);
    }
  }

  /** Операция обрезается до 4 символов (как в gtport): «Брошен» → «Брош». */
  opText(op: string): string {
    return op.length > 4 ? op.slice(0, 4) : op;
  }

  /** Цвет станции/индекса группы: единогласная метка подгрупп (prim_4), иначе — обычный. */
  groupColor(item: PodhodItem): string | null {
    const sgs = item.subgroups;
    if (!sgs.length || !sgs[0].prim_4) return null;
    const mark = sgs.every((s) => s.prim_4 === sgs[0].prim_4) ? sgs[0].prim_4 : null;
    return readableTextColor(mark);
  }

  /**
   * Цвет примечания — по имени терминала в тексте из реестра ports (в gtport
   * был хардкод «аттис → синий, нмтп → зелёный»; универсально — цвет терминала).
   * Цвета реестра подобраны заливками, текст бледным не читается — затемняем.
   */
  primColor(prim: string): string | null {
    if (!prim) return null;
    const low = prim.toLowerCase();
    for (const t of this.terminals()) {
      if (t.name && low.includes(t.name.toLowerCase())) {
        return readableTextColor(t.color || null);
      }
    }
    return null;
  }

  /** Вес: без хвоста лишних знаков (до 2 после запятой). */
  fmtVes(v: number): string {
    return String(Math.round(v * 100) / 100);
  }

  /** «2026-07-01…» → «01 июл» (формат колонки «Погрузка» gtport). */
  fmtDDMMM(ts: string | null): string {
    if (!ts || ts.length < 10) return '—';
    const months = ['янв', 'фев', 'мар', 'апр', 'май', 'июн', 'июл', 'авг', 'сен', 'окт', 'ноя', 'дек'];
    return `${ts.slice(8, 10)} ${months[Number(ts.slice(5, 7)) - 1] ?? ''}`;
  }

  /** «2026-07-30T09:00:00» → «30.07 - 09:00» (формат «Плана подвода» gtport). */
  fmtPlan(ts: string | null): string {
    if (!ts || ts.length < 16) return '—';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)} - ${ts.slice(11, 16)}`;
  }

  // ── PNG / MAX ──────────────────────────────────────────────────────────────
  private async png(): Promise<Blob> {
    // Содержимое модалки живёт в overlay-портале CDK, вне host — ищем по документу.
    const el = document.querySelector('#podhod-tbl') as HTMLElement | null;
    if (!el) throw new Error('таблица не найдена');
    // Снимаем ограничение высоты, чтобы в кадр попала вся таблица (как gtport).
    const maxH = el.style.maxHeight;
    el.style.maxHeight = 'none';
    try {
      const blob = await toBlob(el, { pixelRatio: 2, backgroundColor: '#ffffff' });
      if (!blob) throw new Error('не удалось отрисовать картинку');
      return blob;
    } finally {
      el.style.maxHeight = maxH;
    }
  }

  private fileLabel(): string {
    return `${this.terminal()}${this.presetName() ? '_' + this.presetName() : ''}`;
  }

  async exportPng(): Promise<void> {
    try {
      const blob = await this.png();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `Подход_${this.fileLabel()}_${todayMsk()}.png`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    }
  }

  /** Картинка по маршруту формы podhod; пресет — сводным маршрутом (terminal=''). */
  async sendToMax(): Promise<void> {
    this.sending.set(true);
    try {
      const blob = await this.png();
      const routeTerminal = this.presetName() ? '' : this.terminal();
      const caption = `Подход ${this.presetName() || this.terminal()} на ${todayMsk()}`;
      const res = await this.max.sendImage('podhod', routeTerminal, blob,
        `Подход_${this.fileLabel()}_${todayMsk()}.png`, caption);
      if (res.chats === 0) {
        this.msg.warning('Нет настроенного маршрута рассылки (форма «podhod»)');
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

  // ── Excel (клиентский; раскладка повторяет серверный файл gtport) ──────────
  async exportExcel(): Promise<void> {
    const items = this.rows();
    if (!items.length) return;
    try {
      const XLSX = await import('xlsx-js-style');
      const border = { style: 'thin', color: { rgb: '000000' } };
      const borders = { top: border, bottom: border, left: border, right: border };
      const center = { horizontal: 'center', vertical: 'center' };
      const cell = (v: unknown, extra: Record<string, unknown> = {}, num = false) => ({
        v, t: num ? 'n' : 's',
        s: { font: { sz: 12, ...(extra['font'] as object ?? {}) }, border: borders, alignment: center, ...extra },
      });

      const title = `Отчет по порту: ${this.terminal()} - ${this.fmtNow()}`;
      const headers = ['№', 'Дорога', 'Станция опер', 'Индекс поезда', 'Операция',
        'Станция погрузки', 'Погрузка', 'Отправитель', 'Кол-во', 'Вес', 'Примечание', 'План подвода'];

      const rows: unknown[][] = [];
      rows.push([{ v: title, t: 's', s: { font: { sz: 12, bold: true }, alignment: { horizontal: 'left', vertical: 'center' } } }]);
      rows.push(headers.map((h) => cell(h, { font: { bold: true }, fill: { fgColor: { rgb: 'F5F5F5' } } })));

      const merges: object[] = [{ s: { r: 0, c: 0 }, e: { r: 0, c: 11 } }];
      let r = 2;
      for (const item of items) {
        const gc = this.excelColor(this.groupColor(item));
        const groupFont = gc ? { color: { rgb: gc }, bold: true } : {};
        const opFont = item.oper_s === 'Брошен' ? { color: { rgb: 'FF0000' }, bold: true } : {};
        const span = Math.max(item.subgroups.length, 1);
        const sgs: (PodhodSubgroup | null)[] = item.subgroups.length ? item.subgroups : [null];

        sgs.forEach((sg, i) => {
          const primFont = sg && this.primColor(sg.prim_3)
            ? { color: { rgb: this.excelColor(this.primColor(sg.prim_3)) } } : {};
          rows.push([
            cell(item.n, {}, true),
            cell(item.doroga_oper),
            cell(item.station_oper, { font: groupFont }),
            cell(item.index, { font: groupFont }),
            cell(this.opText(item.oper_s), { font: opFont }),
            cell(sg?.station_nach ?? '—'),
            cell(sg ? this.fmtDDMMM(sg.date_nach) : '—'),
            cell(sg?.gruzotpr ?? '—'),
            cell(sg?.vagon_count ?? 0, { numFmt: '0' }, true),
            cell(sg?.total_weight ?? 0, { numFmt: '0.00' }, true),
            cell(sg?.prim_3 ?? '—', { font: primFont }),
            cell(this.fmtPlan(item.plan_msk), { font: { bold: true } }),
          ]);
          if (i === 0 && span > 1) {
            for (const c of [0, 1, 2, 3, 4, 11]) {
              merges.push({ s: { r, c }, e: { r: r + span - 1, c } });
            }
          }
          r++;
        });
      }

      const ws = XLSX.utils.aoa_to_sheet(rows as never[][]);
      ws['!merges'] = merges as never[];
      ws['!cols'] = [3.5, 10, 33.5, 18, 13, 31, 11, 34, 8, 9, 33.5, 15].map((wch) => ({ wch }));
      const wb = XLSX.utils.book_new();
      XLSX.utils.book_append_sheet(wb, ws, todayMsk().slice(8, 10) + '.' + todayMsk().slice(5, 7));
      XLSX.writeFile(wb, `Подход_${this.fileLabel()}_${todayMsk()}.xlsx`);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    }
  }

  /** «#RRGGBB» → «RRGGBB» для xlsx; пусто/не hex — чёрный не задаём. */
  private excelColor(c: string | null): string {
    return c && c.startsWith('#') ? c.slice(1).toUpperCase() : (c ?? '');
  }

  private fmtNow(): string {
    const d = todayMsk(); // дата по МСК; минуты в заголовке файла не критичны
    return `${d.slice(8, 10)}.${d.slice(5, 7)}.${d.slice(0, 4)}`;
  }
}
