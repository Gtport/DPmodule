import { inject } from '@angular/core';
import { CanActivateFn, Router } from '@angular/router';
import { TimeBaseService } from '../shared/time-base.service';

/**
 * Гард страниц, осмысленных только при НЕСКОЛЬКИХ терминалах («Перестановки»):
 * у клиента с единственным терминалом прямая ссылка уводит на главную —
 * пункт меню и так скрыт (решение владельца 13.08.2026, terminal_count из
 * GET /settings/ui). Настройки не загрузились (null) — экран НЕ отбираем,
 * как и при скрытии пунктов меню: временная ошибка не должна ломать работу.
 */
export const multiTerminalGuard: CanActivateFn = async () => {
  const ui = inject(TimeBaseService);
  const router = inject(Router);
  await ui.init();
  return ui.terminalCount() === 1 ? router.createUrlTree(['/home']) : true;
};
