export const environment = {
  production: false,
  moduleId: 'dpport',
  apiBaseUrl: '/api',
  keycloak: {
    // Keycloak выведен наружу через nginx на том же домене (location /realms/ → :8180),
    // поэтому URL совпадает с origin приложения — запросы same-origin, без CORS.
    url: 'https://app.gtport.ru',
    realm: 'iqport',
    clientId: 'iqport-dpport',
  },
};
