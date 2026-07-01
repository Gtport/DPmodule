import { Component, inject, signal } from '@angular/core';
import { FormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { ActivatedRoute, Router } from '@angular/router';
import { NzFormModule } from 'ng-zorro-antd/form';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzAlertModule } from 'ng-zorro-antd/alert';
import { AuthService } from '../../core/auth/auth.service';

/**
 * Страница логина: полноэкранный фирменный градиент, по центру — «стеклянная»
 * карточка формы. Фон оживляют всплывающие слова (Мониторинг, Аналитика,
 * Прогнозирование, Моделирование) — лёгкая CSS-анимация на transform/opacity.
 * Логика авторизации не меняется.
 */
@Component({
  selector: 'app-login',
  imports: [
    ReactiveFormsModule,
    NzFormModule, NzInputModule, NzButtonModule, NzIconModule, NzAlertModule,
  ],
  template: `
    <div class="auth">
      <!-- Фон: всплывающие слова -->
      <div class="floats" aria-hidden="true">
        @for (w of floats; track $index) {
          <span class="float"
                [style.left.%]="w.x" [style.top.%]="w.y"
                [style.font-size.px]="w.size"
                [style.animation-delay.s]="w.delay"
                [style.animation-duration.s]="w.dur">{{ w.text }}</span>
        }
      </div>

      <!-- Карточка формы -->
      <form nz-form class="box" [formGroup]="form" (ngSubmit)="submit()">
        <h1 class="brand">IQPort<span class="tm">™</span></h1>

        @if (error()) {
          <nz-alert nzType="error" [nzMessage]="error()!" nzShowIcon class="error" />
        }

        <nz-form-item>
          <nz-form-control nzErrorTip="Введите логин">
            <nz-input-group nzPrefixIcon="user">
              <input nz-input formControlName="username" placeholder="Логин" autocomplete="username" />
            </nz-input-group>
          </nz-form-control>
        </nz-form-item>

        <nz-form-item>
          <nz-form-control nzErrorTip="Введите пароль">
            <nz-input-group nzPrefixIcon="lock">
              <input nz-input type="password" formControlName="password"
                     placeholder="Пароль" autocomplete="current-password" />
            </nz-input-group>
          </nz-form-control>
        </nz-form-item>

        <button nz-button nzType="primary" nzBlock [nzLoading]="loading()"
                [disabled]="loading()">Войти</button>

        <!-- Ссылка «Связаться с поддержкой» -->
        <div class="contact">
          @if (!showEmail()) {
            <button type="button" class="contact__link" (click)="showEmail.set(true)">
              <span nz-icon nzType="mail" nzTheme="outline"></span>
              Связаться с поддержкой
            </button>
          } @else {
            <div class="contact__email">
              <span class="contact__addr">{{ supportEmail }}</span>
              <button type="button" class="contact__copy" (click)="copyEmail()">
                {{ copied() ? '✓ Скопировано' : 'Копировать' }}
              </button>
            </div>
          }
        </div>
      </form>
    </div>
  `,
  styles: [`
    /* Полноэкранный фирменный градиент */
    .auth {
      position: relative;
      min-height: 100vh;
      overflow: hidden;
      display: flex;
      align-items: center;
      justify-content: center;
      padding: var(--space-md);
      background:
        radial-gradient(130% 130% at 78% 12%, var(--color-primary-hover) 0%, transparent 55%),
        linear-gradient(135deg, var(--color-primary) 0%, var(--color-primary-active) 100%);
    }

    /* --- Всплывающие слова --- */
    .floats { position: absolute; inset: 0; overflow: hidden; }
    .float {
      position: absolute;
      transform: translate(-50%, 0);
      color: var(--color-bg-surface);
      font-weight: 600;
      letter-spacing: .5px;
      white-space: nowrap;
      opacity: 0;
      pointer-events: none;
      will-change: transform, opacity;
      animation-name: float-up;
      animation-timing-function: ease-in-out;
      animation-iteration-count: infinite;
    }
    @keyframes float-up {
      0%   { opacity: 0;    transform: translate(-50%, 44px) scale(.94); }
      18%  { opacity: .30; }
      50%  { opacity: .22;  transform: translate(-50%, -8px) scale(1); }
      82%  { opacity: .10; }
      100% { opacity: 0;    transform: translate(-50%, -64px) scale(1.03); }
    }

    /* --- «Стеклянная» карточка формы --- */
    .box {
      position: relative;
      z-index: 1;
      width: 340px;
      max-width: 100%;
      padding: var(--space-xl) var(--space-lg);
      background: rgba(255, 255, 255, .92);
      backdrop-filter: blur(10px);
      -webkit-backdrop-filter: blur(10px);
      border: 1px solid rgba(255, 255, 255, .55);
      border-radius: var(--radius-lg);
      box-shadow: var(--shadow-xl);
    }
    .brand {
      margin: 0 0 var(--space-lg);
      text-align: center;
      font-size: 32px;
      font-weight: 700;
      letter-spacing: .5px;
      color: var(--color-primary-active);
    }
    .brand .tm { font-size: .45em; vertical-align: super; font-weight: 600; opacity: .8; }
    .error { margin-bottom: var(--space-md); }

    /* Ссылка «Связаться» */
    .contact {
      margin-top: var(--space-lg);
      padding-top: var(--space-md);
      text-align: center;
      border-top: 1px solid var(--color-border-light);
    }
    .contact__link {
      display: inline-flex; align-items: center; gap: var(--space-sm);
      padding: var(--space-xs) var(--space-md);
      font: inherit; font-size: var(--font-size-sm); font-weight: 500;
      color: var(--color-primary-active);
      background: var(--color-bg-subtle);
      border: none; border-radius: var(--radius-sm); cursor: pointer;
      transition: background .2s ease, color .2s ease;
    }
    .contact__link:hover { background: var(--color-bg-hover); color: var(--color-primary-hover); }
    .contact__email {
      display: inline-flex; align-items: center; gap: var(--space-sm);
      padding: var(--space-sm) var(--space-md);
      background: var(--color-bg-subtle); border-radius: var(--radius-sm);
    }
    .contact__addr { font-size: var(--font-size-sm); color: var(--color-text-secondary); }
    .contact__copy {
      font: inherit; font-size: var(--font-size-sm);
      padding: 2px var(--space-sm);
      color: var(--color-bg-surface); background: var(--color-primary);
      border: none; border-radius: var(--radius-sm); cursor: pointer;
      transition: background .2s ease;
    }
    .contact__copy:hover { background: var(--color-primary-hover); }

    /* Доступность: без анимации — слова замирают полупрозрачными */
    @media (prefers-reduced-motion: reduce) {
      .float { animation: none; opacity: .16; transform: translate(-50%, 0); }
    }
    /* Узкие экраны — фон-слова убираем, чтобы не мешали форме */
    @media (max-width: 600px) {
      .floats { display: none; }
    }
  `],
})
export class LoginComponent {
  private readonly fb = inject(FormBuilder);
  private readonly auth = inject(AuthService);
  private readonly router = inject(Router);
  private readonly route = inject(ActivatedRoute);

  readonly supportEmail = 'help@gtport.com';
  readonly loading = signal(false);
  readonly error = signal<string | null>(null);
  readonly showEmail = signal(false);
  readonly copied = signal(false);

  /** Всплывающие слова фона: позиция (%), кегль (px), задержка/длительность (с).
   *  Разнесены по краям — центр отдан карточке формы. */
  readonly floats = [
    { text: 'Мониторинг',             x: 16, y: 72, size: 52, delay: 0.0, dur: 9.5 },
    { text: 'Аналитика',              x: 84, y: 66, size: 44, delay: 1.6, dur: 10.5 },
    { text: 'Прогнозирование',        x: 24, y: 26, size: 36, delay: 3.2, dur: 11.5 },
    { text: 'Моделирование',          x: 80, y: 24, size: 42, delay: 4.8, dur: 10.0 },
    { text: 'Флот',                   x: 33, y: 12, size: 52, delay: 6.4, dur: 9.0 },
    { text: 'Карта движения поездов', x: 30, y: 88, size: 29, delay: 8.0, dur: 12.0 },
    { text: 'Претензионная работа',   x: 72, y: 90, size: 29, delay: 9.6, dur: 11.2 },
    { text: 'Дислокация',             x: 88, y: 44, size: 44, delay: 2.4, dur: 12.5 },
    { text: 'Погрузка',               x: 10, y: 48, size: 48, delay: 5.6, dur: 11.0 },
    { text: 'Склад',                  x: 62, y: 14, size: 52, delay: 8.8, dur: 9.8 },
  ];

  readonly form = this.fb.nonNullable.group({
    username: ['', Validators.required],
    password: ['', Validators.required],
  });

  async submit(): Promise<void> {
    if (this.form.invalid) {
      this.form.markAllAsTouched();
      return;
    }
    this.loading.set(true);
    this.error.set(null);
    try {
      const { username, password } = this.form.getRawValue();
      await this.auth.login(username, password);
      const returnUrl = this.route.snapshot.queryParamMap.get('returnUrl') || '/home';
      this.router.navigateByUrl(returnUrl);
    } catch {
      this.error.set('Неверный логин или пароль');
    } finally {
      this.loading.set(false);
    }
  }

  copyEmail(): void {
    navigator.clipboard?.writeText(this.supportEmail);
    this.copied.set(true);
    setTimeout(() => this.copied.set(false), 2000);
  }
}
