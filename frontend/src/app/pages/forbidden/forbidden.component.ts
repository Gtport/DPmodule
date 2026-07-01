import { Component } from '@angular/core';
import { RouterLink } from '@angular/router';
import { NzResultModule } from 'ng-zorro-antd/result';
import { NzButtonModule } from 'ng-zorro-antd/button';

@Component({
  selector: 'app-forbidden',
  imports: [RouterLink, NzResultModule, NzButtonModule],
  template: `
    <nz-result nzStatus="403" nzTitle="403 — Доступ запрещён"
               nzSubTitle="У вашей учётной записи нет роли для этого раздела.">
      <div nz-result-extra>
        <a nz-button nzType="primary" routerLink="/home">На главную</a>
      </div>
    </nz-result>
  `,
})
export class ForbiddenComponent {}
