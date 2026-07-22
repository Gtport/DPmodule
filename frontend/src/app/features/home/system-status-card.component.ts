import { Component, OnDestroy, OnInit, computed, inject, output, signal } from '@angular/core';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { AuthService } from '../../core/auth/auth.service';
import { DISP } from '../../layout/shell/nav.config';
import { DislTermStatus, PlanApiService, PlanStatus, SystemStatus } from '../plan/plan-api.service';
import { DislocationApiService } from '../dislocation/dislocation-api.service';
import { LkIntakeModalComponent } from './lk-intake-modal.component';
import {
  DISL_AGE, PLAN_AGE, ageColor, fmtStamp, nowJd, nowMsk, planLabel, sourceLabel,
} from '../../shared/status-format';

/**
 * Карточка «Статус системы» в колонке «Оперативка» (перенос функционала страницы
 * «Дислокация» на главный экран, решение владельца).
 *
 * Вид — вертикальный, как у gtport StatusPanel: строка = метка + чип, цвет чипа
 * по возрасту метки (дислокация 60/180 мин, планы 720/1440). Наше добавление к
 * оригиналу — две строки часов: МСК и ЖД-сутки (метка слева, в чипе только
 * дата/время). Автообновление раз в минуту, часы — раз в 10 секунд без сети.
 *
 * Внизу карточки — два действия в одну строку (только для роли диспетчера/
 * администратора): «Обновить АСУ» (один клик, сразу пересобирает снимок) и
 * «Обновить ЛК» (перемещаемая модалка с двухшаговым приёмом файлов).
 */
@Component({
  selector: 'app-system-status-card',
  imports: [NzButtonModule, NzTagModule, NzTooltipModule, LkIntakeModalComponent],
  template: `
    <div class="card">
      <div class="head">
        <b>Статус системы</b>
      </div>

      <div class="rows">
        <!-- Часы: московские и операционные ЖД-сутки (час ≥ 18 → дата +1) -->
        <div class="row">
          <span class="lbl">МСК</span>
          <span class="vals">
            <nz-tag class="chip clk" nzColor="default">{{ nowMsk() }}</nz-tag>
          </span>
        </div>
        <div class="row" nz-tooltip nzTooltipTitle="ЖД-сутки: с 18:00 МСК начинается следующая дата">
          <span class="lbl">ЖД</span>
          <span class="vals">
            <nz-tag class="chip clk" nzColor="default">{{ nowJd() }}</nz-tag>
          </span>
        </div>

        <!-- Дислокация: чем и когда обновлена -->
        <div class="row">
          <span class="lbl">Дислокация</span>
          <span class="vals">
            @if (status()?.dislocation; as d) {
              <nz-tag class="chip" nzColor="default">{{ sourceLabel(d.source) }}</nz-tag>
              <nz-tag class="chip" [nzColor]="ageColor(d.age_minutes, DISL_AGE.warn, DISL_AGE.danger)"
                      nz-tooltip [nzTooltipTitle]="d.actor ? 'обновил: ' + d.actor : ''">
                {{ fmt(d.doc_ts) }}
              </nz-tag>
            } @else {
              <nz-tag class="chip" nzColor="default">нет данных</nz-tag>
            }
          </span>
        </div>

        <!-- Ветки дислокации по грузополучателям (у gtport — Аттис/НМТП) -->
        @for (t of status()?.dislocation?.terminals ?? []; track t.organisation) {
          <div class="row">
            <span class="lbl sub">{{ termLabel(t) }}</span>
            <span class="vals">
              <nz-tag class="chip" [nzColor]="ageColor(t.age_minutes, DISL_AGE.warn, DISL_AGE.danger)">{{ fmt(t.formation_ts) }}</nz-tag>
            </span>
          </div>
        }

        <!-- Планы подвода -->
        @for (p of status()?.plans ?? []; track p.plan_code) {
          <div class="row">
            <span class="lbl">{{ planLabel(p.plan_code) }}</span>
            <span class="vals">
              @if (p.loaded) {
                <nz-tag class="chip" [nzColor]="ageColor(p.age_minutes, PLAN_AGE.warn, PLAN_AGE.danger)"
                        nz-tooltip [nzTooltipTitle]="planTip(p)">{{ fmt(p.updated_at) }}</nz-tag>
              } @else {
                <nz-tag class="chip" nzColor="default">—</nz-tag>
              }
            </span>
          </div>
        }
      </div>

      <!-- Действия диспетчера — внизу карточки, в одну строку (решение владельца). -->
      @if (canUpdate()) {
        <div class="acts">
          <button nz-button nzSize="small" class="act" [nzLoading]="busyAsu()"
                  nz-tooltip nzTooltipTitle="Обновить из АСУ: заберёт данные и сразу пересоберёт дислокацию"
                  (click)="asuPull()">Обновить АСУ</button>
          <button nz-button nzSize="small" class="act"
                  nz-tooltip nzTooltipTitle="Обновить из ЛК: загрузка файлов грузополучателей вручную"
                  (click)="lkOpen.set(true)">Обновить ЛК</button>
        </div>
      }
    </div>

    <!-- Приём ЛК — перемещаемая модалка (решение владельца) -->
    @if (lkOpen()) {
      <app-lk-intake-modal (closed)="lkOpen.set(false)" (updated)="onUpdated()" />
    }
  `,
  styles: [`
    .card { background: var(--color-bg-surface); border-radius: var(--radius-card);
            box-shadow: var(--shadow-card); padding: var(--space-sm) var(--space-md) var(--space-sm); }
    /* Шапка — как у карточек «Прибывшие»/«Ближайшие поезда»: один размер на странице. */
    .head { display: flex; align-items: center; gap: 4px; margin-bottom: var(--space-xs); }
    /* Две кнопки в одну строку, равной ширины; кегль — как у текста карточек. */
    .acts { display: flex; gap: var(--space-sm); margin-top: var(--space-sm); }
    .act { flex: 1 1 0; min-width: 0; font-size: var(--font-size-sm); }
    .rows { display: flex; flex-direction: column; gap: 3px; }
    /* Узкая (половинная) карточка: длинные пары «метка + чипы» переносим, а не режем. */
    .row { display: flex; align-items: center; gap: var(--space-sm); flex-wrap: wrap;
           font-size: var(--font-size-sm); }
    /* Текст — как в таблицах «Прибывшие»/«Ближайшие»: основной цвет, не серый. */
    .lbl { white-space: nowrap; }
    .lbl.sub { padding-left: var(--space-sm); }
    .vals { margin-left: auto; display: inline-flex; align-items: center; gap: 4px; flex-wrap: wrap;
            justify-content: flex-end; }
    .chip { margin: 0; }
    .clk { font-variant-numeric: tabular-nums; font-weight: 600; }
    :host ::ng-deep .chip.ant-tag { margin: 0; padding: 0 6px; line-height: 18px; }
  `],
})
export class SystemStatusCardComponent implements OnInit, OnDestroy {
  private readonly api = inject(PlanApiService);
  private readonly disl = inject(DislocationApiService);
  private readonly auth = inject(AuthService);
  private readonly msg = inject(NzMessageService);

  /** Снимок пересобран (АСУ или ЛК) — главный экран освежает свои счётчики. */
  readonly refreshed = output<void>();

  readonly status = signal<SystemStatus | null>(null);
  readonly now = signal(new Date());
  readonly busyAsu = signal(false);
  readonly lkOpen = signal(false);
  /** Приём ЛК и забор из АСУ — действия диспетчера, прочим ролям не показываем. */
  readonly canUpdate = computed(() => this.auth.hasAnyRole(DISP));

  private timer: ReturnType<typeof setInterval> | null = null;
  private clock: ReturnType<typeof setInterval> | null = null;

  ngOnInit(): void {
    void this.load();
    this.timer = setInterval(() => void this.load(), 60_000);
    this.clock = setInterval(() => this.now.set(new Date()), 10_000);
  }

  ngOnDestroy(): void {
    if (this.timer) clearInterval(this.timer);
    if (this.clock) clearInterval(this.clock);
  }

  async load(): Promise<void> {
    try {
      this.status.set(await this.api.getStatus());
    } catch {
      /* тихо: панель некритична, следующий тик повторит */
    }
  }

  /** Ручной забор из АСУ: один клик — заберёт и сразу пересоберёт снимок. */
  async asuPull(): Promise<void> {
    this.busyAsu.set(true);
    try {
      const res = await this.disl.asuPull();
      this.msg.success(`Дислокация обновлена из АСУ: ${res.count} ваг. (было ${res.prev_snapshot})`);
      this.onUpdated();
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.busyAsu.set(false);
    }
  }

  /** Снимок пересобран (АСУ или ЛК) — освежаем панель и сообщаем наверх. */
  onUpdated(): void {
    void this.load();
    this.refreshed.emit();
  }

  /** Пороги свежести и форматы — из общего модуля (shared/status-format). */
  protected readonly DISL_AGE = DISL_AGE;
  protected readonly PLAN_AGE = PLAN_AGE;
  readonly ageColor = ageColor;

  nowMsk(): string { return nowMsk(this.now()); }
  nowJd(): string { return nowJd(this.now()); }
  sourceLabel(src: string): string { return sourceLabel(src); }
  planLabel(code: string): string { return planLabel(code); }
  fmt(ts: string | null): string { return fmtStamp(ts); }

  termLabel(t: DislTermStatus): string {
    return t.terminals?.length ? t.terminals.join('\u00b7') : t.organisation;
  }

  planTip(p: PlanStatus): string {
    const parts: string[] = [];
    if (p.doc_ts) parts.push('план на ' + this.fmt(p.doc_ts));
    if (p.actor) parts.push('загрузил: ' + p.actor);
    if (p.filename) parts.push(p.filename);
    return parts.join(' \u00b7 ');
  }
}
