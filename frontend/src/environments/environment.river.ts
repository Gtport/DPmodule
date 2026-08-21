import { apiBase } from './api-base';

export const environment = {
  production: false,
  moduleId: 'dpport',
  apiBaseUrl: apiBase(),
  portalUrl: '',
  keycloak: {
    // Второй инстанс VPS (клиент river, свой домен): Keycloak ОБЩИЙ с боевым
    // инстансом (решение владельца 12.08.2026 — один realm iqport, одни
    // пользователи, до настоящей мультитенантной раскатки). KC_HOSTNAME
    // боевого Keycloak зашит на 95850.koara.live, поэтому same-origin
    // ('' как в development) на другом домене не работает — адрес абсолютный.
    // Валидные redirect URI домена инстанса должны быть прописаны у клиента
    // iqport-dpport (иначе 400 Invalid parameter: redirect_uri).
    url: 'https://95850.koara.live',
    realm: 'iqport',
    clientId: 'iqport-dpport',
  },
};
