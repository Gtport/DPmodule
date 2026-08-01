import { Component, input } from '@angular/core';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { LKIssue, LKStatus } from '../dislocation/dislocation-api.service';

/**
 * Список принятых файлов ЛК с замечаниями контроля приёма. Один вид на две
 * модалки — ручную загрузку («ЛК») и автозабор («АВТО ЛК»): диспетчер смотрит
 * на одно и то же, откуда бы файлы ни пришли.
 *
 * Строка — сокращённые имена терминалов, метка формирования с возрастом и чипы
 * замечаний; полное наименование и имя файла — в подсказке. Замечания без своего
 * файла (нет файла, разрыв срезов) идут отдельными строками внизу.
 */
@Component({
  selector: 'app-lk-files-view',
  imports: [NzTagModule, NzTooltipModule],
  template: `
    <div class="files">
      @for (f of status()?.files ?? []; track f.filename) {
        <div class="frow">
          <span class="forg" [title]="f.organisation + ' · ' + f.filename">
            {{ f.terminals.join(' · ') || ('ОКПО ' + f.okpo) }}
          </span>
          <nz-tag class="chip" [nzColor]="ageColor(f.age_minutes)">{{ fmtTs(f.formation_ts) }} · {{ f.age_minutes }}м</nz-tag>
          @for (iss of issuesFor(f.okpo); track iss.code) {
            <nz-tag class="chip" [nzColor]="iss.level === 'block' ? 'error' : 'warning'"
                    nz-tooltip [nzTooltipTitle]="iss.message">{{ issueLabel(iss.code) }}</nz-tag>
          }
        </div>
      } @empty {
        <p class="muted">{{ emptyText() }}</p>
      }
      @for (iss of orphanIssues(); track $index) {
        <div class="frow frow-issue">
          <nz-tag class="chip" [nzColor]="iss.level === 'block' ? 'error' : 'warning'">{{ issueLabel(iss.code) }}</nz-tag>
          <span class="imsg">{{ iss.message }}</span>
        </div>
      }
    </div>
  `,
  styles: [`
    /* Компактный список: каждый файл строго в одну строку. */
    .files { margin-top: var(--space-sm); display: flex; flex-direction: column; }
    .frow {
      display: flex; flex-wrap: nowrap; align-items: center; gap: var(--space-sm);
      padding: 4px 2px; border-bottom: 1px solid var(--color-border, #f0f0f0); font-size: var(--font-size-sm);
    }
    .frow:last-child { border-bottom: none; }
    .frow-issue { color: var(--color-text-secondary); }
    .forg { flex: 1 1 auto; min-width: 60px; font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .imsg { flex: 1 1 auto; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .chip { margin: 0; white-space: nowrap; }
    .muted { color: var(--color-text-muted); margin: var(--space-sm) 0 0; }
  `],
})
export class LkFilesViewComponent {
  readonly status = input<LKStatus | null>(null);
  readonly emptyText = input('Файлы ЛК не загружены.');

  /** Замечания, привязанные к файлу с этим ОКПО (устаревание, неизвестный ОКПО). */
  issuesFor(okpo: string): LKIssue[] {
    return (this.status()?.issues ?? []).filter((i) => i.okpo === okpo);
  }

  /** Общие замечания без своей строки-файла: нет файла (missing) и разрыв срезов (gap). */
  orphanIssues(): LKIssue[] {
    const present = new Set((this.status()?.files ?? []).map((f) => f.okpo));
    return (this.status()?.issues ?? []).filter((i) => !i.okpo || !present.has(i.okpo));
  }

  /** Короткая подпись тега по коду замечания (полный текст — в тултипе/строке). */
  issueLabel(code: string): string {
    switch (code) {
      case 'stale': return 'устарел';
      case 'unknown': return 'нет в справочнике';
      case 'missing': return 'нет файла';
      case 'gap': return 'разрыв срезов';
      default: return code;
    }
  }

  /** Цвет чипа по возрасту метки формирования (мин): ≤60 синий, ≤180 оранжевый, иначе красный. */
  ageColor(age: number): string {
    if (age <= 60) return 'blue';
    if (age <= 180) return 'orange';
    return 'red';
  }

  /** «2026-07-14T03:42:33» → «14.07 03:42». */
  fmtTs(ts: string | null): string {
    if (!ts || ts.length < 16) return '—';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)} ${ts.slice(11, 16)}`;
  }
}
