import { Component, inject, signal } from '@angular/core';
import { FormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { ActivatedRoute, Router } from '@angular/router';
import { NzFormModule } from 'ng-zorro-antd/form';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzAlertModule } from 'ng-zorro-antd/alert';
import { AuthService } from '../../core/auth/auth.service';
import { environment } from '../../../environments/environment';

/** Минимальная форма логина (своя страница, без редиректа на Keycloak). */
@Component({
  selector: 'app-login',
  imports: [
    ReactiveFormsModule,
    NzFormModule, NzInputModule, NzButtonModule, NzIconModule, NzAlertModule,
  ],
  template: `
    <div class="wrap">
      <form nz-form class="box" [formGroup]="form" (ngSubmit)="submit()">
        <h1 class="title">IQPort</h1>
        <p class="subtitle">{{ moduleId }}</p>

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
      </form>
    </div>
  `,
  styles: [`
    .wrap { display: flex; align-items: center; justify-content: center; min-height: 100vh;
            background: var(--color-bg, #edecec); padding: var(--space-md, 16px); }
    .box { width: 320px; padding: var(--space-lg, 24px); background: #fff;
           border-radius: var(--radius-md, 4px); box-shadow: var(--shadow-sm, 0 2px 8px rgba(0,0,0,.1)); }
    .title { margin: 0; text-align: center; font-size: 24px; font-weight: 600; color: var(--color-text, #333); }
    .subtitle { margin: 0 0 var(--space-md, 16px); text-align: center;
                text-transform: uppercase; letter-spacing: 2px; color: var(--color-text-muted, #999); }
    .error { margin-bottom: var(--space-md, 16px); }
  `],
})
export class LoginComponent {
  private readonly fb = inject(FormBuilder);
  private readonly auth = inject(AuthService);
  private readonly router = inject(Router);
  private readonly route = inject(ActivatedRoute);

  readonly moduleId = environment.moduleId;
  readonly loading = signal(false);
  readonly error = signal<string | null>(null);

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
}
