import { Component, inject } from '@angular/core';
import { UpperCasePipe } from '@angular/common';
import { NzCardModule } from 'ng-zorro-antd/card';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { AuthService } from '../../core/auth/auth.service';
import { environment } from '../../../environments/environment';

@Component({
  selector: 'app-home',
  imports: [UpperCasePipe, NzCardModule, NzTagModule],
  template: `
    <nz-card [nzTitle]="'Шаблон модуля ' + (environment.moduleId | uppercase)" class="card">
      <p class="subtitle">Стартовая страница. Замени на экран модуля.</p>
      <p>Пользователь: <b>{{ auth.username() }}</b></p>
      <p>Роли:</p>
      @for (r of auth.roles(); track r) {
        <nz-tag [nzColor]="'blue'">{{ r }}</nz-tag>
      } @empty {
        <span class="muted">— ролей нет —</span>
      }
    </nz-card>
  `,
  styles: [`
    .card { max-width: 640px; border-radius: var(--radius-card); box-shadow: var(--shadow-card); }
    p { margin: var(--space-sm) 0; }
    .subtitle { color: var(--color-text-secondary); font-size: var(--font-size-subtitle); }
    .muted { color: var(--color-text-muted); }
  `],
})
export class HomeComponent {
  readonly auth = inject(AuthService);
  protected readonly environment = environment;
}
