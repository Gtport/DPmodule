# АНБ на VPS: памятка эксплуатации

> Для ассистента и владельца. С 21.08.2026 VPS `212.113.99.3` обслуживает
> **только клиента АНБ**. Основной инстанс (Мыс) переехал на корпоративный прод
> `147.45.97.83` и на VPS потушен — его юниты `dpmodule-*` **не включать**.
> История развёртывания АНБ — `SECOND_INSTANCE.md`; настройки таблицами — `SETTINGS.md`.

## Карточка инстанса

| | |
|---|---|
| Адрес | **`https://anb-port.duckdns.org`** (DuckDNS → 212.113.99.3) |
| Клиент | ООО «АНБ» — нефтебаза, речной порт; ст. назначения АМУР (970302, ДВС) |
| Профиль работы | нефтепродукты в цистернах, ~33 ваг/сутки; **выгрузки ЛК грузят руками** (интеграций нет) |
| VPS | `ssh alex@212.113.99.3` (sudo — только владелец: у ассистента нет TTY для пароля) |
| Каталог | `/home/alex/projects/DPmodule2`, ветка `main` |
| Юниты | `dpmodule2-backend`, `dpmodule2-frontend` — **пользовательские** systemd (`systemctl --user`) |
| Конфиг бэкенда | `config.river.yaml` (+ `.env` — только `PG_PASSWORD`) |
| Порты | API `127.0.0.1:8081`, метрики `9091`, фронт `4201`; наружу — только через nginx |
| База | `dpport_river`, схема `dpport`, роль `gtport_app`, порт `5432` |
| Вход | Keycloak realm `iqport`, пользователи `disp` / `boss` |
| nginx | `/etc/nginx/sites-available/anb-port.duckdns.org` |

## ⚠️ Три вещи, которые нельзя ломать

1. **Keycloak живёт в docker-контейнере бывшего боевого Мыса и нужен АНБ.**
   Фронт АНБ ходит за токенами абсолютным адресом `https://95850.koara.live`.
   Значит, несмотря на потушенный Мыс, должны жить: контейнер `keycloak`
   (`docker ps` — поднимается сам), nginx-блок домена `95850.koara.live` и его
   TLS-сертификат. «Прибраться» за уехавшим Мысом = положить вход АНБ.
2. **Юниты Мыса `dpmodule-backend` / `dpmodule-frontend` не запускать.**
   На 21.08.2026 они остановлены, но ещё стояли в автозапуске (`enabled`) —
   владельцу нужно один раз выполнить:
   `systemctl --user disable dpmodule-backend dpmodule-frontend && systemctl --user reset-failed`.
   Пока это не сделано — после ребута VPS старый Мыс-бэкенд встанет сам и его
   включённые интеграции (рассылки MAX, крон АСУ) задублируют корп-прод.
3. **Интеграции АНБ выключены конфигом — не включать.** В `config.river.yaml`
   `max`/`asu`/`wagonops`/`reference`/`bros` стоят `enabled: false`; данные
   заходят только ручной загрузкой ЛК.

## Повседневные команды

```bash
# статус и логи
systemctl --user status dpmodule2-backend dpmodule2-frontend
journalctl --user -u dpmodule2-backend -n 50 --no-pager    # или -f — вживую
journalctl --user -u dpmodule2-frontend -n 50 --no-pager

# рестарт / стоп / старт
systemctl --user restart dpmodule2-backend dpmodule2-frontend
systemctl --user stop    dpmodule2-backend dpmodule2-frontend
systemctl --user start   dpmodule2-backend dpmodule2-frontend

# smoke
curl -s -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8081/health   # 200
curl -s -o /dev/null -w "%{http_code}\n" https://anb-port.duckdns.org/  # 200
journalctl --user -u dpmodule2-backend -n 30 | grep -iE "error|listening|disabled"
# в логе: listening 127.0.0.1:8081, интеграции «disabled», справочники прогреты
```

## Раскатка обновления

Код уходит в бой только через `origin/main` (гейт — `git push` с машины
разработки: пушится лишь то, что собирается и проходит тесты).

```bash
cd /home/alex/projects/DPmodule2
git pull
go build -o bin/server ./cmd/server/...
cd frontend && npm ci && cd ..        # только если менялся package-lock.json
PG_DSN="postgres://gtport_app:<пароль из .env>@localhost:5432/dpport_river?sslmode=disable" \
  go run ./cmd/migrate/... -dir migrations up   # только если пришли миграции
systemctl --user restart dpmodule2-backend dpmodule2-frontend
curl -s -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8081/health
```

Каталог Мыса `/home/alex/projects/DPmodule` не обновлять и миграции в базу
`dpport` на VPS не катить — этот инстанс уехал, его копия здесь заморожена.

## База и настройки клиента

```bash
psql "postgres://gtport_app:<пароль>@localhost:5432/dpport_river?sslmode=disable"
```

- База **боевая** — с ней работает живой клиент. Правки данных — только по
  явной просьбе; сначала `SELECT`, потом точечный `UPDATE`, потом снова `SELECT`
  (цикл из `SETTINGS.md`).
- Пороги приёма — `dpport.client_settings` (`id=1`), JSON `ingest_policy`.
  У АНБ намеренно ослаблен `dislocation.max_staleness_minutes = 720`:
  выгрузку ЛК снимают и грузят руками с лагом в часы. Не «чинить» до дефолтных 60.
- Терминал — `dpport.ports` (одна строка). ⚠️ `pc_other`/`pc_total` = 40 —
  заглушка, реальную перерабатывающую способность уточнить у владельца.
- `plan_profile`, `nitka_schedule`, `nmtp_*` пусты **намеренно**: пустые таблицы
  скрывают «План подвода» и отчёт НМТП в UI. Не заполнять.
- Клиентский сид и настройки — `scripts/clients/river.sql` (эталон того, что залито).

## Сертификаты (два, оба нужны)

| Домен | Зачем | Проверка |
|---|---|---|
| `anb-port.duckdns.org` | сам АНБ | выпущен 12.08.2026, истекает **10.11.2026** |
| `95850.koara.live` | Keycloak для входа АНБ | должен продлеваться и после отъезда Мыса |

```bash
echo | openssl s_client -connect anb-port.duckdns.org:443 2>/dev/null | openssl x509 -noout -dates
echo | openssl s_client -connect 95850.koara.live:443    2>/dev/null | openssl x509 -noout -dates
sudo certbot renew --dry-run    # только владелец
```

## Типовые поломки

| Симптом | Причина / что делать |
|---|---|
| Сайт отдаёт 502 | Упал юнит: `systemctl --user status dpmodule2-*`, поднять, смотреть `journalctl` |
| «invalid token» / вечный логин | Keycloak: жив ли контейнер (`docker ps`), отвечает ли `https://95850.koara.live/realms/iqport/.well-known/openid-configuration` (ждём 200), не истёк ли серт koara.live |
| 400 `Invalid parameter: redirect_uri` | В клиенте `iqport-dpport` пропал redirect URI `https://anb-port.duckdns.org/*` |
| Выгрузка ЛК отвергается по свежести | Файл старше `max_staleness_minutes` (720): снять свежую выгрузку; порог — `client_settings.ingest_policy` |
| После ребута VPS ничего не работает | Юниты `dpmodule2-*` в `enabled` и стартуют сами; keycloak-контейнер тоже. Проверить `docker ps` и `/health`; Postgres — systemd |
| Запрос из фронта ушёл «не туда» | У фронта свой `frontend/proxy.river.conf.json` (`/api` → `:8081`); боевой `proxy.conf.json` целится в `:8080` — не путать |

## Откат / аварийная остановка

```bash
systemctl --user stop dpmodule2-backend dpmodule2-frontend     # до ручного старта или ребута
systemctl --user disable --now dpmodule2-backend dpmodule2-frontend  # насовсем (и после ребута)
```
Всё аддитивно: nginx-блок и база при остановке юнитов не трогаются.
