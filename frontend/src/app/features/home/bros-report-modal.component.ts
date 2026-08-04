import { Component, OnInit, computed, inject, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { DragDropModule } from '@angular/cdk/drag-drop';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzSelectModule } from 'ng-zorro-antd/select';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { addDaysIso, todayMsk } from '../../shared/msk-date';
import { ArrivalsApiService } from './arrivals-api.service';
import {
  BrosApiService, BrosRecord, BrosReportRow, BrosReasonCode, BrosTopReason, BrosJournalEntry,
} from './bros-api.service';

/** Суточный агрегат периода. */
interface DailyStat { date: string; count: number; created: number; lifted: number; }

/**
 * Перемещаемая модалка «Отчёт по брошенным поездам» (точный перенос gtport
 * BrosReport). Главная таблица с разбивкой суток по кодам; индекс кликабелен —
 * открывает «Детальную информацию» (поля + разбивка + журнал). Иконки в шапке:
 * поиск по индексу (по всей базе), статистика, экспорт Excel, свернуть фильтр.
 * Разбивка/топ причин — из бэкенд-агрегации журнала (`/report`).
 */
@Component({
  selector: 'app-bros-report-modal',
  imports: [
    FormsModule, DragDropModule, NzModalModule, NzButtonModule, NzIconModule,
    NzInputModule, NzSelectModule, NzSpinModule, NzTagModule, NzTooltipModule,
  ],
  template: `
    <nz-modal [nzVisible]="true" [nzTitle]="ttl" [nzFooter]="null" nzWidth="1280px"
              [nzMask]="false" (nzOnCancel)="closed.emit()">
      <ng-template #ttl>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          Отчёт по брошенным поездам — {{ portLabel() }}
        </div>
      </ng-template>

      <ng-container *nzModalContent>
        <div class="toolbar">
          <span class="head">Брошенные поезда — {{ portLabel() }}</span>
          <span class="spacer"></span>
          <button nz-button nzType="text" nzSize="small" (click)="openSearch()" nz-tooltip nzTooltipTitle="Поиск поезда по индексу">
            <span nz-icon nzType="file-search"></span>
          </button>
          <button nz-button nzType="text" nzSize="small" (click)="showStats.set(true)" [disabled]="rows().length === 0"
                  nz-tooltip nzTooltipTitle="Статистика">
            <span nz-icon nzType="bar-chart"></span>
          </button>
          <button nz-button nzType="text" nzSize="small" (click)="exportExcel()" [disabled]="rows().length === 0"
                  nz-tooltip nzTooltipTitle="Экспорт в Excel">
            <span nz-icon nzType="download"></span>
          </button>
          <button nz-button nzType="text" nzSize="small" (click)="showFilters.set(!showFilters())"
                  nz-tooltip nzTooltipTitle="Фильтры">
            <span nz-icon nzType="filter"></span>
          </button>
        </div>

        @if (showFilters()) {
          <div class="filters">
            <nz-select class="term" nzSize="small" [ngModel]="terminal()" (ngModelChange)="terminal.set($event)">
              <nz-option nzValue="" nzLabel="Все терминалы"></nz-option>
              @for (t of terminals(); track t) { <nz-option [nzValue]="t" [nzLabel]="t"></nz-option> }
            </nz-select>
            <label class="fl">Начало <input type="date" class="date" [ngModel]="start()" (ngModelChange)="start.set($event)" /></label>
            <label class="fl">Конец <input type="date" class="date" [ngModel]="end()" (ngModelChange)="end.set($event)" /></label>
            <button nz-button nzType="primary" nzSize="small" [nzLoading]="loading()" (click)="load()">
              <span nz-icon nzType="search"></span> Загрузить
            </button>
            <button nz-button nzSize="small" (click)="last30()">30 дней</button>
          </div>
        }

        @if (loading()) {
          <div class="center"><nz-spin nzSimple></nz-spin></div>
        } @else {
          <div class="dp-tbl-wrap" id="bros-report-tbl">
            <table class="dp-tbl">
              <thead>
                <tr>
                  <th class="c-idx" (click)="sortBy('index_1')">Индекс {{ arrow('index_1') }}</th>
                  <th (click)="sortBy('station_br')">Станция {{ arrow('station_br') }}</th>
                  <th class="c-dor" (click)="sortBy('doroga_br')">Дорога {{ arrow('doroga_br') }}</th>
                  <th class="c-dt" (click)="sortBy('date_br')">Дата броса {{ arrow('date_br') }}</th>
                  <th class="c-dt" (click)="sortBy('date_pod_fact')">Дата подъёма {{ arrow('date_pod_fact') }}</th>
                  <th class="c-dt">Подъём запл</th>
                  <th class="c-code">Код бр</th>
                  <th class="c-days">Суток</th>
                  <th class="c-b hb05">к.5</th>
                  <th class="c-b hok">01✓</th>
                  <th class="c-b hbad">01✗</th>
                  <th class="c-b">прочие</th>
                  <th>Состав</th>
                </tr>
              </thead>
              <tbody>
                @for (r of sortedRows(); track r.id) {
                  <tr [style.background]="rowBg(r)">
                    <td class="c-idx"><a class="idx" (click)="openDetails(r)">{{ r.index_1 || '—' }}</a></td>
                    <td class="ell" [title]="r.station_br">{{ r.station_br || '—' }}</td>
                    <td class="c">{{ r.doroga_br || '—' }}</td>
                    <td class="c">{{ fmtDate(r.date_br) }}</td>
                    <td class="c">{{ fmtDate(r.date_pod_fact) }}</td>
                    <td class="c">{{ fmtDate(r.date_pod) }}</td>
                    <td class="c">
                      @if (r.reason) {
                        <span class="code" nz-tooltip [nzTooltipTitle]="codeDesc(r.reason)">{{ r.reason }}</span>
                      } @else { — }
                    </td>
                    <td class="c"><span class="chip" [class]="chipCls(r.days_total)">{{ r.days_total }}</span></td>
                    <td class="c b c05">{{ r.days_code05 || '-' }}</td>
                    <td class="c b ok">{{ r.days_code01_agreed || '-' }}</td>
                    <td class="c b bad">{{ r.days_code01_notagreed || '-' }}</td>
                    <td class="c b">{{ r.days_other || '-' }}</td>
                    <td class="ell" [title]="r.sostav">{{ r.sostav || '—' }}</td>
                  </tr>
                } @empty {
                  <tr><td colspan="13" class="empty">Нет данных за выбранный период</td></tr>
                }
              </tbody>
            </table>
          </div>
          <p class="hint">Индекс кликабелен — детали и журнал. Суток — простой (дата броса → подъём или сегодня), разбивка по кодам из журнала.</p>
        }
      </ng-container>
    </nz-modal>

    <!-- Детальная информация (клик по индексу) -->
    @if (detailRow(); as d) {
      <nz-modal [nzVisible]="true" [nzTitle]="dttl" [nzFooter]="null" nzWidth="920px"
                [nzMask]="false" (nzOnCancel)="detailRow.set(null)">
        <ng-template #dttl>
          <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
            Детальная информация &nbsp;<span class="sub">{{ d.index_1 }} · {{ d.station_br }} · {{ fmtDate(d.date_br) }}</span>
          </div>
        </ng-template>
        <ng-container *nzModalContent>
          <table class="kv">
            <tr><td class="k">Нач. индекс (index_0):</td><td>{{ d.index_0 || '—' }}</td><td class="k">Тек. индекс (index_1):</td><td>{{ d.index_1 || '—' }}</td></tr>
            <tr><td class="k">Станция:</td><td>{{ d.station_br || '—' }}</td><td class="k">Дорога:</td><td>{{ d.doroga_br || '—' }}</td></tr>
            <tr><td class="k">Дата бросания:</td><td>{{ fmtDateTime(d.date_br) }}</td><td class="k">Грузополучатель:</td><td>{{ d.gruzpol_s || '—' }}</td></tr>
            <tr><td class="k">Подъём запл:</td><td>{{ fmtDate(d.date_pod) }}</td><td class="k">Факт. подъём:</td><td>{{ fmtDate(d.date_pod_fact) }}</td></tr>
            <tr><td class="k">Смотрелся на нитку:</td><td>{{ fmtDateTime(d.prog_0) }}</td><td class="k">Прогноз при подъёме:</td><td>{{ fmtDateTime(d.prog_1) }}</td></tr>
            <tr><td class="k">Ходу до назначения, ч:</td><td>{{ d.to_go != null ? d.to_go.toFixed(2) : '—' }}</td><td class="k">История планов:</td><td>{{ d.plan_history || '—' }}</td></tr>
            <tr><td class="k">Состав:</td><td colspan="3">{{ d.sostav || '—' }}</td></tr>
            <tr><td class="k">Вагонов:</td><td>{{ d.vagon_count }}</td><td class="k">Статус:</td>
              <td><nz-tag [nzColor]="d.status_br ? 'blue' : 'green'">{{ d.status_br ? 'активен' : 'завершён' }}</nz-tag></td></tr>
          </table>

          <div class="box">
            <div class="box-h">Разбивка суток простоя</div>
            <nz-tag>Всего: {{ d.days_total }} сут.</nz-tag>
            @if (d.days_code05) { <nz-tag nzColor="blue">к.5: {{ d.days_code05 }}</nz-tag> }
            @if (d.days_code01_agreed) { <nz-tag nzColor="green">01 согл: {{ d.days_code01_agreed }}</nz-tag> }
            @if (d.days_code01_notagreed) { <nz-tag nzColor="red">01 несогл: {{ d.days_code01_notagreed }}</nz-tag> }
            @if (d.days_other) { <nz-tag>Прочие: {{ d.days_other }}</nz-tag> }
          </div>

          <div class="box-h"><span nz-icon nzType="history"></span> История записей журнала</div>
          @if (detailLoading()) {
            <div class="center"><nz-spin nzSimple nzSize="small"></nz-spin></div>
          } @else {
            <div class="dp-tbl-wrap" style="max-height: 40vh">
              <table class="dp-tbl">
                <thead><tr>
                  <th class="c-dt">Дата</th><th class="c-code">Код</th><th class="c-b">Тип</th>
                  <th>Причина / Комментарий</th><th class="c-dt">Подъём запл</th>
                  <th>Заявка / Письмо</th><th class="c-dt">План подв</th><th class="c-op">Оператор</th>
                </tr></thead>
                <tbody>
                  @for (e of detailJournal(); track e.id) {
                    <tr>
                      <td class="c">{{ fmtDate(e.date) }}</td>
                      <td class="c"><span class="code">{{ e.reason || '—' }}</span></td>
                      <td class="c">{{ agreeLabel(e) }}</td>
                      <td>{{ e.reason_text || e.comment || '—' }}</td>
                      <td class="c">{{ fmtDate(e.date_pod) }}</td>
                      <td>{{ e.zayavka_nomer ? (e.zayavka_nomer + ' / ' + fmtDate(e.zayavka_date)) : '—' }}</td>
                      <td class="c">{{ fmtDate(e.plan_pod) }}</td>
                      <td class="c">{{ e.created_by }}</td>
                    </tr>
                  } @empty {
                    <tr><td colspan="8" class="empty">Записей журнала нет</td></tr>
                  }
                </tbody>
              </table>
            </div>
          }
        </ng-container>
      </nz-modal>
    }

    <!-- Поиск по индексу -->
    @if (showSearch()) {
      <nz-modal [nzVisible]="true" [nzTitle]="sttl" [nzFooter]="null" nzWidth="1120px"
                [nzMask]="false" (nzOnCancel)="showSearch.set(false)">
        <ng-template #sttl>
          <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
            Поиск поезда по индексу
            @if (searchDone()) { <nz-tag nzColor="blue">{{ searchResults().length }} записей</nz-tag> }
          </div>
        </ng-template>
        <ng-container *nzModalContent>
          <div class="filters">
            <input nz-input placeholder="индекс поезда (index_0 или index_1)" [ngModel]="searchQuery()"
                   (ngModelChange)="searchQuery.set($event)" (keydown.enter)="runSearch()" />
            <button nz-button nzType="primary" nzSize="small" [nzLoading]="searching()" (click)="runSearch()">
              <span nz-icon nzType="search"></span> Найти
            </button>
          </div>
          <p class="hint">Поиск по подстроке в index_0/index_1 · минимум 3 символа · по всей истории бросков, не ограничен периодом.</p>
          <div class="dp-tbl-wrap">
            <table class="dp-tbl">
              <thead><tr>
                <th class="c-idx">Индекс</th><th>Станция</th><th class="c-dor">Дорога</th>
                <th class="c-dt">Дата броса</th><th class="c-dt">Дата подъёма</th><th class="c-dt">Подъём запл</th>
                <th class="c-code">Код бр</th><th class="c-days">Суток</th><th class="c-st">Статус</th><th>Состав</th>
              </tr></thead>
              <tbody>
                @for (r of searchResults(); track r.id) {
                  <tr [style.background]="rowBg(r)">
                    <td class="c-idx"><a class="idx" (click)="openDetailsFromSearch(r)">{{ r.index_1 || '—' }}</a></td>
                    <td class="ell" [title]="r.station_br">{{ r.station_br || '—' }}</td>
                    <td class="c">{{ r.doroga_br || '—' }}</td>
                    <td class="c">{{ fmtDate(r.date_br) }}</td>
                    <td class="c">{{ fmtDate(r.date_pod_fact) }}</td>
                    <td class="c">{{ fmtDate(r.date_pod) }}</td>
                    <td class="c">{{ r.reason || '—' }}</td>
                    <td class="c"><span class="chip" [class]="chipCls(searchDays(r))">{{ searchDays(r) }}</span></td>
                    <td class="c"><nz-tag [nzColor]="r.status_br ? 'blue' : 'green'">{{ r.status_br ? 'активен' : 'поднят' }}</nz-tag></td>
                    <td class="ell" [title]="r.sostav">{{ r.sostav || '—' }}</td>
                  </tr>
                } @empty {
                  <tr><td colspan="10" class="empty">{{ searchDone() ? 'Ничего не найдено' : 'Введите индекс и нажмите «Найти»' }}</td></tr>
                }
              </tbody>
            </table>
          </div>
        </ng-container>
      </nz-modal>
    }

    <!-- Статистика -->
    @if (showStats()) {
      <nz-modal [nzVisible]="true" [nzTitle]="sxttl" [nzFooter]="null" nzWidth="1040px"
                [nzMask]="false" (nzOnCancel)="showStats.set(false)">
        <ng-template #sxttl>
          <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
            Статистика — {{ portLabel() }} &nbsp;<span class="sub">{{ fmtDate(start()) }} – {{ fmtDate(end()) }}</span>
          </div>
        </ng-template>
        <ng-container *nzModalContent>
          <div class="tiles">
            <div class="tile"><b>{{ rows().length }}</b><span>Поездов всего</span></div>
            <div class="tile"><b>{{ totalVagons() }}</b><span>Вагонов всего</span></div>
            <div class="tile"><b>{{ totalDays() }}</b><span>Поездо-суток</span></div>
            <div class="tile"><b>{{ avgDays() }}</b><span>Среднее суток/поезд</span></div>
            <div class="tile"><b>{{ avgDailyCount() }}</b><span>Среднесут. остаток</span></div>
            <div class="tile"><b>{{ totalCreated() }}</b><span>Новых бросков</span></div>
            <div class="tile"><b>{{ totalLifted() }}</b><span>Поднято</span></div>
          </div>

          <div class="box-h">Поездо-сутки по типам причин</div>
          <div class="cards">
            <div class="pc c05"><div class="pc-t"><span>Код 5 — официальная заявка</span><b>{{ daysCode05() }}</b></div>
              <div class="bar"><i [style.width.%]="pct(daysCode05())"></i></div><span class="pc-p">{{ pct(daysCode05()) }}% от итого</span></div>
            <div class="pc ok"><div class="pc-t"><span>Код 01 согласованный (письмо)</span><b>{{ daysAgreed() }}</b></div>
              <div class="bar"><i [style.width.%]="pct(daysAgreed())"></i></div><span class="pc-p">{{ pct(daysAgreed()) }}% от итого</span></div>
            <div class="pc bad"><div class="pc-t"><span>Код 01 несогласованный</span><b>{{ daysNotAgreed() }}</b></div>
              <div class="bar"><i [style.width.%]="pct(daysNotAgreed())"></i></div><span class="pc-p">{{ pct(daysNotAgreed()) }}% от итого</span></div>
            <div class="pc oth"><div class="pc-t"><span>Прочие коды</span><b>{{ daysOther() }}</b></div>
              <div class="bar"><i [style.width.%]="pct(daysOther())"></i></div><span class="pc-p">{{ pct(daysOther()) }}% от итого</span></div>
          </div>
          <div class="prot">
            <div class="prot-t">«Согласованный» простой (код5 + код01 согл.) — {{ protectedPct() }}%</div>
            <div class="bar big"><i [style.width.%]="protectedPct()"></i></div>
            <span class="pc-p">Простой с юр. оформлением (заявка или письмо). Остальные {{ 100 - protectedPct() }}% — «несогласованный» (ответственность РЖД).</span>
          </div>

          @if (topReasons().length) {
            <div class="box-h">Топ причин по поездо-суткам</div>
            <table class="dp-tbl">
              <thead><tr><th class="c-code">Код</th><th>Расшифровка</th><th class="c-b">Поездо-суток</th><th class="c-b">Поездов</th></tr></thead>
              <tbody>
                @for (t of topReasons(); track t.code) {
                  <tr><td class="c"><b>{{ t.code }}</b></td><td class="ell" [title]="reasonDesc(t.code)">{{ reasonDesc(t.code) }}</td>
                    <td class="c num"><b>{{ t.days }}</b></td><td class="c num">{{ t.trains }}</td></tr>
                }
              </tbody>
            </table>
          }

          <div class="box-h">Динамика по дням</div>
          <div class="dp-tbl-wrap" style="max-height: 36vh">
            <table class="dp-tbl">
              <thead><tr><th class="c-dt">Дата</th><th class="c-b">Наличие</th><th class="c-b">Брошено</th><th class="c-b">Поднято</th></tr></thead>
              <tbody>
                @for (d of dailyRows(); track d.date) {
                  <tr [class.peak]="d.count === maxCount() && maxCount() > 0">
                    <td class="c">{{ fmtDate(d.date) }}</td><td class="c num">{{ d.count }}</td>
                    <td class="c num">{{ d.created || '-' }}</td><td class="c num">{{ d.lifted || '-' }}</td></tr>
                }
                <tr class="tot"><td class="c"><b>Итого</b></td><td class="c num"><b>{{ totalDays() }}</b></td>
                  <td class="c num"><b>{{ totalCreated() }}</b></td><td class="c num"><b>{{ totalLifted() }}</b></td></tr>
              </tbody>
            </table>
          </div>
        </ng-container>
      </nz-modal>
    }
  `,
  styles: [`
    .ttl { cursor: move; user-select: none; }
    .ttl .sub { color: var(--color-text-muted); font-weight: 400; font-size: var(--font-size-sm); }
    .toolbar { display: flex; align-items: center; gap: 4px; margin-bottom: var(--space-sm); }
    .toolbar .head { font-weight: 600; }
    /* Иконки шапки — крупные, как в сайдбаре (--layout-icon-size × 1.2 ≈ 19px). */
    .toolbar button { width: 34px; height: 34px; }
    .toolbar [nz-icon] { font-size: calc(var(--layout-icon-size, 16px) * 1.2); }
    .spacer { flex: 1 1 auto; }
    .filters { display: flex; align-items: center; gap: var(--space-sm); margin-bottom: var(--space-sm); flex-wrap: wrap; }
    .filters .ant-input { width: 300px; flex: 0 0 auto; }
    .fl { font-size: var(--font-size-sm); color: var(--color-text-secondary); display: inline-flex; align-items: center; gap: 4px; }
    .term { width: 150px; }
    .date { padding: 3px 6px; border: 1px solid var(--color-border); border-radius: var(--radius-sm); }
    .center { display: flex; justify-content: center; padding: var(--space-lg); }
    .dp-tbl-wrap { background: var(--color-bg-surface); }
    .dp-tbl { table-layout: fixed; }
    .dp-tbl th { cursor: default; padding: 5px 8px; white-space: nowrap; }
    .dp-tbl thead th.c-idx, .dp-tbl thead th:nth-child(2), .dp-tbl thead th.c-dor, .dp-tbl thead th.c-dt { cursor: pointer; }
    .hb05 { color: var(--color-info-text); } .hok { color: var(--color-success-text); } .hbad { color: var(--color-danger-text); }
    .c-idx { width: 120px; } .c-dor { width: 56px; } .c-dt { width: 104px; }
    .c-code { width: 58px; } .c-days { width: 62px; } .c-b { width: 74px; } .c-st { width: 78px; } .c-op { width: 100px; }
    .num, .b { font-variant-numeric: tabular-nums; }
    .idx { color: var(--color-primary-active); text-decoration: underline; cursor: pointer; }
    .ell { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .c { text-align: center; }
    .code { font-weight: 600; cursor: help; text-decoration: underline dotted; text-underline-offset: 2px; }
    td.b.c05 { color: var(--color-info-text); } td.b.ok { color: var(--color-success-text); }
    td.b.bad { color: var(--color-danger-text); font-weight: 600; }
    .chip { display: inline-block; min-width: 26px; padding: 1px 8px; border-radius: 11px; font-weight: 600;
            background: var(--color-bg-hover); color: var(--color-text); font-variant-numeric: tabular-nums; }
    /* Чипы-акценты — язык бейджей приложения (dtag/dp-stale), не material. */
    .chip.warn { background: var(--color-warning-bg); color: var(--color-warning-text);
                 border: 1px solid var(--color-warning); }
    .chip.danger { background: var(--color-danger); color: var(--color-bg-surface); }
    .empty { text-align: center; color: var(--color-text-secondary); padding: var(--space-md); }
    .hint { margin: var(--space-xs) 0 0; color: var(--color-text-muted); font-size: var(--font-size-sm); }
    /* Детали: сетка полей */
    .kv { width: 100%; border-collapse: collapse; font-size: var(--font-size-sm); margin-bottom: var(--space-md); }
    .kv td { border: 1px solid var(--color-border-light); padding: 5px 8px; }
    .kv .k { background: var(--color-bg-subtle); color: var(--color-text-secondary); width: 170px; }
    .box { border: 1px solid var(--color-border-light); border-radius: var(--radius-sm); padding: var(--space-sm);
           margin-bottom: var(--space-md); display: flex; align-items: center; gap: var(--space-xs); flex-wrap: wrap; }
    .box-h { font-weight: 600; font-size: var(--font-size-sm); margin: 0 0 var(--space-xs); }
    /* Статистика */
    .tiles { display: flex; gap: var(--space-sm); flex-wrap: wrap; margin-bottom: var(--space-md); }
    .tile { flex: 1 1 0; min-width: 92px; text-align: center; padding: 6px 8px; border: 1px solid var(--color-border-light);
            border-radius: var(--radius-sm); background: var(--color-bg-surface); }
    .tile b { display: block; font-size: 1.3rem; font-variant-numeric: tabular-nums; line-height: 1.1; }
    .tile span { font-size: var(--font-size-sm); color: var(--color-text-muted); }
    .cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: var(--space-sm); margin-bottom: var(--space-sm); }
    .pc { padding: var(--space-sm); border-radius: var(--radius-sm); border: 1px solid var(--color-border-light); }
    .pc-t { display: flex; justify-content: space-between; align-items: center; font-size: var(--font-size-sm); }
    .pc-t b { font-size: 1.1rem; }
    .pc-p { font-size: var(--font-size-sm); color: var(--color-text-muted); }
    .bar { height: 4px; border-radius: 2px; background: rgba(0,0,0,.08); margin: 4px 0; overflow: hidden; }
    .bar.big { height: 8px; }
    .bar i { display: block; height: 100%; }
    /* Карточки причин — статусные тинты токенов (совместимо с тёмной темой). */
    .pc.c05 { background: var(--color-info-bg); }
    .pc.c05 .pc-t, .pc.c05 b { color: var(--color-info-text); }
    .pc.c05 .bar i { background: var(--color-info); }
    .pc.ok { background: var(--color-success-bg); }
    .pc.ok .pc-t, .pc.ok b { color: var(--color-success-text); }
    .pc.ok .bar i { background: var(--color-success); }
    .pc.bad { background: var(--color-danger-bg); }
    .pc.bad .pc-t, .pc.bad b { color: var(--color-danger-text); }
    .pc.bad .bar i { background: var(--color-danger); }
    .pc.oth { background: var(--color-bg-subtle); }
    .pc.oth .bar i { background: var(--color-text-muted); }
    /* Блок «в ожидании подъёма» — брендовая сине-голубая гамма (был material-фиолет). */
    .prot { border: 1px solid var(--color-primary); border-radius: var(--radius-sm); padding: var(--space-sm);
            background: var(--color-info-bg); margin-bottom: var(--space-md); }
    .prot-t { font-weight: 600; color: var(--color-info-text); font-size: var(--font-size-sm); }
    .prot .bar i { background: var(--color-primary); }
    tr.tot > td { background: var(--color-bg-subtle); }
    tr.peak > td { background: var(--color-warning-bg); }
  `],
})
export class BrosReportModalComponent implements OnInit {
  private readonly api = inject(BrosApiService);
  private readonly arrivals = inject(ArrivalsApiService);
  private readonly msg = inject(NzMessageService);

  readonly closed = output<void>();

  /** Предвыбранный терминал с карточки «Отчёты по брошенным»; '' — все. */
  readonly initialTerminal = input('');

  readonly start = signal(addDaysIso(todayMsk(), -30));
  readonly end = signal(todayMsk());
  readonly terminal = signal('');
  readonly loading = signal(false);
  readonly showFilters = signal(true);

  readonly rows = signal<BrosReportRow[]>([]);
  readonly topReasons = signal<BrosTopReason[]>([]);
  readonly terminals = signal<string[]>([]);
  readonly codes = signal<BrosReasonCode[]>([]);
  private readonly termColor = signal<Record<string, string>>({});

  // Сортировка главной таблицы.
  readonly orderBy = signal<keyof BrosReportRow>('date_br');
  readonly orderDir = signal<'asc' | 'desc'>('desc');

  // Детали поезда.
  readonly detailRow = signal<BrosReportRow | null>(null);
  readonly detailJournal = signal<BrosJournalEntry[]>([]);
  readonly detailLoading = signal(false);

  // Поиск.
  readonly showSearch = signal(false);
  readonly searchQuery = signal('');
  readonly searching = signal(false);
  readonly searchDone = signal(false);
  readonly searchResults = signal<BrosRecord[]>([]);

  // Статистика.
  readonly showStats = signal(false);

  readonly portLabel = computed(() => this.terminal() || 'Все терминалы');

  private readonly codesMap = computed(() => {
    const m: Record<string, string> = {};
    for (const c of this.codes()) m[c.code] = c.description;
    return m;
  });

  readonly sortedRows = computed(() => {
    const by = this.orderBy(), dir = this.orderDir();
    return [...this.rows()].sort((a, b) => {
      const av = a[by], bv = b[by];
      if (typeof av === 'number' && typeof bv === 'number') return dir === 'asc' ? av - bv : bv - av;
      return dir === 'asc'
        ? String(av ?? '').localeCompare(String(bv ?? ''))
        : String(bv ?? '').localeCompare(String(av ?? ''));
    });
  });

  // ── Сводка/статистика ──────────────────────────────────────────────────────
  readonly totalVagons = computed(() => this.rows().reduce((s, r) => s + r.vagon_count, 0));
  readonly totalDays = computed(() => this.rows().reduce((s, r) => s + r.days_total, 0));
  readonly avgDays = computed(() => {
    const n = this.rows().length;
    return n ? Math.round((this.totalDays() / n) * 10) / 10 : 0;
  });
  readonly daysCode05 = computed(() => this.rows().reduce((s, r) => s + r.days_code05, 0));
  readonly daysAgreed = computed(() => this.rows().reduce((s, r) => s + r.days_code01_agreed, 0));
  readonly daysNotAgreed = computed(() => this.rows().reduce((s, r) => s + r.days_code01_notagreed, 0));
  readonly daysOther = computed(() => this.rows().reduce((s, r) => s + r.days_other, 0));
  readonly protectedPct = computed(() => {
    const t = this.totalDays();
    return t ? Math.round(((this.daysCode05() + this.daysAgreed()) / t) * 100) : 0;
  });
  readonly dailyRows = computed<DailyStat[]>(() => this.computeDaily());
  readonly totalCreated = computed(() => this.dailyRows().reduce((s, d) => s + d.created, 0));
  readonly totalLifted = computed(() => this.dailyRows().reduce((s, d) => s + d.lifted, 0));
  readonly avgDailyCount = computed(() => {
    const d = this.dailyRows();
    return d.length ? Math.round((d.reduce((s, x) => s + x.count, 0) / d.length) * 10) / 10 : 0;
  });
  readonly maxCount = computed(() => this.dailyRows().reduce((m, d) => Math.max(m, d.count), 0));

  pct(v: number): number {
    const t = this.totalDays();
    return t ? Math.round((v / t) * 100) : 0;
  }

  ngOnInit(): void {
    this.terminal.set(this.initialTerminal());
    void this.loadRefs();
    void this.load();
  }

  async load(): Promise<void> {
    this.loading.set(true);
    try {
      const res = await this.api.report(this.start(), this.end(), this.terminal());
      this.rows.set(res.records);
      this.topReasons.set(res.top_reasons);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
      this.rows.set([]);
      this.topReasons.set([]);
    } finally {
      this.loading.set(false);
    }
  }

  last30(): void {
    this.start.set(addDaysIso(todayMsk(), -30));
    this.end.set(todayMsk());
    void this.load();
  }

  private async loadRefs(): Promise<void> {
    try {
      const [codes, terms] = await Promise.all([this.api.getReasonCodes(), this.arrivals.getTerminals()]);
      this.codes.set(codes);
      this.terminals.set(terms.map((t) => t.name));
      const cm: Record<string, string> = {};
      for (const t of terms) cm[t.name] = t.color;
      this.termColor.set(cm);
    } catch {
      /* справочники не критичны */
    }
  }

  sortBy(col: keyof BrosReportRow): void {
    if (this.orderBy() === col) this.orderDir.set(this.orderDir() === 'asc' ? 'desc' : 'asc');
    else { this.orderBy.set(col); this.orderDir.set('asc'); }
  }
  arrow(col: keyof BrosReportRow): string {
    return this.orderBy() === col ? (this.orderDir() === 'asc' ? '↑' : '↓') : '';
  }

  rowBg(r: BrosRecord): string | null { return this.termColor()[r.gruzpol_s] ?? null; }
  codeDesc(code: string): string { return this.codesMap()[code] || 'Код не найден в справочнике'; }
  reasonDesc(code: string): string {
    const base = code.replace(' ✓', '').replace(' ✗', '');
    return this.codesMap()[base] || (code === 'неизвестен' ? '—' : '');
  }
  chipCls(days: number): string { return days >= 7 ? 'danger' : days >= 4 ? 'warn' : ''; }

  // ── Детали ──────────────────────────────────────────────────────────────────
  async openDetails(r: BrosReportRow): Promise<void> {
    this.detailRow.set(r);
    this.detailJournal.set([]);
    this.detailLoading.set(true);
    try {
      this.detailJournal.set(await this.api.getJournal(r.id));
    } catch {
      /* журнал не критичен */
    } finally {
      this.detailLoading.set(false);
    }
  }
  /** Из поиска: BrosRecord → добираем суток на фронте (без разбивки) и открываем детали. */
  openDetailsFromSearch(r: BrosRecord): void {
    const row: BrosReportRow = {
      ...r, days_total: this.searchDays(r),
      days_code05: 0, days_code01_agreed: 0, days_code01_notagreed: 0, days_other: 0,
    };
    void this.openDetails(row);
  }
  agreeLabel(e: BrosJournalEntry): string {
    if (e.is_agreed === true) return 'согл.';
    if (e.is_agreed === false) return 'несогл.';
    return '—';
  }

  // ── Поиск ───────────────────────────────────────────────────────────────────
  openSearch(): void { this.showSearch.set(true); }
  async runSearch(): Promise<void> {
    const q = this.searchQuery().trim();
    if (q.length < 3) { this.msg.warning('Минимум 3 символа'); return; }
    this.searching.set(true);
    try {
      this.searchResults.set(await this.api.search(q));
      this.searchDone.set(true);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.searching.set(false);
    }
  }
  /** Суток простоя для строки поиска (простой date-diff, без журнала). */
  searchDays(r: BrosRecord): number {
    const from = dayNum(r.date_br);
    if (from === null) return 0;
    const to = r.date_pod_fact ? dayNum(r.date_pod_fact) : dayNum(todayMsk());
    return to === null ? 0 : Math.max(0, to - from);
  }

  // ── Динамика по дням ─────────────────────────────────────────────────────────
  private computeDaily(): DailyStat[] {
    const s = dayNum(this.start()), e = dayNum(this.end());
    if (s === null || e === null || e < s) return [];
    const out: DailyStat[] = [];
    for (let d = s; d <= e; d++) {
      let count = 0, created = 0, lifted = 0;
      for (const r of this.rows()) {
        const br = dayNum(r.date_br);
        if (br === null) continue;
        const pf = r.date_pod_fact ? dayNum(r.date_pod_fact) : null;
        if (br <= d && (pf === null || pf >= d)) count++;
        if (br === d) created++;
        if (pf === d) lifted++;
      }
      out.push({ date: dayIso(d), count, created, lifted });
    }
    return out;
  }

  // ── Экспорт Excel (формат оригинала gtport BrosReport, 3 листа) ──────────────
  async exportExcel(): Promise<void> {
    if (this.rows().length === 0) return;
    try {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const XLSX: any = await import('xlsx-js-style');
      const wb = XLSX.utils.book_new();
      const ed = (ts: string | null) => (ts && ts.length >= 10 ? this.fmtDate(ts) : '-'); // пусто → «-»

      // ── Лист 1: данные ──
      const h1 = ['Индекс', 'Станция', 'Дорога', 'Дата бросания', 'Дата подъёма (факт)',
        'Подъём запл', 'Код бр', 'Суток всего', 'из них код5', 'из них 01 согл.',
        'из них 01 несогл.', 'Состав', 'Кол-во вагонов', 'История планов'];
      const rows = [...this.rows()].sort((a, b) => (a.date_br ?? '').localeCompare(b.date_br ?? ''))
        .map((r) => [
          r.index_1, r.station_br, r.doroga_br, ed(r.date_br), ed(r.date_pod_fact), ed(r.date_pod),
          r.reason || '', r.days_total, r.days_code05, r.days_code01_agreed, r.days_code01_notagreed,
          r.sostav, r.vagon_count, r.plan_history || '',
        ]);
      const ws1 = XLSX.utils.aoa_to_sheet([h1, ...rows]);
      ws1['!cols'] = [15, 20, 10, 12, 14, 12, 8, 10, 10, 12, 14, 40, 10, 30].map((w: number) => ({ wch: w }));
      const rng = XLSX.utils.decode_range(ws1['!ref'] || 'A1');
      for (let R = rng.s.r; R <= rng.e.r; R++) {
        for (let C = rng.s.c; C <= rng.e.c; C++) {
          const ref = XLSX.utils.encode_cell({ r: R, c: C });
          if (!ws1[ref]) ws1[ref] = { t: 's', v: '' };
          ws1[ref].s = {
            alignment: { horizontal: 'center', vertical: 'center', wrapText: true },
            border: { top: b(), bottom: b(), left: b(), right: b() },
            font: R === 0 ? { bold: true, sz: 11 } : { sz: 10 },
            fill: R === 0 ? { patternType: 'solid', fgColor: { rgb: 'E0E0E0' } } : undefined,
          };
        }
      }
      XLSX.utils.book_append_sheet(wb, ws1, 'Брошенные поезда');

      // ── Лист 2: статистика по дням ──
      const d = this.dailyRows();
      const maxOf = (sel: (x: DailyStat) => number) => d.reduce((m, x) => Math.max(m, sel(x)), 0);
      const dateOfMax = (sel: (x: DailyStat) => number) => {
        const m = maxOf(sel);
        const hit = d.find((x) => sel(x) === m);
        return hit ? this.fmtDate(hit.date) : '-';
      };
      const avg = (sel: (x: DailyStat) => number) => (d.length ? (d.reduce((s, x) => s + sel(x), 0) / d.length).toFixed(1) : '0');
      const sh2 = [
        ['Дата', 'Наличие', 'Брошено', 'Поднято'],
        ...d.map((x) => [this.fmtDate(x.date), x.count, x.created, x.lifted]),
        ['ИТОГО', this.rows().length, this.totalCreated(), this.totalLifted()],
        ['Среднесуточное', avg((x) => x.count), avg((x) => x.created), avg((x) => x.lifted)],
        ['Максимум', maxOf((x) => x.count), maxOf((x) => x.created), maxOf((x) => x.lifted)],
        ['Дата максимума', dateOfMax((x) => x.count), dateOfMax((x) => x.created), dateOfMax((x) => x.lifted)],
      ];
      const ws2 = XLSX.utils.aoa_to_sheet(sh2);
      ws2['!cols'] = [18, 14, 12, 12].map((w: number) => ({ wch: w }));
      XLSX.utils.book_append_sheet(wb, ws2, 'Статистика по дням');

      // ── Лист 3: анализ причин ──
      const tot = this.totalDays();
      const p = (v: number) => (tot ? `${Math.round((v / tot) * 100)}%` : '-');
      const sh3 = [
        ['Показатель', 'Поездо-суток', '% от всего'],
        ['Всего суток простоя', tot, '100%'],
        ['По коду 5 (официальная заявка)', this.daysCode05(), p(this.daysCode05())],
        ['По коду 01 согласованному (письмо)', this.daysAgreed(), p(this.daysAgreed())],
        ['По коду 01 несогласованному', this.daysNotAgreed(), p(this.daysNotAgreed())],
        ['По прочим кодам', this.daysOther(), p(this.daysOther())],
        [''],
        ['Топ причин по поездо-суткам', '', ''],
        ['Код', 'Поездо-суток', 'Уникальных поездов'],
        ...this.topReasons().map((t) => [
          t.code + (this.reasonDesc(t.code) ? ` — ${this.reasonDesc(t.code).substring(0, 60)}` : ''),
          t.days, t.trains,
        ]),
      ];
      const ws3 = XLSX.utils.aoa_to_sheet(sh3);
      ws3['!cols'] = [60, 18, 20].map((w: number) => ({ wch: w }));
      XLSX.utils.book_append_sheet(wb, ws3, 'Анализ причин');

      const label = this.terminal() || 'все_порты';
      XLSX.writeFile(wb, `брошенные_${label}_${this.fmtDate(this.start())}-${this.fmtDate(this.end())}.xlsx`);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    }
  }

  /** «2026-07-24T00:00:00» / «2026-07-24» → «24.07.2026» (полный год); пусто → «—». */
  fmtDate(ts: string | null): string {
    if (!ts || ts.length < 10) return '—';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)}.${ts.slice(0, 4)}`;
  }
  /** «2026-07-24T08:05:00» → «24.07.2026 08:05»; только дата → «24.07.2026»; пусто → «—». */
  fmtDateTime(ts: string | null): string {
    if (!ts || ts.length < 10) return '—';
    const d = this.fmtDate(ts);
    return ts.length >= 16 && ts.includes('T') ? `${d} ${ts.slice(11, 16)}` : d;
  }
}

/** Тонкая чёрная граница ячейки Excel (xlsx-js-style). */
function b() { return { style: 'thin', color: { rgb: '000000' } }; }

/** «YYYY-MM-DD…» → номер дня (дни от эпохи, полдень UTC — без сноса на границе). */
function dayNum(ts: string | null): number | null {
  if (!ts || ts.length < 10) return null;
  const t = Date.parse(ts.slice(0, 10) + 'T12:00:00Z');
  if (Number.isNaN(t)) return null;
  return Math.floor(t / 86400000);
}

/** Номер дня → «YYYY-MM-DD». */
function dayIso(n: number): string {
  return new Date(n * 86400000 + 43200000).toISOString().slice(0, 10);
}
