/**
 * Даты в поясе Москвы — единый источник для экранов.
 *
 * Зачем: `new Date().toISOString()` отдаёт UTC, и в поясе восточнее Гринвича
 * (МСК = UTC+3, сервер CEST = UTC+2) на границе суток съезжает на день назад —
 * «вчера» превращается в «позавчера». Тут дата берётся строго по Москве, а
 * сдвиг на N суток считается с якорем в полдень UTC (границу суток не пересекает).
 */

/** Сегодня по Москве (yyyy-MM-dd), независимо от пояса браузера. */
export function todayMsk(): string {
  return new Date().toLocaleString('sv-SE', { timeZone: 'Europe/Moscow' }).slice(0, 10);
}

/** Дата ± N суток от yyyy-MM-dd (якорь — полдень UTC, без сноса на границе суток). */
export function addDaysIso(date: string, days: number): string {
  const d = new Date(date + 'T12:00:00Z');
  d.setUTCDate(d.getUTCDate() + days);
  return d.toISOString().slice(0, 10);
}

/** Вчера по Москве (учётный лист закрывают на следующий день). */
export function yesterdayMsk(): string {
  return addDaysIso(todayMsk(), -1);
}
