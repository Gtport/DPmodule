import { Component, OnInit, inject, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { DragDropModule } from '@angular/cdk/drag-drop';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzRadioModule } from 'ng-zorro-antd/radio';
import { NzSelectModule } from 'ng-zorro-antd/select';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { todayMsk } from '../../shared/msk-date';
import { ArrivalsApiService, TerminalTarget } from '../home/arrivals-api.service';
import { PerestanovkaApiService, PerestanovkaFactRow } from './perestanovka-api.service';
import { loadXlsx } from '../../shared/xlsx';

/**
 * Перемещаемая модалка «Факт. перестановки» (перенос gtport RearrangementFact):
 * строки истории с перестановкой (получатель ≠ назначение) за период по дате
 * прибытия либо выгрузки, срез по терминалу-цели. Колонка «Соответствие» —
 * совпадение назначения с фактическим местом выгрузки (в gtport так ловили
 * вагоны, выгруженные не там, куда переставляли). Excel — на клиенте, полной
 * раскладкой gtport; отходы: «Собственник» = owner, «Марка»/«ГТД» — поля
 * повагонки (freight_exact_name/gtd_number), типы «Аттис/НМТП» заменены
 * выбором терминала из реестра.
 */
@Component({
  selector: 'app-perestanovka-fact-modal',
  imports: [
    FormsModule, DragDropModule, NzButtonModule, NzIconModule, NzModalModule,
    NzRadioModule, NzSelectModule, NzSpinModule, NzTooltipModule,
  ],
  template: `
    <nz-modal [nzVisible]="true" [nzTitle]="ttl" [nzFooter]="null" nzWidth="1360px"
              [nzMask]="false" (nzOnCancel)="closed.emit()">
      <ng-template #ttl>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          Факт перестановок — {{ terminal() || 'все терминалы' }}
        </div>
      </ng-template>

      <ng-container *nzModalContent>
        <div class="filters">
          <nz-select class="term" nzSize="small" [ngModel]="terminal()" (ngModelChange)="terminal.set($event)">
            <nz-option nzValue="" nzLabel="Все терминалы"></nz-option>
            @for (t of terminals(); track t.name) { <nz-option [nzValue]="t.name" [nzLabel]="t.name"></nz-option> }
          </nz-select>
          <nz-radio-group [ngModel]="by()" (ngModelChange)="by.set($event)" nzSize="small">
            <label nz-radio-button nzValue="prib">По прибытию</label>
            <label nz-radio-button nzValue="vigr">По выгрузке</label>
          </nz-radio-group>
          <label class="fl">С <input type="date" class="date" [ngModel]="from()" (ngModelChange)="from.set($event)" /></label>
          <label class="fl">По <input type="date" class="date" [ngModel]="to()" (ngModelChange)="to.set($event)" /></label>
          <button nz-button nzType="primary" nzSize="small" [nzLoading]="loading()" (click)="load()">
            <span nz-icon nzType="search"></span> Загрузить
          </button>
          <button nz-button nzSize="small" (click)="month()">Текущий месяц</button>
          <span class="spacer"></span>
          @if (mismatches() > 0) {
            <span class="warn" nz-tooltip nzTooltipTitle="Вагоны, выгруженные не на терминале назначения — подсвечены в таблице">
              несоответствий: {{ mismatches() }}
            </span>
          }
          <button nz-button nzType="text" nzSize="small" (click)="exportExcel()" [disabled]="!rows().length"
                  nz-tooltip nzTooltipTitle="Экспорт в Excel">
            <span nz-icon nzType="download"></span>
          </button>
        </div>

        @if (loading()) {
          <div class="center"><nz-spin nzSimple></nz-spin></div>
        } @else {
          <div class="dp-tbl-wrap">
            <table class="dp-tbl">
              <thead>
                <tr>
                  <th class="c-vag">Вагон</th>
                  <th class="c-idx">Индекс ПП</th>
                  <th class="c-dt">Дата погр</th>
                  <th>Ст. погрузки</th>
                  <th class="c-term">Получатель</th>
                  <th class="c-term">Назначение</th>
                  <th>Груз</th>
                  <th class="c-ves">Вес</th>
                  <th>Клиент</th>
                  <th class="c-dtt">Прибыл</th>
                  <th class="c-dtt">Выгружен</th>
                  <th class="c-term">Выгружен на</th>
                  <th class="c-ok">Соотв</th>
                </tr>
              </thead>
              <tbody>
                @for (r of rows(); track $index) {
                  <tr [class.mismatch]="!match(r)">
                    <td class="num">{{ r.vagon }}</td>
                    <td class="num idx" [title]="r.index_pp">{{ r.index_pp || '—' }}</td>
                    <td class="c">{{ fmtDay(r.date_nach_d) }}</td>
                    <td class="ell" [title]="r.station_nach">{{ r.station_nach || '—' }}</td>
                    <td class="c">{{ r.gruzpol_s || '—' }}</td>
                    <td class="c b">{{ r.naznach || '—' }}</td>
                    <td class="ell" [title]="r.cargo_s">{{ r.cargo_s || '—' }}</td>
                    <td class="c num">{{ r.ves ?? '—' }}</td>
                    <td class="ell" [title]="r.client">{{ r.client || '—' }}</td>
                    <td class="c">{{ fmtMin(r.date_prib) }}</td>
                    <td class="c">{{ fmtMin(r.date_vigr) }}</td>
                    <td class="c">{{ r.place_vigr || '—' }}</td>
                    <td class="c">{{ match(r) ? '✓' : '✗' }}</td>
                  </tr>
                } @empty {
                  <tr><td colspan="13" class="empty">Нет перестановок за выбранный период</td></tr>
                }
              </tbody>
            </table>
          </div>
          <p class="hint">Строки истории с получателем, отличным от назначения. «Соотв» — выгружен на терминале назначения; ✗ подсвечены.</p>
        }
      </ng-container>
    </nz-modal>
  `,
  styles: [`
    .ttl { cursor: move; user-select: none; }
    .filters { display: flex; align-items: center; gap: var(--space-sm); margin-bottom: var(--space-sm); flex-wrap: wrap; }
    .term { width: 150px; }
    .fl { font-size: var(--font-size-sm); color: var(--color-text-secondary); display: inline-flex; align-items: center; gap: 4px; }
    .date { padding: 3px 6px; border: 1px solid var(--color-border); border-radius: var(--radius-sm); }
    .spacer { flex: 1 1 auto; }
    .warn { color: var(--color-error, #cf1322); font-weight: 600; font-size: var(--font-size-sm); }
    .center { display: flex; justify-content: center; padding: var(--space-lg); }
    .dp-tbl-wrap { max-height: 65vh; overflow: auto; }
    .dp-tbl { table-layout: fixed; }
    .c-vag { width: 84px; } .c-idx { width: 118px; } .c-dt { width: 76px; } .c-dtt { width: 100px; }
    .c-term { width: 100px; } .c-ves { width: 52px; } .c-ok { width: 58px; }
    th { white-space: nowrap; }
    .c { text-align: center; } .b { font-weight: 600; }
    .num { font-variant-numeric: tabular-nums; }
    .idx, .ell { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    tr.mismatch td { background: var(--color-error-bg, #fff1f0); }
    .empty { text-align: center; color: var(--color-text-secondary); padding: var(--space-md); }
    .hint { margin: var(--space-xs) 0 0; color: var(--color-text-muted); font-size: var(--font-size-sm); }
  `],
})
export class PerestanovkaFactModalComponent implements OnInit {
  private readonly api = inject(PerestanovkaApiService);
  private readonly arrivals = inject(ArrivalsApiService);
  private readonly msg = inject(NzMessageService);

  readonly closed = output<void>();

  readonly terminals = signal<TerminalTarget[]>([]);
  readonly terminal = signal('');
  readonly by = signal<'prib' | 'vigr'>('prib');
  readonly from = signal(todayMsk().slice(0, 8) + '01'); // с начала месяца
  readonly to = signal(todayMsk());
  readonly loading = signal(false);
  readonly rows = signal<PerestanovkaFactRow[]>([]);
  readonly mismatches = signal(0);

  ngOnInit(): void {
    void this.arrivals.getTerminals().then(
      (t) => this.terminals.set(t),
      () => { /* без реестра — просто нет вариантов в селекте */ },
    );
    void this.load();
  }

  /** Ещё не выгруженный вагон (пустой place_vigr) расхождением не считается —
   *  сверять нечего (отход от gtport, который метил такие строки «НЕТ»). */
  match(r: PerestanovkaFactRow): boolean {
    return !r.place_vigr || r.place_vigr === r.naznach;
  }

  month(): void {
    this.from.set(todayMsk().slice(0, 8) + '01');
    this.to.set(todayMsk());
    void this.load();
  }

  async load(): Promise<void> {
    this.loading.set(true);
    try {
      const res = await this.api.fact(this.from(), this.to(), this.by(), this.terminal());
      this.rows.set(res.rows);
      this.mismatches.set(res.rows.filter((r) => !this.match(r)).length);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.loading.set(false);
    }
  }

  /** «2026-07-29T…» → «29.07.26». */
  fmtDay(ts?: string | null): string {
    if (!ts || ts.length < 10) return '—';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)}.${ts.slice(2, 4)}`;
  }

  /** «2026-07-29T12:34:56» → «29.07.26 12:34». */
  fmtMin(ts?: string | null): string {
    if (!ts || ts.length < 16) return '—';
    return `${this.fmtDay(ts)} ${ts.slice(11, 16)}`;
  }

  /** Excel полной раскладкой gtport (все строки, включая скрытые с экрана поля). */
  async exportExcel(): Promise<void> {
    try {
      const XLSX = await loadXlsx();
      const headers = [
        'Вагон', 'Накладная Р', 'Накладная', 'Индекс Р', 'Индекс ПП',
        'Дата погр', 'Ст. погр', 'Грузоотпр', 'Получатель', 'Назначение',
        'Груз', 'Вес', 'Клиент', 'Срок доставки', 'Прибыл',
        'ПП', 'Просрочка', 'Выгружен', 'Выгружен на', 'Смерзаемость',
        'Собственник', 'Марка', 'ГТД', 'Судовая', 'Соответствие',
      ];
      const data: (string | number)[][] = [headers];
      for (const r of this.rows()) {
        data.push([
          r.vagon, r.invoice_main || '', r.invoice || '', r.index_main || '', r.index_pp || '',
          this.fmtDay(r.date_nach_d), r.station_nach || '', r.gruzotpr || '', r.gruzpol_s || '', r.naznach || '',
          r.cargo_s || '', r.ves ?? '', r.client || '', this.fmtDay(r.date_dostav), this.fmtMin(r.date_prib),
          this.fmtMin(r.plan_jd), r.delay ? `${r.delay} дн` : '', this.fmtMin(r.date_vigr), r.place_vigr || '',
          r.frost ?? '', r.owner || '', r.marka || '', r.gtd || '', r.shipments || '',
          this.match(r) ? 'ДА' : 'НЕТ',
        ]);
      }
      const ws = XLSX.utils.aoa_to_sheet(data);
      ws['!cols'] = [13, 16, 14, 16, 14, 10, 30, 30, 11, 11, 20, 8, 15, 12, 15, 15, 10, 15, 11, 12, 30, 20, 18, 14, 12]
        .map((wch) => ({ wch }));
      for (let c = 0; c < headers.length; c++) {
        const cell = ws[XLSX.utils.encode_cell({ r: 0, c })];
        if (cell) cell.s = { font: { bold: true }, fill: { patternType: 'solid', fgColor: { rgb: 'F5F5F5' } } };
      }
      const wb = XLSX.utils.book_new();
      XLSX.utils.book_append_sheet(wb, ws, 'Факт перестановок');
      XLSX.writeFile(wb, `Факт перестановок ${this.terminal() || 'все'} ${this.from()}—${this.to()}.xlsx`);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    }
  }
}
