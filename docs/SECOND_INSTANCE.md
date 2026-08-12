# Второй инстанс на VPS: новый клиент (речной порт)

Задание для Claude Code **на VPS**. Ветка кода — `multi-tenant`.

Цель: поднять рядом с боевым приложением второй инстанс того же репозитория со
своей базой, настроить нового клиента таблицами и получить контур «одна раскатка
обновляет оба приложения». Боевой инстанс работает непрерывно; все шаги ниже
аддитивны — ни один боевой файл, юнит или контейнер не редактируется.

## Что уже сделано в коде (ветка `multi-tenant`, запушена)

- Миграции очищены от данных боевого клиента; клиентские сиды — `scripts/clients/`.
- `config.river.yaml` — конфиг второго инстанса.
- `environment.river.ts` + конфигурация `river` в `angular.json` — фронт второго
  инстанса (Keycloak абсолютным адресом).
- Станции плана подвода приходят с бэка; у клиента без плана пункт меню «План
  подвода» скрывается сам.

## Раскладка (что чем отличается)

| | Боевой («3 порта») | Второй (речной порт, `river`) |
|---|---|---|
| Каталог | `/home/alex/projects/DPmodule` | `/home/alex/projects/DPmodule2` |
| Ветка | `main` | `multi-tenant` |
| Юниты | `dpmodule-backend` / `dpmodule-frontend` | `dpmodule2-backend` / `dpmodule2-frontend` |
| Конфиг | `config.vps.yaml` | `config.river.yaml` |
| Порты | 8080 / 9090 / 4200 | 8081 / 9091 / 4201 |
| База | `dpport` | `dpport_river` (схема в обеих — `dpport`) |
| Фронт | `ng serve` (development) | `ng serve --configuration river` |
| Домен | `95850.koara.live` | `<домен2>` — даёт владелец |

Keycloak **один на оба** (контейнер боевого, realm `iqport`, те же пользователи).

## Шаги

### 0. Преф-лайт
```bash
ss -ltnp | grep -E ':(8081|9091|4201)\b'   # должно быть пусто
free -m; df -h /home                        # клон + node_modules ≈ 1.5 ГБ, второй ng serve ≈ 0.5–1 ГБ RAM
```
Мало места или памяти — остановиться и сказать владельцу.

### 1. База (не ждёт домена)
```bash
sudo -u postgres psql -c "CREATE DATABASE dpport_river OWNER gtport_app"
sudo -u postgres psql -d dpport_river -v ON_ERROR_STOP=1 \
     -v app_role=gtport_app -v app_schema=dpport -f /home/alex/projects/DPmodule/scripts/db_bootstrap.sql
```
`db_bootstrap.sql` сам ставит схему, `GRANT CREATE` (нужен миграции 000001) и
`search_path`/`TimeZone` на пару роль+база. ⚠️ Всё, что льётся `psql` под `alex`,
принадлежит `alex` — миграции гнать только под `gtport_app` (DSN ниже).

### 2. Второй клон и накат
```bash
git clone git@github.com:Gtport/DPmodule.git /home/alex/projects/DPmodule2
cd /home/alex/projects/DPmodule2 && git checkout multi-tenant
cp /home/alex/projects/DPmodule/.env .env      # нужен только PG_PASSWORD; лишние секреты убрать
go build -o bin/server ./cmd/server/...
cd frontend && npm ci && cd ..

PG_DSN="postgres://gtport_app:<пароль>@localhost:5432/dpport_river?sslmode=disable" \
  go run ./cmd/migrate/... -dir migrations up
```

### 3. Справочники и клиентский сид

**Комплект CSV клиента АНБ уже собран и проверен вживую** (12.08.2026, WSL:
чистая база + этот комплект + реальная выгрузка ЛК → 316 вагонов, все
атрибутированы, ни одной неизвестной станции). Лежит на машине разработчика:
`<WSL>/projects/DP/DPmodule/_reference/seed_river/` — скопировать в
`_reference/seed/` клона 2 (например `scp -r` с WSL на VPS; git его не переносит,
каталог в gitignore).

Профиль клиента (из выгрузки ЛК за 31.07–12.08.2026): ООО «АНБ», нефтебаза,
станция назначения АМУР (970302, ДВС), ОКПО 33576117, грузы — нефтепродукты в
цистернах (дизтопливо/авиатопливо/бензин, группа «НЕФТЬ И НЕФТЕПРОДУКТЫ»),
подход ~430 вагонов за две недели (~33/сутки). В комплекте: `ports.csv` — один
терминал АНБ (`plan_code` пустой; ⚠️ `pc_other`/`pc_total` = 40 — ЗАГЛУШКА,
уточнить у владельца реальную перерабатывающую способность); `stations.csv` —
боевой справочник + 33 станции маршрутов АНБ (Южно-Уральская, Куйбышевская,
Горьковская — доливка также отдельным файлом `stations_additions.csv`);
`marka.csv` — 8 связок отправитель×станция погрузки (ГПН-Логистика/Комбинатская,
Орскнефтеоргсинтез/Никель, Газпромтранс/Сургут, Татнефть-Транс/Биклянь, Тайга
Энерджи/Магнитогорск, РН-Транс/Дземги+Суховская-Южная+Новая Еловка);
`naznach_station.csv` — одно универсальное правило АМУР→АНБ; `port_cargo_line.csv`
— одна линия «Нефтепродукты»; `max_chat.csv`/`max_route.csv` — пустые.

```bash
cd /home/alex/projects/DPmodule2
psql "$PG_DSN" -v ON_ERROR_STOP=1 -f scripts/seed_directories.sql
psql "$PG_DSN" -v ON_ERROR_STOP=1 -f scripts/clients/river.sql   # уже в git, заполнен
```
⚠️ Без справочников бэкенд не стартует (валидация `DirectoryCache` на старте).
`plan_profile`, `nitka_schedule`, `nmtp_*` клиенту НЕ заполнять: пусто = функции
скрыты, Stage 4 считает прогноз по перерабатывающей способности.
`scripts/clients/river.sql` уже ставит `client_name` и ослабляет
`max_staleness_minutes` до 720 (грузят руками — дефолтные 60 минут отвергали бы
выгрузку, снятую пару часов назад).

### 4. Юниты
Копии боевых (`~/.config/systemd/user/dpmodule-*.service`) с заменами:
`WorkingDirectory` → клон 2, `EnvironmentFile` → его `.env`, бэкенд
`-config config.river.yaml`, фронт `ng serve --configuration river --port 4201`.
⚠️ У фронта продублировать `--allowed-hosts` под `<домен2>` — иначе dev-сервер
отбросит чужой Host.
```bash
systemctl --user daemon-reload
systemctl --user enable --now dpmodule2-backend dpmodule2-frontend
```

### 5. nginx + сертификат (после DNS)
Новый server block `<домен2>`: `/` → `127.0.0.1:4201` (с WebSocket-апгрейдом, как
у боевого), `/api/` → `127.0.0.1:8081`, `client_max_body_size 50m`.
`/realms/` проксировать НЕ нужно: фронт ходит на Keycloak абсолютным адресом
боевого домена. `nginx -t` перед каждым `reload`; после certbot проверить, что
`95850.koara.live` по-прежнему отвечает.

### 6. Keycloak (единственное касание боевого)
В админке realm `iqport`, клиент `iqport-dpport`: добавить `https://<домен2>/*`
в **Valid redirect URIs** и в **Web origins**. Контейнер не перезапускать, realm
и пользователей не менять. Без этого будет 400 `Invalid parameter: redirect_uri`.

## Проверка

Второй инстанс:
```bash
curl -s -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8081/health   # 200
journalctl --user -u dpmodule2-backend -n 30 | grep -iE "error|listening|disabled"
```
В логе — `listening 127.0.0.1:8081`, интеграции «disabled», справочники прогреты.
Браузером `https://<домен2>`: вход существующим пользователем (`disp`/`boss`) →
главная с ОДНИМ терминалом, меню БЕЗ «Плана подвода». Затем ручной приём ЛК:
загрузить xlsx выгрузки клиента → вагоны разложились по терминалу; дыры
справочников придут уведомлениями (колокольчик) — дозаполнить в Админе.

Боевой инстанс (после шагов 5 и 6): вход на `95850.koara.live`, главная с тремя
терминалами, «План подвода» с обеими вкладками, `curl 127.0.0.1:8080/health`,
`journalctl --user -u dpmodule-backend` без ошибок, `docker ps` — Up-время
Keycloak не сбросилось.

Контур «раскатка меняет оба»: тривиальная правка в `multi-tenant` → по скиллу
`/deploy` сначала `DPmodule2` (канарейка) → smoke → затем `DPmodule`.

## Откат

Всё аддитивно, точек невозврата нет: `systemctl --user disable --now dpmodule2-*`,
убрать server block nginx и redirect URI из Keycloak, при необходимости
`DROP DATABASE dpport_river`. Боевой инстанс при этом не трогается вовсе.
