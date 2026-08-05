export const environment = {
  production: false,
  moduleId: 'dpport',
  apiBaseUrl: '/api',
  // Портала на этой машине нет — пункт «Портал» в свитчере скрыт. Если поднять
  // клон iqport/portal локально (`npm start -- --port 4201`, 4200 занят модулем),
  // вписать 'http://localhost:4201'.
  portalUrl: '',
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
