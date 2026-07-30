import { Component, OnInit, computed, inject, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { DragDropModule } from '@angular/cdk/drag-drop';
import { toBlob } from 'html-to-image';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzDropDownModule, NzContextMenuService, NzDropdownMenuComponent } from 'ng-zorro-antd/dropdown';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzSelectModule } from 'ng-zorro-antd/select';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { NzSwitchModule } from 'ng-zorro-antd/switch';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { blobErrorMessage } from '../../shared/blob-error';
import { todayMsk } from '../../shared/msk-date';
import { ArrivalsApiService, TerminalTarget } from '../home/arrivals-api.service';
import { MaxApiService } from './max-api.service';
import { NmtpApiService, NmtpMode, NmtpReport, NmtpSection, NmtpTrainRow } from './nmtp-api.service';

/** Группа шапки первого уровня: подпись клиента + сколько колонок накрывает. */
interface HeadGroup {
  label: string;
  span: number;
}

/** Форма диалога правки/новой строки (редактор живёт только на экране). */
interface RowForm {
  index: string;
  station: string;
  date: string; // yyyy-MM-dd
  note: string;
  vagon: string;
  counts: number[];
}

/**
 * Перемещаемая модалка «Подход вагонов {терминал}» по форме порта (НМТП) —
 * экранное представление той же формы, что серверная книга .xlsx: шапка в три
 * уровня (клиент / станции погрузки / марка), секции-дороги (пустая дорога —
 * одна полоса-заголовок, причальные станции показываются только с поездами),
 * «БРОШЕННЫЕ ПОЕЗДА - {дорога}» (в колонке прибытия — дата бросания), итоги
 * в ходу/брошенных/на сети и блок «Итог по грузовым колонкам». Раскраска —
 * как экран gtport (замечание владельца 30.07.2026): зелёные полосы дорог,
 * голубые брошенных; жёлтая колонка «итого» и оранжевое «прочее» — из книги.
 *
 * Переключатель «скрыть перестановки» — режим gtport UseNaznachOnly (строго по
 * назначению, без поездов «НА {сосед}»; актуален для терминалов одной станции —
 * АЭ/ГУТ-2). «Ожид. прибытие» — только плановым поездам, время владивостокское
 * (+7 к МСК — как в книге, порт живёт в местном времени). «В MAX» — картинкой
 * по маршруту формы `nmtp` (миграция 000054), Excel — серверный .xlsx в том же
 * режиме, что на экране.
 */
@Component({
  selector: 'app-nmtp-modal',
  imports: [
    FormsModule, DragDropModule, NzButtonModule, NzDropDownModule, NzIconModule,
    NzInputModule, NzModalModule, NzSelectModule, NzSpinModule, NzSwitchModule,
    NzTooltipModule,
  ],
  template: `
    <nz-modal [nzVisible]="true" [nzTitle]="ttl" [nzFooter]="null" nzWidth="1500px"
              [nzMask]="false" (nzOnCancel)="closed.emit()">
      <ng-template #ttl>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          <span class="tbadge" [style.background]="terminalColor()">{{ terminal() }}</span>
          Подход вагонов (форма порта)
          <span class="sub">поездов: {{ report()?.trains_active ?? 0 }}</span>
          @if (dirty()) {
            <span class="edited" nz-tooltip
                  nzTooltipTitle="Правки живут только на экране и уйдут в Excel/MAX/PNG; «Обновить» их сбросит">
              изменено
            </span>
          }
        </div>
      </ng-template>

      <ng-container *nzModalContent>
        <div class="filters">
          <nz-switch nzSize="small" [ngModel]="naznachOnly()" (ngModelChange)="setMode($event)"></nz-switch>
          <span class="mode" nz-tooltip
                nzTooltipTitle="Строго по назначению: без поездов, переставляемых на соседний терминал (режим gtport «по назначению»)">
            скрыть перестановки
          </span>
          <button nz-button nzType="primary" nzSize="small" [nzLoading]="loading()" (click)="load()">
            <span nz-icon nzType="reload"></span> Обновить
          </button>
          <span class="spacer"></span>
          <button nz-button nzSize="small" [nzLoading]="sending()" (click)="sendToMax()" [disabled]="!report()"
                  nz-tooltip nzTooltipTitle="Отправить картинкой в чаты MAX">
            <span nz-icon nzType="send"></span> В MAX
          </button>
          <button nz-button nzType="text" nzSize="small" (click)="exportPng()" [disabled]="!report()"
                  nz-tooltip nzTooltipTitle="Сохранить как картинку">
            <span nz-icon nzType="camera"></span>
          </button>
          <button nz-button nzType="text" nzSize="small" [nzLoading]="downloading()" (click)="exportExcel()"
                  [disabled]="!report()" nz-tooltip nzTooltipTitle="Скачать книгу .xlsx (собирает сервер)">
            <span nz-icon nzType="download"></span>
          </button>
        </div>

        @if (loading()) {
          <div class="center"><nz-spin nzSimple></nz-spin></div>
        } @else if (report(); as r) {
          <div class="dp-tbl-wrap" style="max-height: 70vh" id="nmtp-tbl">
            <table class="dp-tbl nmtp">
              <thead>
                <tr>
                  <th rowspan="3" class="c-idx">ПОЕЗД</th>
                  <th rowspan="3">СТАНЦИЯ</th>
                  <th rowspan="3" class="c-d">ДАТА<br /><small>(принято к перевозке)</small></th>
                  <th rowspan="3" class="c-note">ПРИМЕЧАНИЕ</th>
                  <th rowspan="3" class="c-vag">ВАГОН<br /><small>(для контроля)</small></th>
                  <th rowspan="3" class="c-d">ожид. дата приб.</th>
                  <th rowspan="3" class="c-t">ожид. время приб. <small>(влад.)</small></th>
                  @for (g of headGroups(); track $index) {
                    <th [attr.colspan]="g.span" class="grp-l">{{ g.label }}</th>
                  }
                  @if (r.has_other) { <th rowspan="3" class="other grp-l">ПРОЧЕЕ</th> }
                  <th rowspan="3" class="itogo grp-l">итого</th>
                </tr>
                <tr>
                  @for (c of r.columns; track $index) {
                    <th class="st" [class.grp-l]="groupStart()[$index]">
                      <div class="clip" [title]="c.station">{{ c.station }}</div>
                    </th>
                  }
                </tr>
                <tr>
                  @for (c of r.columns; track $index) {
                    <th class="mark" [class.grp-l]="groupStart()[$index]">{{ c.mark }}</th>
                  }
                </tr>
              </thead>
              <tbody>
                @for (s of activeSections(); track s.label) {
                  <tr class="section">
                    <td [attr.colspan]="fixedCols + cargoCols()">
                      {{ s.label }}
                      <button nz-button nzType="text" nzSize="small" class="addb"
                              title="Добавить свою строку (только на экране)" (click)="openAdd(s)">
                        <span nz-icon nzType="plus"></span>
                      </button>
                    </td>
                    <td class="c">{{ s.total || '' }}</td>
                  </tr>
                  @for (row of s.rows ?? []; track $index) {
                    <tr (contextmenu)="openMenu($event, s, row, rowMenu)" [class.custom]="row.custom">
                      <td class="c">{{ row.index }}</td>
                      <td class="c">{{ row.station_oper }}</td>
                      <td class="c">{{ fmtDate(row.date_nach) }}</td>
                      <td class="c">{{ row.note }}</td>
                      <td class="c num">{{ row.control_vagon }}</td>
                      <td class="c">{{ progDate(row) }}</td>
                      <td class="c">{{ progTime(row) }}</td>
                      @for (c of r.columns; track $index; let ci = $index) {
                        <td class="c num cnt" [class.grp-l]="groupStart()[ci]">{{ row.counts[ci] || '' }}</td>
                      }
                      @if (r.has_other) {
                        <td class="c num cnt grp-l" [class.other]="row.counts[r.columns.length]">
                          {{ row.counts[r.columns.length] || '' }}
                        </td>
                      }
                      <td class="c num cnt itogo grp-l">{{ row.total }}</td>
                    </tr>
                  }
                }
                <tr class="counter">
                  <td [attr.colspan]="fixedCols + cargoCols()">Итого вагонов в ходу</td>
                  <td class="c itogo">{{ activeVagons() }}</td>
                </tr>
                <tr class="counter">
                  <td [attr.colspan]="fixedCols + cargoCols()" title="составы от 20 вагонов">Итого поездов в ходу</td>
                  <td class="c itogo">{{ r.trains_active }}</td>
                </tr>

                @for (s of abandonedSections(); track s.label) {
                  <tr class="section ab">
                    <td [attr.colspan]="fixedCols + cargoCols()"
                        title="в колонке прибытия — дата бросания">
                      БРОШЕННЫЕ ПОЕЗДА - {{ s.label }}
                      <button nz-button nzType="text" nzSize="small" class="addb"
                              title="Добавить свою строку (только на экране)" (click)="openAdd(s)">
                        <span nz-icon nzType="plus"></span>
                      </button>
                    </td>
                    <td class="c">{{ s.total || '' }}</td>
                  </tr>
                  @for (row of s.rows ?? []; track $index) {
                    <tr (contextmenu)="openMenu($event, s, row, rowMenu)" [class.custom]="row.custom">
                      <td class="c">{{ row.index }}</td>
                      <td class="c">{{ row.station_oper }}</td>
                      <td class="c">{{ fmtDate(row.date_nach) }}</td>
                      <td class="c">{{ row.note }}</td>
                      <td class="c num">{{ row.control_vagon }}</td>
                      <td class="c">{{ fmtDate(row.date_bros ?? null) }}</td>
                      <td class="c"></td>
                      @for (c of r.columns; track $index; let ci = $index) {
                        <td class="c num cnt" [class.grp-l]="groupStart()[ci]">{{ row.counts[ci] || '' }}</td>
                      }
                      @if (r.has_other) {
                        <td class="c num cnt grp-l" [class.other]="row.counts[r.columns.length]">
                          {{ row.counts[r.columns.length] || '' }}
                        </td>
                      }
                      <td class="c num cnt itogo grp-l">{{ row.total }}</td>
                    </tr>
                  }
                }
                <tr class="counter">
                  <td [attr.colspan]="fixedCols + cargoCols()">Итого вагонов брошенных</td>
                  <td class="c itogo">{{ abandonedVagons() }}</td>
                </tr>
                <tr class="counter">
                  <td [attr.colspan]="fixedCols + cargoCols()" title="составы от 20 вагонов">Итого поездов брошенных</td>
                  <td class="c itogo">{{ r.trains_abandoned }}</td>
                </tr>
                <tr class="counter">
                  <td [attr.colspan]="fixedCols + cargoCols()">Итого поездов на сети</td>
                  <td class="c itogo">{{ r.trains_active + r.trains_abandoned }}</td>
                </tr>

                <tr class="fhead">
                  <td [attr.colspan]="fixedCols" class="c">Итог по грузовым колонкам</td>
                  @for (g of headGroups(); track $index) {
                    <td [attr.colspan]="g.span" class="c b grp-l">{{ g.label }}</td>
                  }
                  @if (r.has_other) { <td class="c b other grp-l">ПРОЧЕЕ</td> }
                  <td class="c b itogo grp-l">ИТОГО</td>
                </tr>
                <tr class="fhead">
                  <td [attr.colspan]="fixedCols" class="c">Названия колонок</td>
                  @for (c of r.columns; track $index; let ci = $index) {
                    <td class="c wrap" [class.grp-l]="groupStart()[ci]">{{ c.station }}</td>
                  }
                  @if (r.has_other) { <td class="c other grp-l"></td> }
                  <td class="c itogo grp-l"></td>
                </tr>
                <tr class="foot">
                  <td [attr.colspan]="fixedCols" class="c">вагонов (шт.)</td>
                  @for (c of r.columns; track $index; let ci = $index) {
                    <td class="c num cnt" [class.grp-l]="groupStart()[ci]">{{ r.col_counts[ci] || '' }}</td>
                  }
                  @if (r.has_other) {
                    <td class="c num cnt grp-l">{{ r.col_counts[r.columns.length] || '' }}</td>
                  }
                  <td class="c num cnt itogo grp-l">{{ r.total_vagons }}</td>
                </tr>
                <tr class="foot">
                  <td [attr.colspan]="fixedCols" class="c">тонн (тыс. т.)</td>
                  @for (c of r.columns; track $index; let ci = $index) {
                    <td class="c num" [class.grp-l]="groupStart()[ci]">{{ fmtTons(r.col_tons[ci]) }}</td>
                  }
                  @if (r.has_other) {
                    <td class="c num grp-l">{{ fmtTons(r.col_tons[r.columns.length]) }}</td>
                  }
                  <td class="c num itogo grp-l">{{ fmtTons(r.total_tons) }}</td>
                </tr>
              </tbody>
            </table>

            <div class="below">
              <div>Прогноз выгрузки по подходу (ваг/сут): <b>{{ r.unload_forecast.toFixed(1) }}</b></div>
              @if (r.norm > 0) {
                <div>Нагрузка на ж/д сеть: загрузка <b>{{ (r.total_vagons / 1000).toFixed(3) }}</b> тыс. ваг ·
                  норма <b>{{ r.norm }}</b> · ниже нормы на
                  <b>{{ ((1 - r.total_vagons / r.norm) * 100).toFixed(1) }}%</b></div>
              }
              @if (r.client_tons.length) {
                <div>Тоннаж по клиентам (тыс. т.):
                  @for (ct of r.client_tons; track ct.client; let last = $last) {
                    {{ ct.client }} — <b>{{ fmtTons(ct.tons) }}</b>@if (!last) { · }
                  }
                </div>
              }
            </div>
          </div>
        }
      </ng-container>
    </nz-modal>

    <nz-dropdown-menu #rowMenu="nzDropdownMenu">
      <ul nz-menu>
        <li nz-menu-item (click)="openEdit()">Изменить строку…</li>
        @if (!menuRow()?.custom) {
          <li nz-menu-item (click)="openMove()">Переместить в колонку…</li>
        }
        <li nz-menu-item nzDanger (click)="deleteRow()">Убрать строку (с экрана)</li>
      </ul>
    </nz-dropdown-menu>

    @if (editCtx(); as e) {
      <nz-modal [nzVisible]="true" nzWidth="540px"
                [nzTitle]="e.isNew ? 'Своя строка — ' + e.section.label : 'Изменить строку (только на экране)'"
                nzOkText="Применить" (nzOnOk)="applyEdit()" (nzOnCancel)="editCtx.set(null)">
        <ng-container *nzModalContent>
          <div class="frm">
            <label>Поезд <input nz-input [(ngModel)]="editForm.index" /></label>
            <label>Станция <input nz-input [(ngModel)]="editForm.station" /></label>
            <label>Дата (принято к перевозке) <input nz-input type="date" [(ngModel)]="editForm.date" /></label>
            <label>Примечание <input nz-input [(ngModel)]="editForm.note" /></label>
            <label>Вагон для контроля <input nz-input [(ngModel)]="editForm.vagon" /></label>
            <div class="cnts">
              @for (c of report()?.columns ?? []; track $index; let ci = $index) {
                <label class="cnt-l">
                  <span class="cap">{{ c.group }} · {{ c.station }} ({{ c.mark }})</span>
                  <input nz-input type="number" min="0" [(ngModel)]="editForm.counts[ci]" />
                </label>
              }
              @if (report()?.has_other) {
                <label class="cnt-l"><span class="cap">ПРОЧЕЕ</span>
                  <input nz-input type="number" min="0"
                         [(ngModel)]="editForm.counts[report()!.columns.length]" />
                </label>
              }
            </div>
            <div class="frm-total">Итого вагонов: <b>{{ formTotal() }}</b></div>
          </div>
        </ng-container>
      </nz-modal>
    }

    @if (moveCtx(); as m) {
      <nz-modal [nzVisible]="true" nzWidth="480px"
                [nzTitle]="'Переместить поезд ' + m.row.index + ' (' + m.row.station_oper + ')'"
                nzOkText="Переместить" [nzOkLoading]="moving()" (nzOnOk)="applyMove()"
                (nzOnCancel)="moveCtx.set(null)">
        <ng-container *nzModalContent>
          <p class="hint">
            Привязка запоминается по номерам вагонов и действует, пока вагоны в
            подходе — даже если поезд переформируется. «Вернуть по правилам» снимает её.
          </p>
          @if (moveFromOptions().length > 1) {
            <div class="mv-lbl">Что переносим</div>
            <nz-select style="width: 100%" [(ngModel)]="moveFromId">
              <nz-option [nzValue]="0" nzLabel="Весь состав ({{ m.row.total }} ваг.)"></nz-option>
              @for (o of moveFromOptions(); track o.value) {
                <nz-option [nzValue]="o.value" [nzLabel]="o.label"></nz-option>
              }
            </nz-select>
          }
          <div class="mv-lbl">Куда</div>
          <nz-select style="width: 100%" [(ngModel)]="moveColumnId">
            <nz-option [nzValue]="0" nzLabel="— вернуть по правилам раскладки —"></nz-option>
            @for (c of report()?.columns ?? []; track $index) {
              @if (c.id) {
                <nz-option [nzValue]="c.id" [nzLabel]="c.group + ' · ' + c.station + ' (' + c.mark + ')'"></nz-option>
              }
            }
          </nz-select>
        </ng-container>
      </nz-modal>
    }
  `,
  styles: [`
    .ttl { cursor: move; user-select: none; display: flex; align-items: center; gap: var(--space-sm); }
    .ttl .sub { color: var(--color-text-muted); font-weight: 400; font-size: var(--font-size-sm); }
    .tbadge { padding: 0 8px; border-radius: var(--radius-sm); border: 1px solid var(--color-border); font-size: var(--font-size-sm); }
    .filters { display: flex; align-items: center; gap: var(--space-sm); margin-bottom: var(--space-sm); flex-wrap: wrap; }
    .mode { font-size: var(--font-size-sm); color: var(--color-text-secondary); cursor: default; }
    .spacer { flex: 1 1 auto; }
    .center { display: flex; justify-content: center; padding: var(--space-lg); }
    .c { text-align: center; white-space: nowrap; }
    .num { font-variant-numeric: tabular-nums; }
    .cnt { font-weight: 600; }
    .nmtp th { text-align: center; vertical-align: middle; }
    .nmtp th small { font-weight: 400; }
    /* border-collapse: collapse рисует границы «сеткой» таблицы — при скролле
     * они уезжают вместе с контентом, и под sticky-шапкой просвечивают строки.
     * separate отдаёт границу каждой ячейке: у шапки она липнет вместе с ней. */
    .nmtp { border-collapse: separate; border-spacing: 0; }
    /* Сетка заметная (--color-border-dark): с border-light границ на экране
     * практически не было видно — замечание владельца 31.07.2026. */
    .nmtp th, .nmtp td { border: 0; border-right: 1px solid var(--color-border-dark);
      border-bottom: 1px solid var(--color-border-dark); }
    .nmtp thead tr:nth-child(1) th { border-top: 1px solid var(--color-border-dark); }
    .nmtp thead tr:nth-child(1) th:first-child { border-left: 1px solid var(--color-border-dark); }
    .nmtp tbody td:first-child { border-left: 1px solid var(--color-border-dark); }
    /* Шапка из трёх рядов: глобальный .dp-tbl клеит все th к top:0 и при
     * скролле ряды наезжают друг на друга. Даём рядам фиксированные высоты
     * и каждому — свой sticky-отступ (rowspan-ячейки живут в первом ряду). */
    .nmtp thead tr:nth-child(1) { height: 24px; }
    .nmtp thead tr:nth-child(2) { height: 64px; }
    .nmtp thead tr:nth-child(3) { height: 22px; }
    .nmtp thead th { z-index: 2; }
    .nmtp thead tr:nth-child(1) th { top: 0; }
    .nmtp thead tr:nth-child(2) th { top: 24px; }
    .nmtp thead tr:nth-child(3) th { top: 88px; }
    .nmtp .st { max-width: 110px; white-space: normal; font-size: 10px; padding: 2px 4px; }
    /* Ряд станций не должен вырасти выше 64px — иначе отступы разъедутся. */
    .nmtp .st .clip { max-height: 58px; overflow: hidden; }
    .nmtp .mark { background: #e4e4e4; font-size: 11px; }
    /* Стыки групп клиентов на экране не выделяем — обычная сетка (замечание
     * владельца 31.07.2026); утолщённые стыки остаются только в книге .xlsx.
     * Класс .grp-l в шаблоне сохранён на случай возврата выделения. */
    .nmtp .itogo { background: #ffffcc; }
    .nmtp .other { background: #ffe7ba; }
    /* Полосы дорог — зелёные, брошенных — голубые (цвета экрана gtport). */
    .nmtp .section td { background: #a9d08e; font-weight: 600; text-align: center; }
    .nmtp .section.ab td { background: #cff0fc; }
    .nmtp .counter td { text-align: right; font-weight: 600; }
    .nmtp .counter td.itogo { text-align: center; }
    .nmtp .fhead td { font-weight: 600; background: var(--color-bg-subtle); }
    .nmtp .wrap { white-space: normal; font-size: 10px; max-width: 110px; }
    .nmtp .b { font-weight: 600; }
    .nmtp .foot td { font-weight: 600; text-align: center; }
    .c-idx { min-width: 110px; } .c-d { width: 74px; } .c-t { width: 70px; }
    .c-note { width: 96px; } .c-vag { width: 84px; }
    .below { padding: var(--space-sm) 2px; display: flex; flex-direction: column; gap: 2px;
      font-size: var(--font-size-sm); background: #fff; }
    .edited { color: var(--color-warning-text, #d48806); font-weight: 600; font-size: var(--font-size-sm); }
    .nmtp tr.custom td { font-style: italic; }
    .nmtp .section .addb { height: 18px; padding: 0 4px; line-height: 1; }
    .frm { display: flex; flex-direction: column; gap: var(--space-sm); }
    .frm label { display: flex; align-items: center; gap: var(--space-sm); justify-content: space-between; }
    .frm input { max-width: 300px; }
    .cnts { display: grid; grid-template-columns: 1fr 1fr; gap: 4px var(--space-md);
      border-top: 1px dashed var(--color-border); padding-top: var(--space-sm); }
    .cnt-l { display: flex; flex-direction: column; gap: 2px; }
    .cnt-l .cap { font-size: 11px; color: var(--color-text-secondary); }
    .frm-total { text-align: right; }
    .hint { color: var(--color-text-secondary); font-size: var(--font-size-sm); }
    .mv-lbl { margin: var(--space-sm) 0 4px; font-weight: 600; font-size: var(--font-size-sm); }
  `],
})
export class NmtpModalComponent implements OnInit {
  private readonly api = inject(NmtpApiService);
  private readonly arrivals = inject(ArrivalsApiService);
  private readonly max = inject(MaxApiService);
  private readonly msg = inject(NzMessageService);
  private readonly ctxMenu = inject(NzContextMenuService);

  /** Терминал (ports.name_s), обязателен. */
  readonly terminal = input.required<string>();
  readonly closed = output<void>();

  readonly loading = signal(false);
  readonly sending = signal(false);
  readonly downloading = signal(false);
  readonly naznachOnly = signal(false);
  readonly report = signal<NmtpReport | null>(null);
  private readonly terminals = signal<TerminalTarget[]>([]);

  // ── Редактор (правки живут ТОЛЬКО на экране — решение владельца 30.07.2026):
  // экспорт в Excel уходит правленым отчётом, база не трогается. Исключение —
  // «переместить в колонку»: это привязка вагонов на сервере (nmtp_vagon_column).
  readonly dirty = signal(false);
  readonly menuRow = signal<NmtpTrainRow | null>(null);
  private menuSection: NmtpSection | null = null;
  readonly editCtx = signal<{ section: NmtpSection; row: NmtpTrainRow | null; isNew: boolean } | null>(null);
  editForm: RowForm = { index: '', station: '', date: '', note: '', vagon: '', counts: [] };
  readonly moveCtx = signal<{ row: NmtpTrainRow } | null>(null);
  moveColumnId = 0;
  /** Что переносим: 0 — весь состав, id — вагоны колонки, -1 — «прочее». */
  moveFromId = 0;
  readonly moving = signal(false);

  /** Занятые колонки строки переноса — варианты «что переносим» (сборный поезд). */
  readonly moveFromOptions = computed<{ value: number; label: string }[]>(() => {
    const row = this.moveCtx()?.row;
    const r = this.report();
    if (!row || !r) return [];
    const out: { value: number; label: string }[] = [];
    r.columns.forEach((c, ci) => {
      if ((row.counts[ci] || 0) > 0 && c.id) {
        out.push({ value: c.id, label: `${c.group} · ${c.station} (${c.mark}) — ${row.counts[ci]} ваг.` });
      }
    });
    const other = row.counts[r.columns.length] || 0;
    if (other > 0) out.push({ value: -1, label: `ПРОЧЕЕ — ${other} ваг.` });
    return out;
  });

  /** Фиксированные колонки слева (как в книге). */
  readonly fixedCols = 7;

  /** Колонок в матрице груза (с «прочим»). */
  readonly cargoCols = computed(() => {
    const r = this.report();
    return r ? r.columns.length + (r.has_other ? 1 : 0) : 0;
  });

  /** Группы клиентов первого уровня шапки (merge одинаковых подряд). */
  readonly headGroups = computed<HeadGroup[]>(() => {
    const cols = this.report()?.columns ?? [];
    const out: HeadGroup[] = [];
    for (const c of cols) {
      const last = out[out.length - 1];
      if (last && last.label === c.group) last.span++;
      else out.push({ label: c.group, span: 1 });
    }
    return out;
  });

  /** Первая колонка каждой группы — утолщённая левая граница. */
  readonly groupStart = computed<boolean[]>(() => {
    const cols = this.report()?.columns ?? [];
    return cols.map((c, i) => i === 0 || c.group !== cols[i - 1].group);
  });

  /**
   * Секции экрана — как gtport: дороги показываются всегда (пустая — одна
   * полоса-заголовок), причальные станции (Мыс, Находка) — только с поездами.
   */
  readonly activeSections = computed(() =>
    (this.report()?.sections ?? []).filter((s) => !s.is_station || (s.rows?.length ?? 0) > 0));
  readonly abandonedSections = computed(() =>
    (this.report()?.abandoned ?? []).filter((s) => !s.is_station || (s.rows?.length ?? 0) > 0));

  /** Вагоны в ходу / брошенные — суммы секций (сервер отдаёт общий итог). */
  readonly activeVagons = computed(() =>
    (this.report()?.sections ?? []).reduce((a, s) => a + s.total, 0));
  readonly abandonedVagons = computed(() =>
    (this.report()?.abandoned ?? []).reduce((a, s) => a + s.total, 0));

  readonly terminalColor = computed(() =>
    this.terminals().find((t) => t.name === this.terminal())?.color || 'transparent');

  ngOnInit(): void {
    void this.loadTerminals();
    void this.load();
  }

  private async loadTerminals(): Promise<void> {
    try {
      this.terminals.set(await this.arrivals.getTerminals());
    } catch { /* реестр нужен только для подкраски — не критичен */ }
  }

  private mode(): NmtpMode {
    return this.naznachOnly() ? 'naznach' : '';
  }

  setMode(v: boolean): void {
    this.naznachOnly.set(v);
    void this.load();
  }

  async load(): Promise<void> {
    this.loading.set(true);
    try {
      this.report.set(await this.api.report(this.terminal(), this.mode()));
      this.dirty.set(false); // свежие данные затирают экранные правки
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
      this.report.set(null);
    } finally {
      this.loading.set(false);
    }
  }

  // ── Редактор ───────────────────────────────────────────────────────────────
  openMenu(ev: MouseEvent, section: NmtpSection, row: NmtpTrainRow, menu: NzDropdownMenuComponent): void {
    ev.preventDefault();
    this.menuRow.set(row);
    this.menuSection = section;
    this.ctxMenu.create(ev, menu);
  }

  openAdd(section: NmtpSection): void {
    const nCols = (this.report()?.columns.length ?? 0) + 1;
    this.editForm = { index: '', station: '', date: '', note: '', vagon: '', counts: new Array(nCols).fill(0) };
    this.editCtx.set({ section, row: null, isNew: true });
  }

  openEdit(): void {
    const row = this.menuRow();
    const section = this.menuSection;
    if (!row || !section) return;
    const nCols = (this.report()?.columns.length ?? 0) + 1;
    const counts = new Array(nCols).fill(0);
    row.counts?.forEach((v, i) => (counts[i] = v || 0));
    this.editForm = {
      index: row.index,
      station: row.station_oper,
      date: row.date_nach?.slice(0, 10) ?? '',
      note: row.note ?? '',
      vagon: row.control_vagon,
      counts,
    };
    this.editCtx.set({ section, row, isNew: false });
  }

  formTotal(): number {
    return this.editForm.counts.reduce((a, b) => a + (Number(b) || 0), 0);
  }

  applyEdit(): void {
    const ctx = this.editCtx();
    const r = this.report();
    if (!ctx || !r) return;
    const f = this.editForm;
    const counts = f.counts.map((v) => Number(v) || 0);
    let row = ctx.row;
    if (ctx.isNew) {
      row = {
        index: f.index || 'Б/И', station_oper: f.station, date_nach: null, note: f.note,
        control_vagon: f.vagon, prog: null, planned: false, counts, total: 0, custom: true,
      };
      ctx.section.rows = [...(ctx.section.rows ?? []), row];
    } else if (row) {
      row.index = f.index;
      row.station_oper = f.station;
      row.note = f.note;
      row.control_vagon = f.vagon;
      row.counts = counts;
    }
    if (row) row.date_nach = f.date ? `${f.date}T00:00:00` : null;
    this.editCtx.set(null);
    this.afterLocalEdit(r);
  }

  deleteRow(): void {
    const row = this.menuRow();
    const section = this.menuSection;
    const r = this.report();
    if (!row || !section || !r) return;
    section.rows = (section.rows ?? []).filter((x) => x !== row);
    this.afterLocalEdit(r);
  }

  openMove(): void {
    const row = this.menuRow();
    if (!row) return;
    this.moveColumnId = 0;
    this.moveFromId = 0;
    this.moveCtx.set({ row });
  }

  /** Привязка вагонов состава к колонке на сервере; после — свежий отчёт. */
  async applyMove(): Promise<void> {
    const m = this.moveCtx();
    if (!m) return;
    if (!m.row.prog) {
      this.msg.warning('У поезда нет прогноза — перенести нельзя');
      return;
    }
    this.moving.set(true);
    try {
      const res = await this.api.move({
        terminal: this.terminal(), index: m.row.index, station_oper: m.row.station_oper,
        prog: m.row.prog, column_id: this.moveColumnId, from_column_id: this.moveFromId,
      });
      this.msg.success(this.moveColumnId
        ? `Перенесено вагонов: ${res.vagons} — привязка запомнена по номерам`
        : `Привязка снята с ${res.vagons} вагонов`);
      this.moveCtx.set(null);
      await this.load();
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.moving.set(false);
    }
  }

  /**
   * Пересчёт после экранной правки: итоги строк/секций/колонок, счётчики
   * (порог 20), прогноз выгрузки. Тоннаж не трогаем — весов у ручных цифр нет,
   * тонны остаются серверными.
   */
  private afterLocalEdit(r: NmtpReport): void {
    const nCols = r.columns.length + 1;
    const colCounts = new Array(nCols).fill(0);
    let near = 0;
    const walk = (secs: NmtpSection[]) => {
      let vagons = 0;
      let trains = 0;
      for (const s of secs) {
        let total = 0;
        for (const row of s.rows ?? []) {
          row.counts = row.counts ?? [];
          row.total = row.counts.reduce((a, b) => a + (b || 0), 0);
          total += row.total;
          if (row.total >= 20) trains++;
          row.counts.forEach((v, i) => (colCounts[i] += v || 0));
        }
        s.total = total;
        vagons += total;
      }
      return { vagons, trains };
    };
    const act = walk(r.sections);
    const ab = walk(r.abandoned);
    for (const s of r.sections) {
      if (s.near) near += s.total;
    }
    r.col_counts = colCounts;
    r.has_other = r.has_other || colCounts[nCols - 1] > 0;
    r.total_vagons = act.vagons + ab.vagons;
    r.trains_active = act.trains;
    r.trains_abandoned = ab.trains;
    r.unload_forecast = near / 7;
    this.dirty.set(true);
    this.report.set({ ...r }); // новый объект — computed пересчитаются
  }

  /** «2026-07-24…» → «24.07.26». */
  fmtDate(ts: string | null): string {
    if (!ts || ts.length < 10) return '';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)}.${ts.slice(2, 4)}`;
  }

  /** Тоннаж: 3 знака, ноль — пусто. */
  fmtTons(v: number): string {
    return v > 0 ? v.toFixed(3) : '';
  }

  /**
   * Ожидаемое прибытие — только плановым поездам (правило владельца
   * 30.07.2026), время владивостокское: +7 ч к московскому, как в книге.
   */
  private progVlad(row: NmtpTrainRow): Date | null {
    if (!row.planned || !row.prog) return null;
    const d = new Date(row.prog);
    return isNaN(d.getTime()) ? null : new Date(d.getTime() + 7 * 3600_000);
  }

  progDate(row: NmtpTrainRow): string {
    const d = this.progVlad(row);
    if (!d) return '';
    const p = (n: number) => String(n).padStart(2, '0');
    return `${p(d.getDate())}.${p(d.getMonth() + 1)}.${String(d.getFullYear()).slice(2)}`;
  }

  progTime(row: NmtpTrainRow): string {
    const d = this.progVlad(row);
    if (!d) return '';
    const p = (n: number) => String(n).padStart(2, '0');
    return `${p(d.getHours())}:${p(d.getMinutes())}`;
  }

  // ── PNG / MAX / Excel ──────────────────────────────────────────────────────
  private async png(): Promise<Blob> {
    // Содержимое модалки живёт в overlay-портале CDK, вне host — ищем по документу.
    const el = document.querySelector('#nmtp-tbl') as HTMLElement | null;
    if (!el) throw new Error('таблица не найдена');
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

  async exportPng(): Promise<void> {
    try {
      const blob = await this.png();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `Подход вагонов ${this.terminal()} ${todayMsk()}.png`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    }
  }

  /** Картинка по маршруту формы nmtp (чат терминала, миграция 000054). */
  async sendToMax(): Promise<void> {
    this.sending.set(true);
    try {
      const blob = await this.png();
      const caption = `Подход вагонов ${this.terminal()} на ${todayMsk()}`;
      const res = await this.max.sendImage('nmtp', this.terminal(), blob,
        `Подход вагонов ${this.terminal()} ${todayMsk()}.png`, caption);
      if (res.chats === 0) {
        this.msg.warning('Нет настроенного маршрута рассылки (форма «nmtp»)');
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

  /** Книга .xlsx с сервера: с экранными правками — POST правленого отчёта. */
  async exportExcel(): Promise<void> {
    this.downloading.set(true);
    try {
      const blob = this.dirty()
        ? await this.api.excelEdited(this.report()!)
        : await this.api.excel(this.terminal(), this.mode());
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `Подход вагонов ${this.terminal()} ${todayMsk()}.xlsx`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      this.msg.error(await blobErrorMessage(err));
    } finally {
      this.downloading.set(false);
    }
  }
}
