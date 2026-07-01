// Базовый (= production) конфиг. Подменяется на dev через fileReplacements в angular.json.
// Значения секретов/доменов на деплое — из CI/Vault (IQPort §6.2), не хардкодить здесь.
export const environment = {
  production: true,

  // ID модуля этого приложения. Меняется при форке шаблона под конкретный модуль.
  moduleId: 'dpport',

  // Базовый URL Go-бэкенда модуля (через API Gateway). Bearer вешается только на него.
  apiBaseUrl: '/api',

  // Keycloak (realm iqport). clientId = iqport-<moduleId>.
  keycloak: {
    url: 'https://uport1.ru',
    realm: 'iqport',
    clientId: 'iqport-dpport',
  },
};
