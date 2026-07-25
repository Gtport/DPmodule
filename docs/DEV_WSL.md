# Разработка на локальной машине (WSL)

Как поднять полноценную рабочую копию DPmodule в WSL: свой Postgres с копией
боевых данных, свой Keycloak, свои бэкенд и фронтенд. VPS при этом остаётся
**боевым сервером**, а не площадкой для экспериментов.

## Разделение ролей

| | WSL (локально) | VPS (212.113.99.3) |
|---|---|---|
| Что делаем | пишем код, гоняем тесты, смотрим экраны | `git pull` + пересборка + рестарт |
| База | своя копия боевой | боевая, с ней работает диспетчер |
| Keycloak | свой в docker (`localhost:8180`) | боевой за nginx |
| Внешние интеграции (АСУ, MAX, памятки) | **выключены** | включены, работают по крону |
| Точка входа | `http://localhost:4200` | `https://95850.koara.live` |

Обмен — **только через GitHub**. Файлы между машинами руками не копируем, кроме
того, что лежит вне git (см. шаг 3) — это разовая операция.

⚠️ Правило из `CLAUDE.md` остаётся в силе и здесь: **один клон = один автор**.
WSL-клон — третий по счёту (после клонов владельца и помощника на VPS), и работает
в нём только один человек.

---

## Шаг 1. Инструменты

Проверить, что уже стоит: `go version && node -v && docker --version && psql --version`.

Нужны версии не ниже боевых, иначе сборка или заливка дампа упадут:

| Инструмент | Версия на VPS | Как поставить в Ubuntu-WSL |
|---|---|---|
| Go | 1.26.5 (`go.mod` требует ≥ 1.26.1) | с [go.dev/dl](https://go.dev/dl/): распаковать в `/usr/local/go`, добавить `/usr/local/go/bin` в `PATH` — в apt нужной версии нет |
| Node | 24.18.0 (npm 11) | `nvm install 24` или репозиторий NodeSource |
| PostgreSQL | 16.14 | `sudo apt install postgresql-16 postgresql-client-16` |
| Docker | для Keycloak | Docker Desktop с включённой интеграцией WSL, либо `docker.io` внутри WSL |

Postgres в WSL после установки надо запускать самому: `sudo service postgresql start`
(либо включить systemd — в `/etc/wsl.conf` секция `[boot]` → `systemd=true`, затем
`wsl --shutdown` в Windows).

## Шаг 2. Доступы

**GitHub.** Клон уже сделан по SSH (`git@github.com:Gtport/DPmodule.git`), значит
ключ работает. Если нет — `ssh-keygen -t ed25519`, публичную часть добавить в
GitHub → Settings → SSH keys.

**VPS.** Нужен для обновления копии базы. Парольный вход на сервере закрыт, поэтому:

```bash
ssh-keygen -t ed25519 -C "wsl"                  # если ключа ещё нет
cat ~/.ssh/id_ed25519.pub                        # скопировать строку
```

Строку добавить на сервере в конец `~/.ssh/authorized_keys` (одной командой в
терминале VPS, подставив свою строку):

```bash
echo 'ssh-ed25519 AAAA... wsl' >> ~/.ssh/authorized_keys
```

Проверка из WSL: `ssh alex@212.113.99.3 'hostname'`. Удобно записать адрес один раз:

```bash
echo 'export DPM_VPS=alex@212.113.99.3' >> ~/.bashrc && source ~/.bashrc
```

## Шаг 3. Файлы вне git

Часть данных в репозиторий намеренно не попадает (справочники клиента, секреты,
учётки Keycloak). Их переносим с VPS один раз:

```bash
cd ~/projects/DPmodule                                   # свой путь к клону
scp -r $DPM_VPS:~/projects/DPmodule/_reference/seed _reference/   # справочники, 1.1 МБ
scp -r $DPM_VPS:~/projects/DPmodule/_data .                       # файлы ЛК и планов, 2.5 МБ
mkdir -p deployments/keycloak/import
scp $DPM_VPS:~/keycloak/import/iqport-realm.json deployments/keycloak/import/
```

Что это:
- `_reference/seed/` — CSV справочников (`cargo`, `stations`, `marka`, `ports`…).
  Без них бэкенд не поднимется: цепочка «схема → seed → загрузка» проверяется на старте.
- `_data/` — ранее загруженные файлы ЛК и планов, удобно для отладки парсеров.
- `iqport-realm.json` — экспорт реалма с пользователями `boss` и `disp`, ролями и
  уже прописанным redirect URI `http://localhost:4200/*`. Внутри учётки — в git не класть.

## Шаг 4. Секреты

Создать `.env` в корне клона (файл в git не попадает):

```bash
PG_PASSWORD=<пароль роли gtport_app — тот же, что на VPS>
```

`ASU_TOKEN` и `MAX_BOT_TOKEN` локально **не нужны**: обе интеграции выключены (шаг 5).
Если понадобится отладить разбор ответа АСУ — добавить `ASU_TOKEN`, но крон не включать.

## Шаг 5. Конфиг бэкенда

```bash
cp config.local.example.yaml config.local.yaml
```

`config.local.yaml` — вне git, поэтому боевой `config.yaml` не будет мелькать в
diff'ах. В шаблоне уже: локальный Postgres, `issuer: http://localhost:4200/realms/iqport`
и `enabled: false` у `asu`, `wagonops`, `reference`, `max`, `bros`.

⚠️ **Не включать `max.enabled`** на локальной машине: чаты в таблице `max_chat`
настоящие, и рассылка уйдёт живым получателям. То же с `asu` и `bros` — это работа
боевого инстанса, второй такой же будет дублировать.

## Шаг 6. База данных

```bash
sudo service postgresql start
scripts/dev_db_refresh.sh          # снимет дамп на VPS и зальёт в локальную dpport
```

Скрипт удаляет и пересоздаёт **локальную** базу, боевую только читает. На самом VPS
он запускаться отказывается (там «локальная» база — это боевая). По умолчанию
переносится всё, кроме тела `vagon_operation` (698 тыс. строк истории продвижения) —
дамп получается ~15 МБ вместо ~35 МБ. Если отлаживаете историю движения вагона:

```bash
scripts/dev_db_refresh.sh --full
```

Дальше эта же команда — способ подтянуть свежие боевые данные, когда локальная
копия устареет.

Миграции применяются к локальной базе обычным способом:

```bash
export PG_DSN="postgres://gtport_app:$PG_PASSWORD@localhost:5432/dpport?sslmode=disable"
make migrate-up
```

⚠️ `PG_DSN` не должен указывать на боевую базу. Миграция, применённая туда с
локальной машины, изменит схему под работающим боевым бинарём.

## Шаг 7. Keycloak

```bash
cd deployments/keycloak
cp .env.example .env
```

В `.env` поставить:

```
KC_BOOTSTRAP_ADMIN_USERNAME=admin
KC_BOOTSTRAP_ADMIN_PASSWORD=<любой локальный>
KC_HOSTNAME=http://localhost:4200
```

`KC_HOSTNAME` обязан совпадать с `keycloak.issuer` в `config.local.yaml` (там же
плюс `/realms/iqport`) — бэкенд сверяет issuer точной строкой, расхождение даёт
«invalid token» на всех страницах.

```bash
docker compose up -d
docker compose logs -f          # ждать «Keycloak … started», первый старт 1–2 минуты
curl -s http://localhost:8180/realms/iqport/.well-known/openid-configuration | grep -o '"issuer":"[^"]*"'
```

Должно вернуть `"issuer":"http://localhost:4200/realms/iqport"`.

## Шаг 8. Запуск

Два терминала (или два таба VS Code):

```bash
# бэкенд
go run ./cmd/server -config config.local.yaml

# фронтенд
cd frontend && npm ci        # только в первый раз, ~437 МБ node_modules
npm start                    # ng serve на http://localhost:4200
```

Dev-сервер проксирует `/api` на `:8080` и `/realms` на `:8180`
(`frontend/proxy.conf.json`) — то, что на VPS делает nginx.

Открыть `http://localhost:4200`, войти пользователем `boss` (роль administrator)
или `disp`. Пароли — те же, что на боевом стенде.

---

## Туннель к боевой базе

Для разведки боевых данных (посмотреть реальный снимок, сравнить с локальным
результатом) — SSH-туннель. Порт локально берём **5433**, чтобы не спорить со
своим Postgres на 5432:

```bash
ssh -N -L 5433:localhost:5432 $DPM_VPS &
psql "postgres://dpport_ro:<пароль>@localhost:5433/dpport"
```

Роль `dpport_ro` — только чтение (`scripts/dev_pg_readonly.sql`, выполняется на VPS
один раз суперпользователем):

```bash
sudo -u postgres psql -d dpport -v ro_pass="'ПАРОЛЬ'" -f scripts/dev_pg_readonly.sql
```

У неё `default_transaction_read_only = on`, поэтому случайный `UPDATE` из psql или
DBeaver отвалится с ошибкой, а не испортит рабочий день диспетчеру.

⚠️ Через туннель ходим **только смотреть**. Бэкенд на боевую базу не направляем:
любое нажатие «Приём ЛК», «Обновить из АСУ» или «Обновить справочники» перезапишет
боевой снимок `disl_actual`.

## Ежедневный цикл

```bash
# WSL: работа
git pull                                  # подтянуть чужие merge
git checkout -b feat/<задача>
# … правки, go build ./... && go test ./... , npm start для проверки экранов
git push -u origin feat/<задача>
gh pr create                              # PR на GitHub, второй участник смотрит diff
```

```bash
# VPS: после merge PR
cd ~/projects/DPmodule
git pull
# сборка и рестарт — скилом /deploy или руками:
go build -o bin/server ./cmd/server/...
systemctl --user restart dpmodule-backend
```

Фронтенд на VPS пересобирать не нужно: `dpmodule-frontend` — это `ng serve`, он сам
подхватывает изменившиеся файлы после `git pull`.

⚠️ Если миграция новая — на VPS применить её **до** рестарта бэкенда
(`PG_DSN=… make migrate-up`).

## Если что-то не работает

| Симптом | Причина |
|---|---|
| На всех страницах «invalid token», в логе `token has invalid issuer` | `KC_HOSTNAME` и `keycloak.issuer` разошлись. Привести к одной строке, перезапустить оба |
| Логин уводит на несуществующую страницу | Keycloak не поднят или `/realms` не проксируется — проверить `docker ps` и `frontend/proxy.conf.json` |
| Бэкенд падает на старте про справочники | не перенесён `_reference/seed/` (шаг 3) |
| `pg_restore: unsupported version` | локальный клиент старше 16 — поставить `postgresql-client-16` |
| `connection refused` на 5432 | в WSL Postgres не стартует сам: `sudo service postgresql start` |
| Порт 4200 занят | уже запущен второй `ng serve` — найти его: `ss -tlnp` |
| Экраны пустые, данных нет | локальная база не залита — `scripts/dev_db_refresh.sh` |
