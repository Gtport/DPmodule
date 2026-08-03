# Keycloak: ролевая модель через группы и починка входа

> Передача работы. Что изменено в realm `iqport` на нашем стенде, какая поломка входа
> найдена по дороге и что осталось решить перед переездом на корпоративный Keycloak.

| | |
|---|---|
| Дата | 3 августа 2026 |
| Realm | `iqport` |
| Стенд | VPS, `https://95850.koara.live` |
| Keycloak | 26.0, контейнер `keycloak`, `127.0.0.1:8180`, compose вне гита (`/home/alex/keycloak`) |

Смежные документы: [`docs/KEYCLOAK.md`](KEYCLOAK.md) (корпоративный realm), [`docs/ROLES.md`](ROLES.md) (ролевая модель).

---

## Коротко

- **Было сломано.** Вход из браузера не работал: в клиенте `iqport-dpport` не было redirect URI
  нашего домена. Починено.
- **Сделано.** Заведены четыре realm-роли `*_dpport` и одноимённые группы; роль висит на группе,
  пользователи — только в группах.
- **Сделано.** Прямые назначения `*_dpport` пользователям сняты, легаси-роли оставлены как страховка.
- **Осталось.** Три решения: `strict_roles`, адрес Keycloak в прод-сборке фронта, обновление
  боевого бэкенда. Ни одно не сделано — каждое меняет поведение боя.

### Границы работы

Трогали **только вход пользователей**. Исходящие запросы к провайдеру (дислокация АСУ, запрос 601,
памятки ГУ-45) как ходили по `X-API-Key`, так и ходят — блок `keycloak.service_account` в боевом
конфиге намеренно оставлен пустым. Почему это важно — раздел «Грабля на будущее».

---

## 1. Найденная поломка: вход из браузера

На `/protocol/openid-connect/auth` Keycloak отдавал **400 `Invalid parameter: redirect_uri`**.
В клиенте `iqport-dpport` были прописаны только `http://localhost:4200/*` и
`https://app.gtport.ru/*` — домен позапрошлого стенда. Нашего адреса в списке не было.

### Почему это не замечали

Все прошлые проверки авторизации шли через `curl` с `grant_type=password`, а у password grant
**нет параметра `redirect_uri`** — этот путь просто не задевался. Токен получался, роли были
верные, `/api` отвечал 200 — и залогиниться при этом было нельзя. Проверялся не тот путь,
которым ходит браузер.

### Проверка одной строкой

400 и «Invalid parameter» означают, что домена нет в списке:

```bash
curl -s -o /dev/null -w '%{http_code}\n' \
  "https://<домен>/realms/iqport/protocol/openid-connect/auth\
?client_id=iqport-dpport&response_type=code&scope=openid\
&redirect_uri=https%3A%2F%2F<домен>%2F"
```

### ⚠️ При смене домена правятся ТРИ места, а не два

К известной паре — `KC_HOSTNAME` в compose Keycloak и `keycloak.issuer` в `config.yaml` —
добавляется третье: **Valid redirect URIs клиента `iqport-dpport`**.

- `post.logout.redirect.uris` стоит в `+` (наследует redirect URIs) — отдельно править не нужно.
- `webOrigins` = `+`, CORS не мешает: Keycloak отдаётся тем же nginx, запросы same-origin.

---

## 2. Что теперь в realm

Модель — **«группа = профиль доступа»**, такая же, как в корпоративном realm'е: роль назначена
на группу, пользователь получает её по членству.

### Роли и группы

| Группа | Роль на группе | Участники | Кто это |
|---|---|---|---|
| `admin_dpport` | `admin_dpport` | admin, boss | Администратор системы |
| `operator_dpport` | `operator_dpport` | disp, operator | Оператор ГТ (правит данные) |
| `client_dpport` | `client_dpport` | client | Клиент (просмотр) |
| `client_dispatcher_dpport` | `client_dispatcher_dpport` | — пусто | Диспетчер клиента |

Группа `client_dispatcher_dpport` пустая — подходящего пользователя в realm'е нет.
Заводить, когда появится клиент-диспетчер.

### Назначения у пользователей

Прямые назначения `*_dpport` **сняты** — иначе двойная бухгалтерия, и при переезде не видно,
откуда у человека права. Легаси-роли оставлены прямыми: страховка до включения `strict_roles`,
снимать их сейчас нельзя.

| Пользователь | Прямые роли (легаси) | Группа | Роли в токене |
|---|---|---|---|
| `boss` | `administrator` | `admin_dpport` | `administrator`, `admin_dpport` |
| `admin` | `administrator` | `admin_dpport` | `administrator`, `admin_dpport` |
| `disp` | `dispatcher` | `operator_dpport` | `dispatcher`, `operator_dpport` |
| `operator` | `operator` | `operator_dpport` | `operator`, `operator_dpport` |
| `client` | `client` | `client_dpport` | `client`, `client_dpport` |

Что роль действительно приходит *из группы*, а не из остатков прямого назначения, проверено
прицельно: у `boss` и `disp` прямых `*_dpport` нет, а в `realm_access.roles` они есть.

---

## 3. Как воспроизвести на другом стенде

Всё сделано через Admin REST API. Последовательность пригодна и для корпоративного Keycloak.
Учётные данные админа сюда намеренно не вынесены — подставь свои.

```bash
KC=http://localhost:8180
REALM=iqport
TOK=$(curl -s $KC/realms/master/protocol/openid-connect/token \
  -d grant_type=password -d client_id=admin-cli \
  -d username="$KC_ADMIN" -d password="$KC_ADMIN_PASSWORD" \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["access_token"])')

ROLES="admin_dpport operator_dpport client_dispatcher_dpport client_dpport"

# 1. realm-роли
for r in $ROLES; do
  curl -s -X POST -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
    -d "{\"name\":\"$r\"}" $KC/admin/realms/$REALM/roles
done

# 2. одноимённые группы
for g in $ROLES; do
  curl -s -X POST -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
    -d "{\"name\":\"$g\"}" $KC/admin/realms/$REALM/groups
done

# 3. роль -> группа
for g in $ROLES; do
  gid=$(curl -s -H "Authorization: Bearer $TOK" \
    "$KC/admin/realms/$REALM/groups?search=$g&exact=true" \
    | python3 -c 'import sys,json;print(json.load(sys.stdin)[0]["id"])')
  rep=$(curl -s -H "Authorization: Bearer $TOK" $KC/admin/realms/$REALM/roles/$g)
  curl -s -X POST -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
    -d "[$rep]" $KC/admin/realms/$REALM/groups/$gid/role-mappings/realm
done

# 4. пользователь -> группа (PUT, без тела)
curl -s -X PUT -H "Authorization: Bearer $TOK" \
  $KC/admin/realms/$REALM/users/$uid/groups/$gid

# 5. снять дублирующее прямое назначение
curl -s -X DELETE -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d "[$rep]" $KC/admin/realms/$REALM/users/$uid/role-mappings/realm
```

**Redirect URI** добавляется иначе: забрать представление клиента целиком, дописать элемент в
`redirectUris`, вернуть через `PUT`. Admin API заменяет представление — частичный PATCH тут
не сработает.

---

## 4. Проверка после изменений

| Что проверяем | Ожидаем | Факт |
|---|---|---|
| Страница входа с нашим redirect URI | 200 | 200 |
| `/api/v1/me` без токена | 401 | 401 |
| `/api/v1/me` с токеном `boss` | 200 | 200 |
| `iss` в токене | `https://95850.koara.live/realms/iqport` | совпал |
| `aud` в токене | `iqport-backend` | совпал |
| Роль приходит из группы | да | да |

---

## 5. Что осталось — три решения

### 5.1. Включить `keycloak.strict_roles: true`

Закрывает дыру: в общем realm'е контура голые `admin` / `operator` / `dispatcher` — легаси-роли
*всего* контура, они могут принадлежать пользователю чужого приложения (у FPPort своих ролей нет
вовсе). Безусловная нормализация выдавала такому человеку права оператора DPPort.

Теперь включать безопасно: канонические роли есть у всех через группы, состав сверен по всем
пятерым пользователям — без прав никто не останется. После включения легаси-роли можно снимать.

### 5.2. Адрес Keycloak в прод-сборке фронта — мина

В `environment.production.ts`, `environment.ts` и `environment.uat.ts` стоит
`keycloak.url: 'https://uport1.ru'` — это внешний Keycloak контура.

Наш стенд жив только потому, что фронт крутится как `ng serve --configuration development`,
а там `url: ''` (same-origin). **Прод-сборка фронта на нашем VPS сломает вход** — уведёт логин
на чужой Keycloak, который к тому же отвечает 403 по гео-блокировке.

### 5.3. Обновить боевой бэкенд

Рабочая копия `/home/alex/projects/DPmodule` отстаёт от коммита с новой ролевой моделью,
работающий бинарь собран ещё раньше. Обновление теперь безопасно: легаси-роли на месте,
поэтому и старый, и новый код видят у пользователей нужные права.

---

## 6. Грабля на будущее: исходящие запросы к провайдеру

> ⚠️ **Не заполнять `keycloak.service_account` «просто чтобы было».**

Заполненность этого блока — **это и есть переключатель режима**. У `data_source.asu.auth_mode`
в базе пусто, у `reference.auth_mode` в конфиге тоже, а пустое значение означает
«keycloak, если сервис-аккаунт настроен».

То есть в момент заполнения блока дислокация и памятки молча перестанут ходить по `X-API-Key`
и уйдут на Bearer. Если провайдер к этому не готов — встанет забор дислокации.
Клиент `iqport_dpport_service` в корпоративном realm'е на сегодня не заведён.

---

## 7. Про переезд на корпоративный Keycloak

Поправка к замыслу, чтобы не было сюрприза. В корпоративном realm'е группы называются иначе
и лежат под `profiles/`: `profiles/dpport-admin`, `profiles/dpport-operator`, `profiles/client`,
`profiles/client-dispatcher`. У нас группы плоские и названы по именам ролей.

**На переезд это не влияет.** Контракт, который читает код, — это имена ролей в
`realm_access.roles`, а не имена групп. Роли у нас теперь называются ровно так же, как в
корпоративном realm'е. Группы там уже заведены и ведутся не нами — от нас потребуется только
сказать, кого в какой профиль положить.

Что сверить на стороне корпоративного Keycloak:

- **Redirect URIs и Web origins** боевого и дев-домена в клиенте `iqport-dpport` — по нашему
  опыту это первое, что забывают.
- **`iss` корпоративного realm'а** против `keycloak.issuer` в конфиге — сравнение дословное,
  схема `http`/`https` тоже часть строки.
- **Состав групп** `profiles/dpport-admin` и `profiles/client*` — подтверждён только
  `dpport-operator`.
- **Клиент `iqport_dpport_service`** под исходящие вызовы — когда до них дойдёт очередь.
  Обрати внимание на разделитель: у сервисного клиента подчёркивания, у остальных дефисы.

---

Мелочь на потом: в redirect URIs клиента остался `https://app.gtport.ru/*` — домен позапрошлого
стенда. Вреда не делает, но при уборке realm'а его стоит убрать.
