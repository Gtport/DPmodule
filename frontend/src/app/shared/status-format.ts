/**
 * Общие правила показа актуальности для статус-панелей: подписи, цвет чипа по
 * возрасту метки, форматы времени и часы МСК/ЖД. Один источник на обе панели —
 * вертикальную карточку главного экрана (`system-status-card`) и горизонтальную
 * полосу «Плана подвода» (`plan-status-panel`), чтобы пороги и форматы не
 * разъезжались.
 */

/**
 * КОРОТКИЕ подписи станций плана для узких статус-панелей. Полные имена станций
 * приходят с бэка (`/settings/ui` → plan_stations, из plan_profile) и стоят на
 * вкладках экрана «План подвода»; сюда они не годятся — «МЫС АСТАФЬЕВА» в
 * колонку карточки не влезает. Незнакомый код показывается как есть
 * (planLabel), поэтому новому клиенту строка здесь не обязательна.
 */
export const PLAN_LABELS: Record<string, string> = { ma: 'ПП Мыс', nk: 'ПП Находка' };

/** Способ обновления дислокации: как он называется для пользователя. */
export const SOURCE_LABELS: Record<string, string> = {
  lk: 'ЛК', json: 'АСУ', asu: 'АСУ', lk_robot: 'ЛК авто',
};

/** Пороги свежести (мин): дислокация — часы, планы — сутки (эталон gtport). */
export const DISL_AGE = { warn: 60, danger: 180 };
export const PLAN_AGE = { warn: 720, danger: 1440 };

export function sourceLabel(s: string): string {
  return SOURCE_LABELS[s] ?? s ?? '—';
}

/**
 * Режим источника провайдера rwgate (ручка /dislocation/status → provider):
 * пара «дислокация-история» словами владельца. asu — штатно («АСУ-АСУ»),
 * lk — провайдер добывает данные роботом ЛК РЖД («АСУ-ЛК», автозапросы истории
 * у нас приостановлены), paused — у провайдера лежат оба источника,
 * unknown/прочее — провайдер не ответил или ответ не разобран.
 */
export function providerModeLabel(src: string): string {
  switch (src) {
    case 'asu': return 'АСУ-АСУ';
    case 'lk': return 'АСУ-ЛК';
    case 'paused': return 'ПАУЗА';
    default: return 'нет ответа';
  }
}

/** Цвет чипа режима провайдера: штатно — зелёный, фолбэк — оранжевый, беда — красный. */
export function providerModeColor(src: string): string {
  switch (src) {
    case 'asu': return 'green';
    case 'lk': return 'orange';
    default: return 'red';
  }
}

export function planLabel(code: string): string {
  return PLAN_LABELS[code] ?? code.toUpperCase();
}

/** Цвет чипа по возрасту (мин): ≤warn — синий, ≤danger — оранжевый, иначе красный. */
export function ageColor(age: number, warn: number, danger: number): string {
  if (age <= warn) return 'blue';
  if (age <= danger) return 'orange';
  return 'red';
}

/** «2026-07-12T08:10:00» → «12.07 08:10»; пусто → «--.-- --:--». */
export function fmtStamp(ts: string | null): string {
  if (!ts || ts.length < 16) return '--.-- --:--';
  return `${ts.slice(8, 10)}.${ts.slice(5, 7)} ${ts.slice(11, 16)}`;
}

/** Компоненты времени МСК «ГГГГ-ММ-ДД ЧЧ:ММ:СС» независимо от пояса браузера. */
function mskString(now: Date): string {
  return now.toLocaleString('sv-SE', { timeZone: 'Europe/Moscow' });
}

/** Текущее время МСК: «дд.мм чч:мм». */
export function nowMsk(now: Date): string {
  const s = mskString(now);
  return `${s.slice(8, 10)}.${s.slice(5, 7)} ${s.slice(11, 16)}`;
}

/** ЖД-время: те же чч:мм, но при часе МСК ≥ 18 дата +1 (операционные ЖД-сутки). */
export function nowJd(now: Date): string {
  const s = mskString(now);
  const d = new Date(Date.UTC(+s.slice(0, 4), +s.slice(5, 7) - 1, +s.slice(8, 10)));
  if (+s.slice(11, 13) >= 18) d.setUTCDate(d.getUTCDate() + 1);
  const dd = String(d.getUTCDate()).padStart(2, '0');
  const mm = String(d.getUTCMonth() + 1).padStart(2, '0');
  return `${dd}.${mm} ${s.slice(11, 16)}`;
}
