import { Component, computed, inject } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { ActivatedRoute } from '@angular/router';
import { NzCardModule } from 'ng-zorro-antd/card';
import { NzIconModule } from 'ng-zorro-antd/icon';

/**
 * Переиспользуемая заглушка раздела. Один компонент на все пустые страницы;
 * заголовок и иконка приходят из route.data ({ title, icon }). По мере переноса
 * из GTport каждый маршрут заменит эту заглушку на свой реальный компонент.
 *
 * route.data — Observable: при переходе между соседними маршрутами Angular
 * переиспользует этот компонент (не пересоздаёт), поэтому читаем через toSignal,
 * а не из snapshot — иначе заголовок не обновлялся бы.
 */
@Component({
  selector: 'app-placeholder',
  imports: [NzCardModule, NzIconModule],
  template: `
    <nz-card class="card">
      <div class="stub">
        <span nz-icon [nzType]="icon()" class="ico"></span>
        <h1 class="title">{{ title() }}</h1>
        <p class="muted">Раздел в разработке — переносится из GTport.</p>
      </div>
    </nz-card>
  `,
  styles: [`
    .card { border-radius: var(--radius-card); box-shadow: var(--shadow-card); }
    .stub {
      display: flex; flex-direction: column; align-items: center; justify-content: center;
      gap: var(--space-sm); padding: var(--space-xl) var(--space-md); text-align: center;
    }
    .ico { font-size: 48px; color: var(--color-primary); }
    .title { margin: 0; font-size: var(--font-size-page-title); color: var(--color-text); }
    .muted { margin: 0; color: var(--color-text-muted); font-size: var(--font-size-subtitle); }
  `],
})
export class PlaceholderComponent {
  private readonly route = inject(ActivatedRoute);
  private readonly data = toSignal(this.route.data, { initialValue: this.route.snapshot.data });
  readonly title = computed(() => this.data()['title'] ?? 'Раздел');
  readonly icon = computed(() => this.data()['icon'] ?? 'appstore');
}
