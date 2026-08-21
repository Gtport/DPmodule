// Путь к API этого деплоя, вычисленный от <base href> страницы.
//
// При base href «/» (все нынешние стенды) даёт прежний '/api'. Под
// path-адресацией (ma.domen.com/dpport/ → контейнер фронта подставляет
// <base href="/dpport/"> на старте, см. frontend/nginx.conf) даёт
// '/dpport/api' — входной nginx хоста срежет префикс и донесёт запрос
// до нашего контейнера как /api.
//
// Путь намеренно АБСОЛЮТНЫЙ, а не относительный 'api': значение уходит
// в HttpClient, в шаблон тайлов Leaflet и в условие Bearer-интерцептора
// (app.config.ts) — относительную строку каждый потребитель резолвил бы
// по-своему, абсолютную все понимают одинаково.
export function apiBase(): string {
  return new URL('api', document.baseURI).pathname.replace(/\/$/, '');
}
