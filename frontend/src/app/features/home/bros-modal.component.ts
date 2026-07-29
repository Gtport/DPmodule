import { Component, OnInit, computed, inject, output, signal } from '@angular/core';
import { DragDropModule } from '@angular/cdk/drag-drop';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzMessageService } from 'ng-zorro-antd/message';
import { toBlob } from 'html-to-image';
import { apiErrorMessage } from '../../core/api/api-error';
import { AuthService } from '../../core/auth/auth.service';
import { todayMsk } from '../../shared/msk-date';
import { ArrivalsApiService } from './arrivals-api.service';
import { MaxApiService } from '../reports/max-api.service';
import { BrosApiService, BrosRecord, BrosReasonCode } from './bros-api.service';
import { BrosJournalModalComponent } from './bros-journal-modal.component';
import { BrosReportModalComponent } from './bros-report-modal.component';

/**
 * Перемещаемая модалка «Брошенные поезда»: активные броски (снимок bros),
 * строки подсвечены цветом терминала (ports.color). Кнопка в строке открывает
 * журнал броска (редактор состояния). Код бросания — с расшифровкой в подсказке.
 */
@Component({
  selector: 'app-bros-modal',
  imports: [
    DragDropModule, NzModalModule, NzButtonModule, NzIconModule, NzTooltipModule,
    BrosJournalModalComponent, BrosReportModalComponent,
  ],
  template: `
    <nz-modal [nzVisible]="true" [nzTitle]="ttl" [nzFooter]="null" nzWidth="1080px"
              [nzMask]="false" (nzOnCancel)="closed.emit()">
      <ng-template #ttl>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          Брошенные поезда ({{ rows().length }})
        </div>
      </ng-template>

      <ng-container *nzModalContent>
        <div class="bar">
          <span class="mut">Активные броски. Клик по «Журнал» — фиксация состояния и кода бросания.</span>
          <span class="spacer"></span>
          <button nz-button nzSize="small" (click)="exportPng()" [disabled]="rows().length === 0"
                  nz-tooltip nzTooltipTitle="Сохранить картинку таблицы">
            <span nz-icon nzType="picture"></span>
          </button>
          @if (canEdit()) {
            <button nz-button nzType="primary" nzSize="small" [nzLoading]="sending()"
                    (click)="sendToMax()" [disabled]="rows().length === 0"
                    nz-tooltip nzTooltipTitle="Сохранить журнал и отправить сводку в Оперативный чат MAX">
              <span nz-icon nzType="send"></span> В MAX
            </button>
          }
          <button nz-button nzSize="small" (click)="showReport.set(true)"
                  nz-tooltip nzTooltipTitle="Отчёт за период (поиск, статистика, Excel)">
            <span nz-icon nzType="bar-chart"></span> Отчёт
          </button>
          <button nz-button nzType="text" nzSize="small" nz-tooltip nzTooltipTitle="Обновить"
                  (click)="load()">
            <span nz-icon nzType="reload"></span>
          </button>
        </div>

        <div class="dp-tbl-wrap" id="bros-active-tbl">
          <table class="dp-tbl">
            <thead>
              <tr>
                <th class="c-idx">Индекс</th>
                <th>Станция</th>
                <th class="c-dor">Дорога</th>
                <th class="c-dt">Дата броса</th>
                <th class="c-term">Терминал</th>
                <th class="c-dt">Подъём запл</th>
                <th class="c-dt">План</th>
                <th class="c-code">Код</th>
                <th>Состав</th>
                <th class="c-vc">Ваг</th>
                <th class="c-act no-snap"></th>
              </tr>
            </thead>
            <tbody>
              @for (r of rows(); track r.id) {
                <tr [style.background]="rowBg(r)">
                  <td class="num idx" [title]="r.index_1">{{ r.index_1 || '—' }}</td>
                  <td class="ell" [title]="r.station_br">{{ r.station_br || '—' }}</td>
                  <td class="c">{{ r.doroga_br || '—' }}</td>
                  <td class="c">{{ fmtDate(r.date_br) }}</td>
                  <td class="c">{{ r.gruzpol_s || '—' }}</td>
                  <td class="c">{{ fmtDate(r.date_pod) }}</td>
                  <td class="c plan" [class.has]="hasPlan(r.plan)">{{ fmtDate(r.plan) }}</td>
                  <td class="c">
                    @if (r.reason) {
                      <span class="code" nz-tooltip [nzTooltipTitle]="codeDesc(r.reason)">{{ r.reason }}</span>
                    } @else { — }
                  </td>
                  <td class="ell" [title]="r.sostav">{{ r.sostav || '—' }}</td>
                  <td class="c num">{{ r.vagon_count }}</td>
                  <td class="c no-snap">
                    @if (canEdit()) {
                      <button nz-button nzType="link" nzSize="small" (click)="editing.set(r)"
                              nz-tooltip nzTooltipTitle="Записать в журнал">
                        <span nz-icon nzType="edit"></span>
                      </button>
                    }
                  </td>
                </tr>
              } @empty {
                <tr><td colspan="11" class="empty">Активных брошенных нет</td></tr>
              }
            </tbody>
          </table>
        </div>

        <p class="hint">Времена — московские. Цвет строки — терминал. План зелёным — назначен подвод.</p>
      </ng-container>
    </nz-modal>

    @if (editing(); as r) {
      <app-bros-journal-modal [record]="r" [codes]="codes()"
                              (saved)="load()" (closed)="editing.set(null)" />
    }
    @if (showReport()) {
      <app-bros-report-modal (closed)="showReport.set(false)" />
    }
  `,
  styles: [`
    .ttl { cursor: move; user-select: none; }
    .bar { display: flex; align-items: center; gap: var(--space-sm); margin-bottom: var(--space-sm);
           font-size: var(--font-size-sm); }
    .spacer { flex: 1 1 auto; }
    .mut { color: var(--color-text-muted); }
    .dp-tbl { table-layout: fixed; }
    .c-idx { width: 120px; } .c-dor { width: 56px; } .c-dt { width: 92px; }
    .c-term { width: 66px; } .c-code { width: 48px; } .c-vc { width: 46px; } .c-act { width: 46px; }
    .num { font-variant-numeric: tabular-nums; }
    .idx, .ell { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .c { text-align: center; }
    .code { font-weight: 600; cursor: help; text-decoration: underline dotted; text-underline-offset: 2px; }
    .plan.has { background: var(--color-success-bg, #d9f7be); font-weight: 600; }
    .empty { text-align: center; color: var(--color-text-secondary); padding: var(--space-md); }
    .hint { margin: var(--space-xs) 0 0; color: var(--color-text-muted); font-size: var(--font-size-sm); }
  `],
})
export class BrosModalComponent implements OnInit {
  private readonly api = inject(BrosApiService);
  /** Журнал и рассылка в MAX — правки: порог operator. */
  readonly canEdit = inject(AuthService).canEdit;
  private readonly arrivals = inject(ArrivalsApiService);
  private readonly ref = inject(MaxApiService);
  private readonly msg = inject(NzMessageService);

  readonly closed = output<void>();

  readonly rows = signal<BrosRecord[]>([]);
  readonly codes = signal<BrosReasonCode[]>([]);
  readonly editing = signal<BrosRecord | null>(null);
  readonly showReport = signal(false);
  readonly sending = signal(false);

  private readonly codesMap = computed(() => {
    const m: Record<string, string> = {};
    for (const c of this.codes()) m[c.code] = c.description;
    return m;
  });
  private readonly termColor = signal<Record<string, string>>({});

  ngOnInit(): void {
    void this.load();
    void this.loadRefs();
  }

  async load(): Promise<void> {
    try {
      this.rows.set(await this.api.getActive());
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    }
  }

  private async loadRefs(): Promise<void> {
    try {
      const [codes, terms] = await Promise.all([
        this.api.getReasonCodes(),
        this.arrivals.getTerminals(),
      ]);
      this.codes.set(codes);
      const cm: Record<string, string> = {};
      for (const t of terms) cm[t.name] = t.color;
      this.termColor.set(cm);
    } catch {
      /* справочники не критичны для показа таблицы */
    }
  }

  /** Светлый фон строки цветом терминала (по gruzpol_s). */
  rowBg(r: BrosRecord): string | null {
    return this.termColor()[r.gruzpol_s] ?? null;
  }

  codeDesc(code: string): string {
    return this.codesMap()[code] || 'Код не найден в справочнике';
  }

  hasPlan(plan: string | null): boolean {
    return !!(plan && plan.length >= 10 && !plan.startsWith('0001'));
  }

  /** «2026-07-24T00:00:00» → «24.07.26»; пусто → «—». */
  fmtDate(ts: string | null): string {
    if (!ts || ts.length < 10) return '—';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)}.${ts.slice(2, 4)}`;
  }

  // ── PNG / MAX (колонка действий в снимок не попадает — .no-snap) ─────────────
  private async png(): Promise<Blob> {
    // Содержимое модалки живёт в overlay-портале CDK, вне host — ищем по документу.
    const el = document.querySelector('#bros-active-tbl') as HTMLElement | null;
    if (!el) throw new Error('таблица не найдена');
    const blob = await toBlob(el, {
      pixelRatio: 2, backgroundColor: '#ffffff',
      filter: (n) => !(n instanceof HTMLElement && n.classList.contains('no-snap')),
    });
    if (!blob) throw new Error('не удалось отрисовать картинку');
    return blob;
  }

  async exportPng(): Promise<void> {
    try {
      const blob = await this.png();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `Брошенные_${todayMsk()}.png`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    }
  }

  /** Фиксируем журнал за сегодня, затем шлём картинку в Оперативный чат (как gtport). */
  async sendToMax(): Promise<void> {
    this.sending.set(true);
    try {
      try {
        await this.api.bulkSave(); // фиксация состояния всех активных перед отправкой
      } catch {
        /* bulk-save не критичен — отправку не срываем */
      }
      const blob = await this.png();
      const res = await this.ref.sendImage('oper', '', blob, `Брошенные_${todayMsk()}.png`,
        `Брошенные поезда на ${this.fmtDate(todayMsk())}`);
      if (res.chats === 0) {
        this.msg.warning('Нет настроенного маршрута рассылки (форма «oper»)');
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
