// UAT — заготовка (IQPort §5). Заполнить, когда появится uat-контур.
// TODO: uat-домены Keycloak/бэкенда/портала.
import { apiBase } from './api-base';

export const environment = {
  production: true,
  moduleId: 'dpport',
  apiBaseUrl: apiBase(),
  portalUrl: '',
  keycloak: {
    url: 'https://uport1.ru',
    realm: 'iqport',
    clientId: 'iqport-dpport',
  },
};
