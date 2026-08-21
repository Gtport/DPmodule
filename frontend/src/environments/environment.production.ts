import { apiBase } from './api-base';

export const environment = {
  production: true,
  moduleId: 'dpport',
  apiBaseUrl: apiBase(),
  // ⚠️ TODO подтвердить у DevOps (см. environment.ts).
  portalUrl: 'https://portal.iqport.ru',
  keycloak: {
    url: 'https://uport1.ru',
    realm: 'iqport',
    clientId: 'iqport-dpport',
  },
};
