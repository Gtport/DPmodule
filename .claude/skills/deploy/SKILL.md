---
name: deploy
description: Собрать и раскатать DPmodule на VPS — сборка bin/server, перезапуск user-юнитов dpmodule-backend/dpmodule-frontend, проверка старта и ручек. Использовать, когда просят «раскатать», «задеплоить», «пересобрать и перезапустить», «применить на сервере» либо когда правки бэка/фронта надо увидеть вживую.
---

# Деплой DPmodule на VPS

Приложение живёт user-юнитами systemd (без sudo). Руками `go run` не поднимать —
процесс умрёт вместе с SSH-сессией.

## ⚠️ На VPS два инстанса одного репозитория

| | Боевой клиент («3 порта») | Второй клиент (речной порт, кодовое имя river) |
|---|---|---|
| Каталог | `/home/alex/projects/DPmodule` | `/home/alex/projects/DPmodule2` |
| Юниты | `dpmodule-backend`, `dpmodule-frontend` | `dpmodule2-backend`, `dpmodule2-frontend` |
| Конфиг | `config.vps.yaml` | `config.river.yaml` |
| Порты | 8080 / 9090 / 4200 | 8081 / 9091 / 4201 |
| База | `dpport` | `dpport_river` (схема в обеих — `dpport`) |
| Фронт | `ng serve` (development) | `ng serve --configuration river` |

**Порядок раскатки — канарейкой: сначала второй инстанс, потом боевой.** У второго
клиента нет живого диспетчера, поэтому поломку видно там, а не в бою:

```bash
cd /home/alex/projects/DPmodule2 && git pull   # шаги 2–4 ниже, юниты dpmodule2-*
# убедились, что встал и экраны живы →
cd /home/alex/projects/DPmodule  && git pull   # те же шаги, боевые юниты
```

Дальше в тексте команды приведены для боевого инстанса — для второго меняется
каталог, имя юнита, порт и `-config`.

## Порядок

### 1. Понять, что менялось
- Менялся Go-код или `config.yaml` → нужен шаг «Бэкенд».
- Менялось что-то во `frontend/` → нужен шаг «Фронтенд».
- Менялась схема БД (`migrations/`) → миграцию накатывать ОТДЕЛЬНО и осознанно
  (см. «Миграции» ниже), не считать её частью обычного деплоя. **Накатывать в ОБЕ
  базы** (`dpport` и `dpport_river`) — код общий, схема должна совпадать.

### 2. Бэкенд
```bash
cd /home/alex/projects/DPmodule
go build -o bin/server ./cmd/server/...      # имя бинаря фиксировано: юнит запускает bin/server
systemctl --user restart dpmodule-backend
sleep 2 && systemctl --user is-active dpmodule-backend
```
Проверить чистоту старта (не просто «active»):
```bash
journalctl --user -u dpmodule-backend --no-pager -n 15 | grep -iE "error|panic|fatal|listening"
```
Ждём `listening {"addr": "127.0.0.1:8080"}` и отсутствие error/panic.

### 3. Фронтенд
⚠️ Дев-сервер (`ng serve`) **умеет залипать**: бывало, что он не пересобирал правки
часами. Не полагаться на файловый watcher — перезапускать явно, когда меняли фронт.
```bash
systemctl --user restart dpmodule-frontend
sleep 20     # компиляция ~6–10 с, но старт дольше
journalctl --user -u dpmodule-frontend --no-pager -n 10 | grep -iE "generation complete|ERROR"
```
Ждём `Application bundle generation complete` без `ERROR`.

Перед перезапуском полезно прогнать `npx ng build --configuration development`
из `frontend/` — это AOT-сборка, ошибки шаблонов ловит строже, чем dev-сервер.

### 4. Проверка
```bash
curl -s -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8080/health          # ждём 200
curl -s -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8080/api/v1/<ручка>  # ждём 401
```
**401 на `/api/v1/*` — это норма и хороший знак**: значит маршрут смонтирован и
закрыт JWT. 404 — маршрут не поднялся (не тот бинарь / не перезапустили).

Живьём — `https://95850.koara.live` (nginx → :4200, WebSocket-апгрейд настроен).

### 5. Доложить
Что пересобрано, статус юнитов, что показал старт, какие ручки ответили. Если
что-то не так — привести строку лога, а не «вроде работает».

## Подводные камни
- **Конфиг с диска = текущая ветка.** Юнит читает `config.yaml` из рабочей папки.
  Если раскатываешь с ветки, где конфиг отличается от `main` (например `max.enabled:
  true`), после переключения на `main` и рестарта настройка откатится. Предупредить
  владельца об этом.
- **Раскатка неслитой ветки** — нормальная практика тут для проверки вживую, но
  сказать об этом прямо: «сейчас крутится код ветки X, после мержа станет постоянным».
- **Не трогать** порт 3000 и docker-контейнер `chromium`.
- **Секреты** (`PG_PASSWORD`, `ASU_TOKEN`, `MAX_BOT_TOKEN`, `KEYCLOAK_CLIENT_SECRET`)
  на этом стенде — в `.env` (в gitignore), его подключает systemd. Канон новых стендов
  другой: те же секреты объявляются в `config.yaml` шаблоном `${vault:<движок>/<путь>:<ключ>}`,
  значение подставляет CI/CD перед стартом; приоритет — файл конфига, env остаётся
  запасным путём. Значения не печатать в вывод даже при отладке.
- Порты приложения слушают только `127.0.0.1`, наружу — исключительно через nginx.

## Миграции (отдельно от деплоя)
Схему ведёт `golang-migrate`. Обёртка `cmd/migrate` умеет только `up|down|drop`
(без `version`), а `schema_migrations` лежит в схеме `dpport`. Гнать `migrate up`
вслепую рискованно: при неверном `search_path` инструмент примет базу за пустую и
попытается накатить всё с нуля.

Безопасный путь для одиночной добавляющей миграции:
1. посмотреть текущую версию: `SELECT version, dirty FROM dpport.schema_migrations`;
2. прогнать SQL миграции в транзакции с `ROLLBACK`, проверить результат;
3. применить в реальной транзакции и сдвинуть маркер версии;
4. **если миграция создаёт таблицу — сразу передать её роли приложения**
   (`psql -d dpport` работает под `alex`, и созданное принадлежит `alex`, а не
   `gtport_app` → приложение получит permission denied, ручка ответит 502):
   ```sql
   ALTER TABLE dpport.<таблица> OWNER TO gtport_app;
   ALTER SEQUENCE dpport.<таблица>_id_seq OWNER TO gtport_app;  -- если есть bigserial
   ```
   Проверить: `SELECT tableowner FROM pg_tables WHERE tablename='<таблица>'`
   — должно быть `gtport_app`, как у остальных настроечных таблиц. (Локально
   этой ловушки нет: `make migrate-up-local` ходит уже под `gtport_app`.)
   Наступили на это 01.08.2026 на `lk_account` (робот ЛК).
5. показать владельцу счётчики до/после.

Любые изменения боевых данных — только после прогона на `ROLLBACK` и с явного
подтверждения владельца.

## Настроечные данные клиента (чистая база)

Миграции — нейтральное ядро, клиентских данных в них нет (вынесены 12.08.2026).
Порядок наката базы клиента с нуля:

```
bootstrap (scripts/db_bootstrap.sql) → migrate up
  → psql -f scripts/seed_directories.sql          # справочники из _reference/seed/*.csv (вне git)
  → psql -f scripts/clients/<клиент>.sql          # станции плана, маршруты MAX, раскладка отчётов
```

`scripts/clients/gtport.sql` — сид боевого клиента (3 терминала); новому клиенту
свой файл по `scripts/clients/_template.sql`. Клиентский сид обязателен и **после
каждого пересева справочников**: `seed_directories.sql` делает TRUNCATE `ports`
и обнуляет `provider_client` / `org_short` / `nmtp_norm`, которых нет в CSV.
