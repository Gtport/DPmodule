import { Component, OnDestroy, OnInit, computed, inject, output, signal } from '@angular/core';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
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
 * оригиналу — строка «Сейчас» с часами МСК и ЖД-сутками. Автообновление раз в
 * минуту, часы — раз в 10 секунд без сети.
 *
 * В шапке — два действия (только для роли диспетчера/администратора):
 * «Обновить из АСУ» (один клик, сразу пересобирает снимок) и «Приём ЛК»
 * (перемещаемая модалка с двухшаговым приёмом файлов).
 */
@Component({
  selector: 'app-system-status-card',
  imports: [NzButtonModule, NzIconModule, NzTagModule, NzTooltipModule, LkIntakeModalComponent],
  template: `
    <div class="card">
      <div class="head">
        <b class="ttl">Статус системы</b>
        <span class="spacer"></span>
        @if (canUpdate()) {
          <button nz-button nzType="text" nzSize="small" [nzLoading]="busyAsu()"
                  nz-tooltip nzTooltipTitle="Обновить из АСУ: заберёт данные и сразу пересоберёт дислокацию"
                  (click)="asuPull()">
            <span nz-icon nzType="cloud-download"></span> АСУ
          </button>
          <button nz-button nzType="text" nzSize="small"
                  nz-tooltip nzTooltipTitle="Приём ЛК: загрузка файлов грузополучателей вручную"
                  (click)="lkOpen.set(true)">
            <span nz-icon nzType="file-excel"></span> ЛК
          </button>
        }
      </div>

      <div class="rows">
        <!-- Сейчас: МСК и операционные ЖД-сутки (час ≥ 18 → дата +1) -->
        <div class="row" nz-tooltip nzTooltipTitle="ЖД-сутки: с 18:00 МСК начинается следующая дата">
          <span class="lbl">Сейчас</span>
          <span class="vals">
            <nz-tag class="chip clk" nzColor="default">МСК {{ nowMsk() }}</nz-tag>
            <nz-tag class="chip clk" nzColor="default">ЖД {{ nowJd() }}</nz-tag>
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
    </div>

    <!-- Приём ЛК — перемещаемая модалка (решение владельца) -->
    @if (lkOpen()) {
      <app-lk-intake-modal (closed)="lkOpen.set(false)" (updated)="onUpdated()" />
    }
  `,
  styles: [`
    .card { background: var(--color-bg-surface); border-radius: var(--radius-card);
            box-shadow: var(--shadow-card); padding: var(--space-sm) var(--space-md) var(--space-sm); }
    .head { display: flex; align-items: center; gap: 4px; margin-bottom: var(--space-xs); }
    .ttl { font-size: var(--font-size-sm); }
    .spacer { flex: 1 1 auto; }
    .rows { display: flex; flex-direction: column; gap: 3px; }
    .row { display: flex; align-items: center; gap: var(--space-sm); font-size: var(--font-size-sm); }
    .lbl { color: var(--color-text-secondary); white-space: nowrap; }
    .lbl.sub { color: var(--color-text-muted); padding-left: var(--space-sm); }
    .vals { margin-left: auto; display: inline-flex; align-items: center; gap: 4px; }
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
