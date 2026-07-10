export const environment = {
  production: false,
  moduleId: 'dpport',
  apiBaseUrl: '/api',
  keycloak: {
    // Keycloak выведен наружу через nginx на том же домене (location /realms/ → :8180),
    // поэтому URL совпадает с origin приложения — запросы same-origin, без CORS.
    // Пустая строка = относительный путь /realms/... → работает на любом origin
    // (IP, 95850.koara.live) без пересборки. Не хардкодим конкретный домен.
    url: '',
    realm: 'iqport',
    clientId: 'iqport-dpport',
  },
};
