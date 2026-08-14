import { Component, computed, inject } from '@angular/core';
import { RouterLink } from '@angular/router';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { TimeBaseService } from '../../shared/time-base.service';
import { HELP_REGIMEN, HELP_TOPICS, HelpRequires, HelpTopic, RegimenRow } from './help-content';

/**
 * Страница «Справка» — регламент смены диспетчера и разборы функционала
 * (решение владельца 14.08.2026). Контент — типизированные объекты в
 * help-content.ts; здесь только рендер: слева оглавление, справа таблица
 * регламента и секции тем с якорями.
 *
 * Оглавление — кнопки со scrollIntoView, а не nz-anchor: скролл-контейнер
 * приложения — внутренний .content шелла (не window), скроллспай nz-anchor
 * пришлось бы прошивать ссылкой через слои; потеря — только подсветка
 * активного пункта.
 *
 * Темы фильтруются настройками клиента (TimeBaseService): без плана подвода
 * нет темы «План», с одним терминалом — «Перестановок», при выключенных
 * уведомлениях — «Уведомлений». null (настройки ещё грузятся) тему НЕ прячет —
 * как в shell: временная ошибка настроек не должна отбирать справку.
 *
 * Скриншоты — public/help/*.png; пока файла нет, картинка прячется по (error),
 * текст справки самодостаточен и без неё.
 */
@Component({
  selector: 'app-help',
  imports: [RouterLink, NzButtonModule, NzIconModule],
  template: `
    <div class="page">
      <aside class="toc">
        <div class="toc-title">Справка</div>
        <button class="toc-item" type="button" (click)="scrollTo('regimen')">Регламент смены</button>
        @for (t of topics(); track t.id) {
          <button class="toc-item" type="button" (click)="scrollTo(t.id)">{{ t.title }}</button>
        }
      </aside>

      <div class="body">
        <section class="sec" id="regimen">
          <h2>Регламент смены</h2>
          <p class="intro">Что и когда делает диспетчер. Времена — МСК; ЖД-сутки — с 18:00 до 18:00.
            Каждая обязанность разобрана ниже — клик по строке ведёт к разбору.</p>
          <table class="reg">
            <thead><tr><th class="when">Когда</th><th>Обязанность</th></tr></thead>
            <tbody>
              @for (r of regimen(); track $index) {
                <tr (click)="scrollTo(r.topicId)">
                  <td class="when"><b>{{ r.when }}</b></td>
                  <td class="duty">{{ r.duty }} <span nz-icon nzType="down" class="go"></span></td>
                </tr>
              }
            </tbody>
          </table>
        </section>

        @for (t of topics(); track t.id) {
          <section class="sec" [id]="t.id">
            <h2>{{ t.title }}
              @if (t.when) { <span class="when-badge">{{ t.when }}</span> }
            </h2>
            @if (t.intro) { <p class="intro">{{ t.intro }}</p> }
            <ol class="steps">
              @for (s of t.steps; track $index) { <li>{{ s }}</li> }
            </ol>
            @if (t.note) {
              <div class="note"><span nz-icon nzType="warning" nzTheme="outline" class="note-ic"></span>{{ t.note }}</div>
            }
            @for (img of t.images ?? []; track img.src) {
              <figure class="shot">
                <img [src]="img.src" [alt]="img.caption || t.title" loading="lazy" (error)="hide($event)" />
                @if (img.caption) { <figcaption>{{ img.caption }}</figcaption> }
              </figure>
            }
            @if (t.links?.length) {
              <div class="links">
                @for (l of t.links; track l.route + l.label) {
                  <a nz-button nzType="primary" nzSize="small"
                     [routerLink]="l.route" [queryParams]="l.query ?? null">
                    <span nz-icon nzType="right"></span> {{ l.label }}
                  </a>
                }
              </div>
            }
          </section>
        }
      </div>
    </div>
  `,
  styles: [`
    .page { display: grid; grid-template-columns: 220px minmax(0, 980px); gap: var(--space-lg);
            align-items: start; }
    /* Оглавление липнет к верху внутреннего скролл-контейнера шелла. */
    .toc { position: sticky; top: 0; display: flex; flex-direction: column; gap: 2px;
           padding: var(--space-sm); background: var(--color-bg-surface);
           border-radius: var(--radius-card); box-shadow: var(--shadow-card); }
    .toc-title { font-weight: 600; margin-bottom: var(--space-xs); }
    .toc-item { border: none; background: none; text-align: left; cursor: pointer;
                padding: 4px 6px; border-radius: var(--radius-sm); color: inherit;
                font-size: var(--font-size-sm); }
    .toc-item:hover { background: var(--color-bg-hover); }
    .body { display: flex; flex-direction: column; gap: var(--space-md); min-width: 0; }
    .sec { background: var(--color-bg-surface); border-radius: var(--radius-card);
           box-shadow: var(--shadow-card); padding: var(--space-md) var(--space-lg);
           /* Якорная прокрутка не должна прятать заголовок под верхний край. */
           scroll-margin-top: var(--space-sm); }
    .sec h2 { margin: 0 0 var(--space-xs); font-size: 1.15rem; display: flex;
              align-items: center; gap: var(--space-sm); flex-wrap: wrap; }
    .when-badge { font-size: var(--font-size-sm); font-weight: 500; padding: 0 8px;
                  border-radius: 10px; background: #f0f7ff; color: #1677ff; }
    .intro { margin: 0 0 var(--space-sm); color: var(--color-text-secondary); line-height: 1.5; }
    .steps { margin: 0; padding-left: 1.25rem; display: flex; flex-direction: column;
             gap: var(--space-xs); }
    .steps li { line-height: 1.5; }
    .note { margin-top: var(--space-sm); padding: var(--space-sm) var(--space-md);
            background: var(--color-warning-bg, #fffbe6); border-radius: var(--radius-sm);
            font-weight: 600; line-height: 1.5; display: flex; gap: var(--space-sm);
            align-items: baseline; }
    .note-ic { color: var(--color-warning-text); }
    .shot { margin: var(--space-md) 0 0; }
    .shot img { max-width: 100%; border: 1px solid var(--color-border-light);
                border-radius: var(--radius-sm); }
    .shot figcaption { color: var(--color-text-secondary); font-size: var(--font-size-sm);
                       margin-top: 2px; }
    .links { margin-top: var(--space-md); display: flex; gap: var(--space-sm); flex-wrap: wrap; }
    .reg { border-collapse: collapse; width: 100%; }
    .reg th, .reg td { border: 1px solid var(--color-border-light); padding: 6px 10px;
                       text-align: left; }
    .reg th { background: var(--color-bg-subtle); }
    .reg tbody tr { cursor: pointer; }
    .reg tbody tr:hover { background: var(--color-bg-hover); }
    .when { width: 160px; white-space: nowrap; }
    .duty .go { font-size: 10px; color: var(--color-text-muted); margin-left: 4px; }
    @media (max-width: 900px) { .page { grid-template-columns: 1fr; } .toc { position: static; } }
  `],
})
export class HelpComponent {
  private readonly uiSettings = inject(TimeBaseService);

  readonly topics = computed<HelpTopic[]>(() => HELP_TOPICS.filter((t) => this.enabled(t.requires)));
  readonly regimen = computed<RegimenRow[]>(() => HELP_REGIMEN.filter((r) => this.enabled(r.requires)));

  /** Тема показывается, пока настройка не доказала обратного (null = грузится). */
  private enabled(req: HelpRequires | undefined): boolean {
    switch (req) {
      case 'plan': {
        const st = this.uiSettings.planStations();
        return st === null || st.length > 0;
      }
      case 'multiTerminal': {
        const n = this.uiSettings.terminalCount();
        return n === null || n > 1;
      }
      case 'notifications':
        return this.uiSettings.notificationsEnabled();
      default:
        return true;
    }
  }

  scrollTo(id: string): void {
    document.getElementById(id)?.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }

  /** Скриншот ещё не снят (файла нет) — прячем рамку, текст самодостаточен. */
  hide(e: Event): void {
    const fig = (e.target as HTMLElement).closest('figure');
    if (fig) (fig as HTMLElement).style.display = 'none';
  }
}
