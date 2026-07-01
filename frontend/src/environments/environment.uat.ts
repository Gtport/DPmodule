// UAT — заготовка (IQPort §5). Заполнить, когда появится uat-контур.
// TODO: uat-домены Keycloak/бэкенда.
export const environment = {
  production: true,
  moduleId: 'dpport',
  apiBaseUrl: '/api',
  keycloak: {
    url: 'https://uport1.ru',
    realm: 'iqport',
    clientId: 'iqport-dpport',
  },
};
