/**
 * Цвета терминалов из реестра ports подобраны как заливки маркеров и строк —
 * среди них бывают бледные (жёлтый УТ-1), и ТЕКСТ таким цветом на белом фоне
 * не читается (жалоба владельца 13.08.2026: примечания «Подхода»). Хелпер
 * затемняет слишком светлый цвет до контраста ≥ 4.5:1 к белому (норма
 * читаемости основного текста), сохраняя оттенок; тёмные цвета не трогает.
 * Для заливок (точки, бейджи, строки) — использовать исходный цвет реестра.
 */

/** Относительная яркость sRGB-канала (0..255) по формуле WCAG. */
function channelLum(c: number): number {
  const s = c / 255;
  return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
}

function luminance(r: number, g: number, b: number): number {
  return 0.2126 * channelLum(r) + 0.7152 * channelLum(g) + 0.0722 * channelLum(b);
}

/** «#RGB»/«#RRGGBB» → [r,g,b]; иной формат — null (цвет отдаём как есть). */
function parseHex(hex: string): [number, number, number] | null {
  const m = /^#([0-9a-f]{3}|[0-9a-f]{6})$/i.exec(hex.trim());
  if (!m) return null;
  const h = m[1].length === 3 ? m[1].split('').map((c) => c + c).join('') : m[1];
  return [parseInt(h.slice(0, 2), 16), parseInt(h.slice(2, 4), 16), parseInt(h.slice(4, 6), 16)];
}

/**
 * Цвет для ТЕКСТА на белом фоне: бледный затемняется до контраста ≥ 4.5:1
 * (яркость ≤ 0.1833), тёмный возвращается без изменений. Не-hex (имя цвета,
 * пусто) — как есть: затемнять нечем, а ломать значение нельзя.
 */
export function readableTextColor(color: string | null): string | null {
  if (!color) return color;
  const rgb = parseHex(color);
  if (!rgb) return color;
  let [r, g, b] = rgb;
  // Контраст к белому = 1.05 / (L + 0.05) ≥ 4.5  ⇔  L ≤ 1.05/4.5 − 0.05.
  const maxLum = 1.05 / 4.5 - 0.05;
  for (let i = 0; i < 40 && luminance(r, g, b) > maxLum; i++) {
    r *= 0.93; g *= 0.93; b *= 0.93;
  }
  const hx = (v: number) => Math.round(v).toString(16).padStart(2, '0');
  return `#${hx(r)}${hx(g)}${hx(b)}`;
}
