import { Component, OnDestroy, OnInit, inject, signal } from '@angular/core';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { PlanApiService, DislTermStatus, PlanStatus, SystemStatus } from './plan-api.service';

/** Понятные подписи станций планов и способов обновления дислокации для панели. */
const PLAN_LABELS: Record<string, string> = { ma: 'ПП Мыс', nk: 'ПП Находка' };
const SOURCE_LABELS: Record<string, string> = { lk: 'ЛК', json: 'АСУ', asu: 'АСУ' };

/**
 * Статус-панель актуальности (по образцу gtport StatusPanel): способ и время
 * обновления дислокации по терминалам + актуальность планов подвода. Цвет чипа —
 * по возрасту метки (свежесть). Времена — МСК. Автообновление раз в минуту.
 */
@Component({
  selector: 'app-plan-status-panel',
  imports: [NzTagModule, NzTooltipModule],
  template: `
    <div class="status-bar">
      <span class="cap">Актуальность · МСК</span>

      <!-- Дислокация -->
      <span class="grp">
        <span class="lbl">Дислокация</span>
        @if (status()?.dislocation; as d) {
          <nz-tag class="chip" nzColor="default">{{ sourceLabel(d.source) }}</nz-tag>
          <nz-tag
            class="chip"
            [nzColor]="ageColor(d.age_minutes, 60, 180)"
            nz-tooltip
            [nzTooltipTitle]="d.actor ? 'обновил: ' + d.actor : ''"
          >{{ fmt(d.doc_ts) }}</nz-tag>
          @for (t of d.terminals; track t.organisation) {
            <span class="term">
              <span class="tlbl">{{ termLabel(t) }}</span>
              <nz-tag class="chip" [nzColor]="ageColor(t.age_minutes, 60, 180)">{{ fmt(t.formation_ts) }}</nz-tag>
            </span>
          }
        } @else {
          <nz-tag class="chip" nzColor="default">нет данных</nz-tag>
        }
      </span>

      <!-- Планы подвода -->
      @for (p of status()?.plans ?? []; track p.plan_code) {
        <span class="grp">
          <span class="lbl">{{ planLabel(p.plan_code) }}</span>
          @if (p.loaded) {
            <nz-tag
              class="chip"
              [nzColor]="ageColor(p.age_minutes, 720, 1440)"
              nz-tooltip
              [nzTooltipTitle]="planTip(p)"
            >{{ fmt(p.updated_at) }}</nz-tag>
          } @else {
            <nz-tag class="chip" nzColor="default">—</nz-tag>
          }
        </span>
      }
    </div>
  `,
  styles: [`
    .status-bar {
      display: flex; flex-wrap: wrap; align-items: center; gap: 6px 14px;
      padding: 6px 10px; margin-bottom: 10px;
      border: 1px solid #eee; border-radius: 6px; background: #fafafa;
      font-size: 0.82rem;
    }
    .cap { color: #888; font-weight: 600; margin-right: 4px; }
    .grp { display: inline-flex; align-items: center; gap: 5px; }
    .lbl { color: #555; }
    .term { display: inline-flex; align-items: center; gap: 3px; }
    .tlbl { color: #999; font-size: 0.76rem; }
    .chip { margin: 0; }
    :host ::ng-deep .status-bar .ant-tag { margin: 0; padding: 0 6px; line-height: 18px; }
  `],
})
export class PlanStatusPanelComponent implements OnInit, OnDestroy {
  private readonly api = inject(PlanApiService);
  readonly status = signal<SystemStatus | null>(null);
  private timer: ReturnType<typeof setInterval> | null = null;

  ngOnInit(): void {
    void this.load();
    this.timer = setInterval(() => void this.load(), 60_000); // раз в минуту
  }
  ngOnDestroy(): void {
    if (this.timer) clearInterval(this.timer);
  }

  /** Публичный ре-фетч (плановая загрузка меняет актуальность — дёргаем извне). */
  async load(): Promise<void> {
    try {
      this.status.set(await this.api.getStatus());
    } catch {
      /* тихо: панель некритична, следующий тик повторит */
    }
  }

  sourceLabel(s: string): string { return SOURCE_LABELS[s] ?? s ?? '—'; }
  planLabel(code: string): string { return PLAN_LABELS[code] ?? code.toUpperCase(); }

  termLabel(t: DislTermStatus): string {
    return t.terminals?.length ? t.terminals.join('·') : t.organisation;
  }

  planTip(p: PlanStatus): string {
    const parts: string[] = [];
    if (p.doc_ts) parts.push('план на ' + this.fmt(p.doc_ts));
    if (p.actor) parts.push('загрузил: ' + p.actor);
    if (p.filename) parts.push(p.filename);
    return parts.join(' · ');
  }

  /** Цвет чипа по возрасту (мин): ≤warn — синий, ≤danger — оранжевый, иначе красный. */
  ageColor(age: number, warn: number, danger: number): string {
    if (age <= warn) return 'blue';
    if (age <= danger) return 'orange';
    return 'red';
  }

  /** «2026-07-12T08:10:00» → «12.07 08:10»; пусто → «--.-- --:--». */
  fmt(ts: string | null): string {
    if (!ts || ts.length < 16) return '--.-- --:--';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)} ${ts.slice(11, 16)}`;
  }
}
