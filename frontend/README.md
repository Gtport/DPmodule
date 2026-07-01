# IQPort — Frontend Template (модуль)

Базовый шаблон Angular-приложения под модуль платформы IQPort (§7.2 архдока).
Каждый новый модуль = форк этого репозитория с заменой нескольких значений.

## Стек

- **Angular 21** — standalone-компоненты, signals, application builder
- **ng-zorro-antd 21** (Angular-порт Ant Design) — единая компонент-библиотека
- **Дизайн-токены** `src/styles/tokens.css` — единый источник стилей платформы
- **keycloak-js 26** — Authorization Code + PKCE (public client, без секретов в браузере)
- Единая Keycloak-сессия realm `iqport` → SSO между всеми 7 модулями

## Дизайн-система (единый стиль на каждом форке)

Правила — в `/Users/mac/coding/IQPort_Design_Rules.md` (на основе dpport). Реализация:

1. **`src/styles/tokens.css`** — все токены (цвета/типографика/spacing/радиусы/тени/z-index/layout + dark). **Единственный источник истины.** Модули НЕ переопределяют токены и НЕ хардкодят цвета — только `var(--token)`.
2. **`src/styles.less`** — тема ng-zorro задаётся через Less-переменные (`@primary-color` и т.д.) на тех же значениях, **не** CSS-оверрайдами `.ant-*` (Design Rules §9.4).
3. **stylelint-guardrail** (`.stylelintrc.json`): `npm run lint:style` падает на хардкоде hex/rgb/named-цветов вне `tokens.css`/`styles.less`. Это то, что не даёт стилям разъехаться на форках (как было в dpport). Подключить в CI.

## Структура

```
src/app/
  core/
    auth/        keycloak.service · auth.guard (RBAC) · auth.interceptor (Bearer на /api)
    config/      modules.config.ts — каталог модулей для module-switcher
  layout/
    shell/             navbar + collapsible sidebar + content + footer
    module-switcher/   переход в другие модули (по ролям)
  features/      экраны модуля (lazy-loaded)
  pages/         forbidden (403)
src/environments/  environment(.development|.production).ts
```

## Запуск локально

```bash
npm install
npm start        # http://localhost:4200 → редирект на Keycloak, вход (demo/demo)
```

## Форк под новый модуль

Поменять в **трёх** environment-файлах (`src/environments/*`):

| Поле | Значение |
|------|----------|
| `moduleId` | `dpport` / `rtport` / `mpport` / … |
| `keycloak.clientId` | `iqport-<moduleId>` (клиент уже заведён в realm) |
| `apiBaseUrl` | URL Go-бэкенда модуля |

Каталог модулей (для switcher) — `src/app/core/config/modules.config.ts`.

## Keycloak

Realm `iqport` импортируется из [`keycloak/realm-iqport.json`](keycloak/realm-iqport.json):
4 роли (`operator/dispatcher/manager/admin`), public-клиенты на каждый модуль,
access-token 15 мин, SSO-сессия 8 ч. Для prod убрать demo-пользователя из realm-файла.

## RBAC

Гвард читает роль из `route.data.roles`:

```ts
{ path: 'admin', canActivate: [authGuard], data: { roles: ['admin'] } }
```

## Деплой

`Dockerfile` (multi-stage node→nginx) + `nginx.conf` (SPA-fallback + проксирование `/api`).
CI — `.gitlab-ci.yml` (SAST + Secret Detection включены; добавить lint/test/build образа).

> Секреты и реальные домены — из CI/Vault (§6.2), не коммитить в репозиторий.
