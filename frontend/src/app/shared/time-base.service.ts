import { HttpClient } from '@angular/common/http';
import { Injectable, inject, signal } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../environments/environment';

/** Шкала ручного ввода времени в диалогах прибытия/выгрузки. */
export type TimeBase = 'jd' | 'msk';

/**
 * Единая шкала ввода времени для всех диалогов правок (решение владельца
 * 03.08.2026): дефолт приходит из настроек клиента (`GET /settings/ui`,
 * client_settings.extra.ui.time_base; у нас — ЖД), диспетчер может переключить
 * в любом диалоге — выбор действует на сессию во всех диалогах сразу.
 * Пересчёт «час ≥ 18 → ±сутки» при записи делает СЕРВЕР по переданной шкале;
 * здесь только состояние переключателя и сдвиг подставляемых дефолтов.
 *
 * Сервис — клиент `GET /settings/ui` целиком, поэтому здесь же живут и прочие
 * флаги интерфейса из того же ответа: map_enabled (решение владельца
 * 07.08.2026 — карту можно выключить конфигом бэка, тогда пункт меню «Карты»
 * не показывается; init зовёт shell на старте).
 */
@Injectable({ providedIn: 'root' })
export class TimeBaseService {
  private readonly http = inject(HttpClient);
  readonly base = signal<TimeBase>('jd');
  /** Экран «Карта» включён на бэке (map.enabled) — иначе прячем пункт меню. */
  readonly mapEnabled = signal(true);
  /** Уведомления включены на бэке (notifications.enabled + БД) — иначе прячем колокольчик. */
  readonly notificationsEnabled = signal(true);
  private loaded = false;

  /** Подтянуть дефолт из настроек клиента (один раз; ошибка — остаёмся на ЖД). */
  async init(): Promise<void> {
    if (this.loaded) return;
    this.loaded = true;
    try {
      const s = await firstValueFrom(
        this.http.get<{ time_base: string; map_enabled?: boolean; notifications_enabled?: boolean }>(
          `${environment.apiBaseUrl}/v1/settings/ui`));
      if (s.time_base === 'msk' || s.time_base === 'jd') this.base.set(s.time_base);
      if (s.map_enabled === false) this.mapEnabled.set(false);
      if (s.notifications_enabled === false) this.notificationsEnabled.set(false);
    } catch {
      /* настройка не критична — дефолт ЖД, карта видна */
    }
  }

  /** Переключение диспетчером — на время сессии, для всех диалогов. */
  set(b: TimeBase): void {
    this.base.set(b);
  }
}

/**
 * Сдвиг даты на ±сутки, если час ≥ 18 — правило операционных ЖД-суток
 * (только для показа дефолтов и пересчёта полей при переключении шкалы;
 * канонический пересчёт при сохранении делает сервер).
 */
export function shiftDateIfEvening(date: string, time: string, deltaDays: 1 | -1): string {
  if (!date || date.length < 10 || !time || time.length < 2) return date;
  const hour = parseInt(time.slice(0, 2), 10);
  if (isNaN(hour) || hour < 18) return date;
  const d = new Date(
    parseInt(date.slice(0, 4), 10), parseInt(date.slice(5, 7), 10) - 1,
    parseInt(date.slice(8, 10), 10) + deltaDays);
  const mm = String(d.getMonth() + 1).padStart(2, '0');
  const dd = String(d.getDate()).padStart(2, '0');
  return `${d.getFullYear()}-${mm}-${dd}`;
}

/** Дата МСК-события в выбранной шкале: для ЖД вечерние часы уходят в следующие сутки. */
export function mskDateInBase(date: string, time: string, base: TimeBase): string {
  return base === 'jd' ? shiftDateIfEvening(date, time, 1) : date;
}

/** Дата хранимого ЖД-штампа в выбранной шкале: для МСК вечерние часы — предыдущие сутки. */
export function jdDateInBase(date: string, time: string, base: TimeBase): string {
  return base === 'msk' ? shiftDateIfEvening(date, time, -1) : date;
}
