import { computed, signal } from '@angular/core';

/**
 * Учёт свежести фоновых автообновлений карточки. Фоновые тики молчат тостами
 * (раз в минуту задолбали бы), но молчать про сам факт сбоя нельзя: диспетчер
 * принимает решения по снимку, а протухшая таблица без метки — ложь.
 * Со 2-го неудачного тика подряд (≈2 мин) карточка показывает бейдж
 * «данные N мин назад» (класс .dp-stale из styles/dense.css).
 *
 * Использование: `readonly stale = new StaleTracker()`; в load() —
 * `stale.ok()` при успехе, `stale.fail()` в catch.
 */
export class StaleTracker {
  private readonly failed = signal(0);
  private readonly lastSuccessAt = signal<Date | null>(null);
  private readonly lastAttemptAt = signal<Date | null>(null);

  ok(): void {
    this.lastSuccessAt.set(new Date());
    this.lastAttemptAt.set(new Date());
    this.failed.set(0);
  }

  fail(): void {
    this.lastAttemptAt.set(new Date());
    this.failed.update((n) => n + 1);
  }

  /** Данные считаем протухшими со 2-го неудачного тика подряд. */
  readonly stale = computed(() => this.failed() >= 2);

  /** «данные N мин назад» — от последнего УДАЧНОГО ответа. */
  readonly label = computed(() => {
    this.lastAttemptAt(); // зависимость: пересчёт на каждом тике
    const at = this.lastSuccessAt();
    if (!at) return 'данные не получены';
    const min = Math.max(0, Math.floor((Date.now() - at.getTime()) / 60_000));
    return `данные ${min} мин назад`;
  });
}
