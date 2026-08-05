export const environment = {
  production: true,
  moduleId: 'dpport',
  apiBaseUrl: '/api',
  // ⚠️ TODO подтвердить у DevOps (см. environment.ts).
  portalUrl: 'https://portal.iqport.ru',
  keycloak: {
    url: 'https://uport1.ru',
    realm: 'iqport',
    clientId: 'iqport-dpport',
  },
};
