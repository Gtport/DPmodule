# Keycloak — развёртывание

Сервер авторизации DPmodule. Наружу не смотрит: слушает `127.0.0.1:8180`,
браузер ходит к нему через nginx (`location /realms/` → `:8180`), поэтому
логин идёт с того же origin, что и приложение — CORS не нужен.

## Что вне git

- **`.env`** — админские креды и публичный URL. Шаблон — `.env.example`.
- **`import/iqport-realm.json`** — экспорт реалма: внутри пользователи с
  credentials, коммитить нельзя. Держится на сервере рядом с compose;
  при разворачивании с нуля взять у владельца или выгрузить из живого
  Keycloak (`kc.sh export`).

Каталог `import/` монтируется read-only и читается только при старте
с флагом `--import-realm`; существующий реалм он не перезаписывает
(в логе `Realm 'iqport' already exists. Import skipped`).

## Запуск

```bash
cd deployments/keycloak
cp .env.example .env      # и подставить значения
mkdir -p import           # положить сюда iqport-realm.json
docker compose up -d
docker compose logs -f    # ждать «Keycloak … started»
```

Первый старт занимает ~1–2 минуты: Quarkus пересобирает конфигурацию
(`Updating the configuration and installing your custom providers`).
Проверка готовности:

```bash
curl -s http://localhost:8180/realms/iqport/.well-known/openid-configuration | jq .issuer
```

## ⚠️ Главная грабля: issuer

Keycloak без `KC_HOSTNAME` подставляет в claim `iss` тот хост и схему,
через которые пришёл запрос. Бэкенд сверяет issuer **точной строкой**, так что
любое расхождение — смена IP, домена, переход с http на https — роняет
авторизацию целиком: в UI «invalid token» на всех страницах, в логе бэкенда
`token has invalid claims: token has invalid issuer`. Токен при этом настоящий
и подписан верно.

`KC_HOSTNAME` эту зависимость снимает: issuer одинаков независимо от точки входа.

**Две строки, которые обязаны совпадать:**

| Где | Что |
|---|---|
| `deployments/keycloak/.env` | `KC_HOSTNAME=https://<домен>` |
| `config.yaml` бэкенда | `keycloak.issuer: https://<домен>/realms/iqport` |

При смене домена править обе, затем `docker compose up -d` здесь и
`systemctl --user restart dpmodule-backend`.

`keycloak.jwks_url` в `config.yaml` трогать не нужно — он внутренний
(`http://localhost:8180/...`) и от публичного адреса не зависит.

## Volume

Данные dev-режима (H2) и **ключи подписи токенов** лежат в volume `kc_data`
(полное имя — `<имя_проекта>_kc_data`, где имя проекта по умолчанию равно
имени каталога, то есть `keycloak_kc_data`). Если запускать compose из
каталога с другим именем, docker создаст **новый пустой volume**: реалм
уедет в импорт заново, ключи сменятся, все разлогинятся. Переносить
инсталляцию — вместе с volume либо через export/import реалма.

## Текущее состояние (2026-07-21)

На боевом сервере compose пока живёт **вне репозитория**, в `/home/alex/keycloak/`,
и именно он обслуживает `https://95850.koara.live`. Этот каталог — приведение
той же конфигурации к виду, годному для git (секреты вынесены в `.env`,
домен параметризован). Переключение сервера на запуск отсюда — отдельным шагом,
аккуратно, с сохранением volume `keycloak_kc_data`.
