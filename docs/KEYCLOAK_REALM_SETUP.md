# Настройка realm заказчика в Keycloak — пошаговая инструкция

> Как с нуля завести realm нового заказчика по схеме из
> [`ARCHITECTURE_MULTITENANT.md`](ARCHITECTURE_MULTITENANT.md): realm на
> заказчика, клиент на инстанс модуля, client-роли, группы терминалов.
> Рассчитано на разработчика, который делает это первый раз. Все пути
> указаны по админ-консоли Keycloak 26.
>
> Сквозной пример — заказчик **edc** с терминалами АЭ, ГУТ-2, УТ-1 и
> инстансами из таблицы архитектурного документа.

Перед началом держи под рукой:

- адрес Keycloak (дальше в примерах `https://auth.domain.ru`) и логин
  админа **master**-realm;
- таблицу инстансов заказчика (модуль + зона): из неё берутся все имена;
- адрес стенда заказчика (в примерах `https://edc.domain.ru`).

**Конвенция имён — закон.** Имя realm = имя заказчика, clientId = имя
инстанса. Всё в нижнем регистре, латиницей, разделитель — дефис. Регистр
критичен: realm `edc` и `EDC` — это разные URL, и второй даст 404 на
discovery.

---

## 1. Создать realm

1. Войди в админ-консоль под админом master.
2. Выпадающий список realm слева сверху → **Create realm**.
3. **Realm name**: `edc` (нижний регистр!). → **Create**.
4. Realm settings → вкладка **General**: Display name — человекочитаемое
   («ЕДЦ»), оно видно на форме логина.
5. Вкладка **Login**: выключи **User registration** (пользователей заводит
   только админ), включи **Forgot password**, если у заказчика будет почта.
6. Вкладка **Sessions/Tokens** — пока по умолчанию; времена жизни токенов
   крутим только по требованию безопасности, синхронно с настройками бэков.

Проверка: `https://auth.domain.ru/realms/edc/.well-known/openid-configuration`
должен отдать JSON, поле `issuer` = `https://auth.domain.ru/realms/edc`.
Этот issuer — ровно то, что попадёт в конфиги всех бэков заказчика.
Никогда не хардкодь остальные эндпоинты — они собираются из discovery.

## 2. Создать группы терминалов

Группы описывают, к какому терминалу пользователь имеет доступ **внутри
общих модулей** (dpport один на три терминала — см. §3.3 архитектурного
документа).

1. **Groups** → **Create group** → имя `terminals`.
2. Провались в `terminals` → **Create sub-group** — по одной на терминал:
   `ae`, `gut2`, `ut1`.

Имена подгрупп — нормализованные: латиница, без дефисов и точек
(`ГУТ-2` → `gut2`). Именно эти строки бэки увидят в claim `terminals`,
поэтому они должны совпадать с тем, что модуль ждёт в конфиге.

## 3. Завести клиента на каждый инстанс (браузерные модули)

Повторить для каждого инстанса из таблицы: `dpport`, `rtport-ae`,
`rtport-gut2`, `rtport-ut1`, `mpport-ut1`, `mpport-ae-gut2`.
Ниже — на примере `rtport-ut1`.

### 3.1. Сам клиент

1. **Clients** → **Create client**.
2. Шаг General settings: тип **OpenID Connect**, **Client ID** =
   `rtport-ut1` (= имя инстанса, буква в букву). Name — по вкусу. → Next.
3. Шаг Capability config:
   - **Client authentication: Off** (public-клиент — SPA не умеет хранить
     секрет);
   - **Standard flow: On** (это и есть Authorization Code);
   - **Direct access grants: Off** — ROPC на платформе упразднён;
   - остальное Off. → Next.
4. Шаг Login settings:
   - **Root URL**: `https://edc.domain.ru/rtport-ut1/`
   - **Valid redirect URIs**: `https://edc.domain.ru/rtport-ut1/*`
   - **Valid post logout redirect URIs**: `https://edc.domain.ru/rtport-ut1/*`
   - **Web origins**: `https://edc.domain.ru`
   → **Save**.

Никаких `*` на весь мир в redirect URI и origins — только конкретный хост
заказчика. Для локальной разработки добавь **отдельной строкой**
`http://localhost:4200/*` (и origin `http://localhost:4200`) — в
prod-realm'е этих строк быть не должно.

5. Вкладка **Advanced** → секция Advanced settings →
   **Proof Key for Code Exchange Code Challenge Method** = `S256`.
   Это включает обязательный PKCE — без него public-клиент уязвим к
   перехвату кода.

### 3.2. Роли клиента

Вкладка **Roles** (внутри клиента!) → **Create role**. Набор — по ролевой
модели модуля (для dpport см. [`ROLES.md`](ROLES.md)); типовой минимум:

- `administrator`
- `dispatcher`
- `viewer`

Важно: это **client-роли**, они существуют только внутри этого клиента.
Realm-роли (Realm roles в левом меню) для прав модулей **не используем** —
realm-роль одна на все модули и протекает между ними (см. раздел
«Обоснование» в архитектурном документе).

### 3.3. Мапперы токена (dedicated scope)

Вкладка **Client scopes** → строка `rtport-ut1-dedicated` → **Add mapper →
By configuration**. Нужны два маппера.

**а) Audience** — чтобы в токене поле `aud` содержало имя клиента, а бэк
мог проверить, что токен выписан именно ему:

| Поле | Значение |
|---|---|
| Mapper type | Audience |
| Name | `audience` |
| Included Client Audience | `rtport-ut1` (свой clientId) |
| Add to access token | On |

**б) Group Membership** — только для клиентов **мульти-терминальных**
инстансов (в примере edc это `dpport` и `mpport-ae-gut2`; для
одно-терминального `rtport-ut1` маппер не нужен — сам факт роли в клиенте
уже означает доступ к терминалу):

| Поле | Значение |
|---|---|
| Mapper type | Group Membership |
| Name | `terminals` |
| Token Claim Name | `terminals` |
| Full group path | **Off** (иначе в claim придёт `/terminals/ut1` вместо `ut1`) |
| Add to access token | On |

> Group Membership с Full path = Off кладёт в claim имена **всех** групп
> пользователя, включая родительскую `terminals`. Бэк должен фильтровать
> по известным ему кодам терминалов и игнорировать остальное.

### 3.4. Проверка клиента без запуска фронта

**Clients** → `rtport-ut1` → вкладка **Client scopes** → подвкладка
**Evaluate**: выбери тестового пользователя → **Generated access token**.
В JSON токена проверь:

- `aud` содержит `rtport-ut1`;
- `resource_access["rtport-ut1"].roles` — роли, которые ты выдал
  пользователю (§6);
- для мульти-терминальных клиентов есть `terminals: [...]`;
- `iss` = `https://auth.domain.ru/realms/edc`.

Это самый быстрый способ отладить мапперы — не нужен ни фронт, ни curl.

## 4. Клиент для rwgate (машинный, service account)

rwgate — провайдер, у него нет пользователей и формы логина. Бэки модулей
ходят в него по машинному токену (client credentials).

1. **Clients** → **Create client** → Client ID `rwgate`.
2. Capability config:
   - **Client authentication: On** (confidential — будет секрет);
   - **Standard flow: Off**, **Direct access grants: Off**;
   - **Service accounts roles: On**.
3. После создания: вкладка **Credentials** → скопируй **Client Secret** —
   он поедет в конфиг (через Vault/env, в git не попадает).
4. Если rwgate должен авторизовать *входящие* вызовы от модулей по ролям:
   заведи client-роли на `rwgate` (например `caller`) и выдай их
   service-account-пользователям клиентов-модулей (Clients → клиент модуля
   → Service account roles). На старте достаточно проверки `aud`.

Проверка из консоли:

```bash
curl -s -X POST "https://auth.domain.ru/realms/edc/protocol/openid-connect/token" \
  -d grant_type=client_credentials \
  -d client_id=rwgate \
  -d client_secret='<секрет из Credentials>'
```

Должен вернуться JSON с `access_token`.

## 5. Делегированный админ заказчика

Чтобы заказчик сам заводил своих пользователей и раздавал группы, не имея
доступа к настройкам клиентов и чужим realm:

1. Заведи пользователя (например `edc-admin`) — как в §6.
2. Users → `edc-admin` → **Role mapping** → **Assign role** → в фильтре
   выбери **Filter by clients** → найди роли клиента `realm-management`
   и выдай:
   - `manage-users` (создание/блокировка пользователей, сброс паролей,
     членство в группах),
   - `view-users`, `query-users`, `query-groups`.
3. **Не выдавай** `manage-clients`, `manage-realm`, `realm-admin` — это
   доступ к настройкам клиентов и мапперов, ломать их заказчик не должен.

`manage-users` позволяет и назначать роли — поэтому объясни админу
заказчика регламент: пользователям выдаются **группы** (терминалы) и
client-роли модулей, больше ничего.

## 6. Завести пользователя (и как это делает заказчик)

1. **Users** → **Create new user**: Username, Email, First/Last name →
   **Create**.
2. Вкладка **Credentials** → **Set password**; `Temporary: On` — при первом
   входе Keycloak заставит сменить пароль.
3. Вкладка **Groups** → **Join Group** → отметь терминалы пользователя
   (`terminals/ut1`, …).
4. Вкладка **Role mapping** → **Assign role** → **Filter by clients** →
   выдай роли в тех инстансах, где человек работает. Пример: диспетчер
   УТ-1 получает `rtport-ut1 → dispatcher`, `dpport → dispatcher`
   и группу `terminals/ut1`.

Итог для пользователя: один логин (SSO внутри realm), в каждом модуле —
свои права, в общих модулях виден только свой терминал.

## 7. Снять шаблон realm для следующих заказчиков

Когда realm первого заказчика отлажен, преврати его в шаблон:

1. **Realm settings** → меню **Action** (справа сверху) →
   **Partial export**: включи *Export groups and roles* и *Export clients*.
   Пользователи и секреты в partial export **не попадают** — это правильно.
2. Сохрани JSON как `realm-template.json` в
   `deployments/keycloak/import/` (каталог вне git — там же, где живут
   наши текущие импорты).
3. Новый заказчик = скопировать JSON, заменить в нём `edc` на имя нового
   заказчика и хост стенда, затем **Create realm → Browse** и указать файл
   (или положить в каталог импорта контейнера и перезапустить с
   `--import-realm`).
4. После импорта руками: пересоздать секрет `rwgate` (Credentials →
   Regenerate), завести делегированного админа (§5), сверить redirect URI.

Состав инстансов у заказчиков может отличаться — лишних клиентов после
импорта просто удали, недостающих добавь по §3.

## 8. Что прописать в конфигах модулей

После настройки realm в развёртывание инстанса (compose/env) уходят:

| Переменная | Откуда берётся | Пример |
|---|---|---|
| `KC_URL` (фронт) | адрес Keycloak | `https://auth.domain.ru` |
| `KC_REALM` (фронт) | имя realm | `edc` |
| `KC_CLIENT_ID` (фронт и бэк) | clientId инстанса | `rtport-ut1` |
| `KC_ISSUER` (бэк) | issuer из discovery | `https://auth.domain.ru/realms/edc` |

Бэк проверяет: подпись по JWKS issuer'а, `aud` содержит свой clientId,
роли — только из `resource_access["<clientId>"].roles`, для
мульти-терминальных — claim `terminals`.

## 9. Чек-лист «завести realm заказчика»

- [ ] Realm создан, имя в нижнем регистре, discovery отвечает.
- [ ] Группы `terminals/<код>` — по числу терминалов.
- [ ] Клиент на каждый браузерный инстанс: public, Standard flow, PKCE
      S256, redirect URI и origins — только хост заказчика.
- [ ] У каждого клиента: client-роли + audience-маппер;
      у мульти-терминальных — маппер `terminals` (Full path Off).
- [ ] Клиент `rwgate`: confidential, service accounts, секрет уехал в
      Vault/env.
- [ ] Делегированный админ: только `manage-users`/`view-users`/
      `query-users`/`query-groups` из `realm-management`.
- [ ] Тестовый пользователь заведён, токен проверен через
      Client scopes → Evaluate.
- [ ] Realm-шаблон переэкспортирован, если менялся состав
      клиентов/ролей/мапперов.

## 10. Типовые грабли

| Симптом | Причина |
|---|---|
| 404 на `.well-known/openid-configuration` | Регистр в имени realm: в URL он ровно такой, как Realm name |
| 400 `Invalid parameter: redirect_uri` на форме логина | Адреса стенда нет в Valid redirect URIs клиента (мы на это уже наступали — см. [`KEYCLOAK_HANDOVER.md`](KEYCLOAK_HANDOVER.md)) |
| «invalid token» на всех страницах при живом логине | `iss` в токене не совпал с issuer в конфиге бэка: сравнение дословное, вплоть до схемы и порта |
| CORS-ошибки в консоли браузера | Хост фронта не вписан в Web origins клиента |
| В claim `terminals` пути вида `/terminals/ut1` | В маппере Group Membership включён Full group path — выключи |
| Роли есть в админке, но нет в токене | Роли выданы не в том клиенте, либо смотришь `realm_access` вместо `resource_access["<clientId>"]` |
| Бэк отвергает токен: audience | Забыт audience-маппер, либо `verify_audience` ждёт другой clientId |
| У пользователя «нет прав» в одном из модулей при рабочем SSO | Роли в клиенте этого инстанса не выданы — SSO логинит, но права в каждом клиенте свои |
