// Базовый (= production) конфиг. Подменяется на dev через fileReplacements в angular.json.
// Значения секретов/доменов на деплое — из CI/Vault (IQPort §6.2), не хардкодить здесь.
import { apiBase } from './api-base';

export const environment = {
  production: true,

  // ID модуля этого приложения. Меняется при форке шаблона под конкретный модуль.
  moduleId: 'dpport',

  // Базовый URL Go-бэкенда модуля (через API Gateway). Bearer вешается только на него.
  // Считается от <base href> (см. api-base.ts): '/api' на прежних стендах,
  // '/dpport/api' под path-адресацией ma.domen.com/dpport/.
  apiBaseUrl: apiBase(),

  // Портал-лаунчер платформы (репозиторий iqport/portal): куда ведёт пункт
  // «Портал» в переключателе модулей. Пустая строка — пункт скрыт.
  // ⚠️ TODO подтвердить у DevOps: адрес взят по конвенции <модуль>.iqport.ru,
  // в коде портала своего URL нет (docs/PORTAL_INTEGRATION.md §2).
  portalUrl: 'https://portal.iqport.ru',

  // Keycloak (realm iqport). clientId = iqport-<moduleId>.
  keycloak: {
    url: 'https://uport1.ru',
    realm: 'iqport',
    clientId: 'iqport-dpport',
  },
};
