/**
 * Единая точка загрузки `xlsx-js-style` для клиентских выгрузок в Excel.
 *
 * Пакет — CommonJS (UMD, `main: dist/xlsx.min.js`), поэтому при динамическом
 * `import()` бандлер кладёт его экспорты в поле `default`, а не в корень
 * namespace-объекта. Прямое `const XLSX = await import('xlsx-js-style')`
 * оставляет `XLSX.utils` неопределённым, и выгрузка падает на первом же вызове
 * («Cannot read properties of undefined (reading 'book_new')») — молча, потому
 * что ошибка уходит в консоль, а кнопка просто «не работает» (жалоба владельца
 * 31.08.2026: кнопка скачивания в «Истории прибывших»).
 *
 * Фолбэк на сам модуль оставлен намеренно: распознает ли сборщик именованные
 * экспорты CommonJS, зависит от его версии и анализа файла, и от сборки к
 * сборке это меняется. Хелпер работает в обоих случаях, поэтому загружать
 * библиотеку напрямую в компонентах больше не нужно.
 */
export async function loadXlsx(): Promise<typeof import('xlsx-js-style')> {
  const mod = await import('xlsx-js-style');
  const cjs = (mod as { default?: typeof import('xlsx-js-style') }).default;
  return cjs ?? mod;
}
