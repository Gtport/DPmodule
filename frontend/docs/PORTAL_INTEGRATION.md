# Стыковка DPPort с порталом IQPort

Портал (`iqport/portal`, локально `D:\portal\portal`) — лаунчер платформы: плитки
модулей, единый вход, переход в модуль по клику. DPPort — один из модулей.
Здесь: что уже сделано на нашей стороне, что осталось сделать **в портале и
Keycloak** (эти репозитории мы не трогаем), и как проверить связку.

## 1. Что сделано в этом репозитории

| Файл | Что |
|---|---|
| `src/app/core/config/modules.config.ts` | Каталог приведён к портальному: поле `available`, роли `view_*`/`edit_*`, у dpport — четыре роли `*_dpport` |
| `src/app/layout/module-switcher/module-switcher.component.ts` | Скрывает неразвёрнутые модули (`available: false`), первым пунктом — «Портал — все модули» |
| `src/environments/*.ts` | Новое поле `portalUrl`; пустая строка = пункт «Портал» скрыт |

**Зачем менялись роли.** В каталоге стояли легаси-имена `operator` / `admin`, а
`normalizeRole()` (`core/auth/roles.ts`) переводит их в `operator_dpport` /
`admin_dpport`. Из-за этого оператору DPPort в свитчере показывались чужие
модули (MPPort, ССПР, SPPort) — ролей на них у него нет, переход упирался бы в
403. Теперь роли соседей записаны их настоящими именами, и проверка честная.

**Каталог модулей дублируется** в портале и в каждом модуле — общей библиотеки на
платформе нет. Владелец списка — портал; при изменениях там переносить сюда.

## 2. Что сделать в портале (`iqport/portal`)

### 2.1 Включить плитку DPPort — обязательно

Сейчас `src/app/core/config/modules.config.ts` в портале:

```ts
{
  id: 'dpport',
  ...
  url: 'https://dpport.iqport.ru',
  available: false,          // ← плитка серая, «Недоступно», в модуль не зайти
}
```

Нужно `available: true` и реальный адрес стенда. Роли у плитки уже правильные
(`admin_dpport`, `operator_dpport`, `client_dispatcher_dpport`, `client_dpport`)
— пункт «Портал: заменить у плитки dpport роли…» из `docs/keycloak-access-model.md`
закрыт, чек-бокс там можно снять.

Адрес модуля по коду развёртывания (`Dockerfile`, `nginx.conf`, `back/backend/config.yaml`):

| Что | Значение |
|---|---|
| Хост стенда | `176.53.160.9` |
| Фронт DPPort | порт `8581` (наружу через ingress торчит только он) |
| Метрики фронта | `9581` |
| Бэкенд DPPort | `8580`, метрики `9580` |

То есть до появления DNS-имени — `http://176.53.160.9:8581`, после —
`https://dpport.iqport.ru`. Какой из двух ставить в портал, решает DevOps: домена
`dpport.iqport.ru` в конфигах пока нет нигде, кроме самого каталога портала.

### 2.2 Адрес самого портала — уточнить

В коде портала своего URL нет (лаунчер себя в каталоге не перечисляет), поэтому
`portalUrl` в наших environment-файлах проставлен **по конвенции** —
`https://portal.iqport.ru`, с пометкой TODO. Портальный nginx слушает `8551`,
его `docker-compose.yml` публикует `80:8551`. Как узнаем боевой адрес — поправить
`src/environments/environment.ts` и `environment.production.ts` (и `uat`, там
пока пусто).

## 3. Единый вход (SSO) — где ломается

SSO между порталом и модулем работает, только если оба ходят в **один и тот же
origin Keycloak**: сессионная кука лежит на домене Keycloak, а не на домене SPA.

| Сборка | `keycloak.url` | SSO с порталом |
|---|---|---|
| `production` / `uat` | `https://uport1.ru` | да — совпадает с порталом |
| `development` | `''` (относительный `/realms/…` через nginx на `:8180`) | **нет** — это локальный Keycloak стенда, у него своя сессия |

Пустой `url` в dev — осознанное решение (свой Keycloak на стенде, same-origin,
без CORS и без пересборки под домен; разбор — `Dockerfile`, комментарий про
`NG_CONFIG`). Поэтому: **связку портал↔dpport проверять на production-сборке**,
на dev-стенде со своим Keycloak единого входа не будет by design.

Бэкенд DPPort сверяет токен дословно (`back/backend/config.yaml`):
issuer `https://uport1.ru/realms/iqport`, audience `iqport-dpport` — подменять
адрес Keycloak на IP нельзя, токен не пройдёт.

## 4. Что нужно в Keycloak (админка realm `iqport`)

Для клиента `iqport-dpport` — иначе переход с портала упрётся в ошибку входа:

1. **Valid redirect URIs** — адрес модуля из §2.1 (`http://176.53.160.9:8581/*`
   и/или `https://dpport.iqport.ru/*`). Ровно эта настройка уже кусала: сломанный
   redirect_uri не замечали, пока вход шёл через password grant.
2. **Web origins** — тот же адрес (или `+`).
3. **Audience mapper** — Included Client Audience = `iqport-dpport`, в access-token.
   Бэкенд проверяет `aud`.
4. **Full scope allowed = OFF** — иначе в токене приедут роли всех модулей realm'а.
5. **Group Membership mapper** (`groups`, Full group path = ON) — нужен rtport/sspr;
   dpport группы не читает, но mapper общий для realm'а.

Модель ролей и групп целиком — `D:\portal\portal\docs\keycloak-access-model.md`.

## 5. Как проверить связку

1. В портале включить плитку (§2.1), собрать: `npm run build`, поднять
   `docker compose up` (порт 80 → 8551).
2. DPPort собрать **production**-конфигом (dev смотрит в локальный Keycloak):
   `docker build --build-arg NG_CONFIG=production`, поднять на `8581`.
3. Войти в портал под пользователем с ролью `operator_dpport` (профиль
   `/profiles/dpport-operator`).
4. Плитка DPPort активна → клик → модуль открывается **без повторного логина**.
5. В навбаре DPPort «Модули» → «Портал — все модули» → возврат, тоже без логина.
6. Под `client_dpport`: DPPort открывается, кнопки действий скрыты (порог `OPER`),
   плитки rtport/sspr в свитчере не показываются.

Локально, без докера: портал `npm start -- --port 4201` в `D:\portal\portal`,
DPPort `npm start` (4200). Единого входа при этом не будет (см. §3) — проверяется
только навигация и фильтрация по ролям.
