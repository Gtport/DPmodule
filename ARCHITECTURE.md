# GTport — Архитектура серверной части (как есть)

> Документ описывает **текущее** состояние backend-части GTport, реконструированное
> из исходного кода. Это карта «как есть» — фундамент для последующего
> `HARDCODE_INVENTORY.md` (что зашито под 3 порта) и `TARGET.md` (целевая
> универсальная модель). Здесь намеренно **нет** предложений по изменению —
> только то, что есть сейчас.

---

## 1. Назначение системы

GTport — рабочий инструмент диспетчера логистического центра, обслуживающего
три морских порта. Система принимает данные о вагонах (дислокацию) из внешней
АСУ и от операторов, обогащает их по справочникам, строит планы подвода,
повагонные и портовые отчёты, и рассылает их по каналам (Telegram, MAX, Email,
Yandex.Disk).

**Три порта зашиты в код** на уровне доменной модели:
`PortType` = `at` / `ut` / `gut` (отображаемые имена `АЭ` / `УТ-1` / `ГУТ-2`).
Это ключевое ограничение, которое предстоит снять в прототипе — детально
оно каталогизируется в отдельном документе.

---

## 2. Технологический стек

**Backend (Go)**
- HTTP-роутер: `go-chi/chi/v5` (+ `chi/cors`, `chi/middleware`)
- БД-доступ: `jmoiron/sqlx`, драйвер `lib/pq`
- Аутентификация: `golang-jwt/jwt/v5`
- Парсинг Excel: `xuri/excelize/v2`
- Rate limiting: `ulule/limiter/v3` (in-memory store)

**Хранилище**
- PostgreSQL, **две базы**:
  - `gtport_prod` — основная (дислокация, справочники, планы, пользователи и т.д.)
  - `gtport_tiles` — тайлы карты (отдельное подключение `TilesDB`)

**Frontend (React)**
- TypeScript + JSX, `react-router`
- UI: смесь `antd` и MUI (`@mui`)
- Запросы: `axios` / `fetch`, токен из `localStorage`

---

## 3. Структура пакетов (Go)

```
server/
  main.go                 — точка входа, ручная сборка зависимостей, запуск
  internal/
    config/               — конфиг из env (config.go)
    db/                   — подключение, миграции
    middleware/           — JWTAuth, RequireRole/Permission, RateLimit, CORS
    api/ (handlers)       — HTTP-обработчики + router.go
    service/              — бизнес-логика (основной объём)
    repository/           — доступ к данным (sqlx)
    models/               — структуры данных
    utils/                — хелперы (форматирование, null-хелперы, время)
    sms_plan/             — самостоятельный модуль со СВОИМ factory/container
```

**Важная особенность сборки зависимостей.** Единого DI-контейнера нет.
Зависимости собираются вручную в `main.go` и частично в `router.go`
(там создаётся часть хендлеров). Исключение — модуль `sms_plan`, у которого
есть собственные `factory.go` и `container.go` с паттерном «конфиг → сервис».
Это единственный модуль с формализованным DI; остальная система — ручная
проводка.

---

## 4. Жизненный цикл приложения (main.go)

Порядок старта:

1. `config.Load()` — чтение env (через `getEnv`/`getEnvAsInt`/`getEnvAsBool`).
2. Подключение к основной БД (`db.NewDB`) и к БД тайлов (`db.NewTilesDB`).
3. Запуск миграций из каталога `migrations` (если каталог есть);
   при ошибке — попытка инициализировать таблицы уведомлений напрямую.
4. Ручная инициализация репозиториев, сервисов и хендлеров.
5. Сборка роутера `api.NewRouter(...)`.
6. Запуск фоновых планировщиков (file-sync, авто-экспорт, очистка уведомлений,
   Yandex.Disk — см. раздел 8).
7. Создание `http.Server` и запуск:
   - если `SERVER_USE_HTTPS=true` и сертификаты на месте → `ListenAndServeTLS`;
   - иначе (или если сертификат не найден) → fallback на `ListenAndServe` (HTTP).
8. Graceful shutdown по `SIGINT`/`SIGTERM` (таймаут 30 с), закрытие каналов
   остановки планировщиков.

---

## 5. Ядро системы — подсистема дислокации

Самая сложная и центральная часть. Управляется `DislocationService`.

### 5.1. Хранение в оперативной памяти

Дислокация целиком держится в RAM ради скорости отчётов:

- `actualMap map[string]Dislocation` — записи по детерминированному `ID`.
- `IndexMaps` — **9 вторичных индексов** (`map[string][]*Dislocation`):
  Vagon, Invoice, Index, StationNach, Gruzotpr, StanNazn, Gruzpol, GruzpolS,
  Naznach.
- Защита: `sync.RWMutex` (`mu`) для данных и отдельный `muIndex` для индексов.

При старте: загрузка `DirectoryCache` → загрузка таблицы `disl_actual` в память
(`LoadActualToMemory`) → первичное обогащение.

### 5.2. Конвейер обогащения (этапы)

Обработка устроена как многоэтапный конвейер. Точка входа выбирает стартовый
этап (`switch stage`): 0, 3 или 4.

- **Stage 0** (`processNewRecordsWithSource`) — полная обработка новых записей.
- **Stage 2** — второе обогащение (`SecondEnrichmentBatch` / `SecondEnrichment`)
  + построение индексов. Обогащение тянет данные из `DirectoryCache`
  (Stations, Marka, CargoOperations, Ports) — например, `enrich_stage2`
  заполняет клиента, груз, sms-поля, sprav-поля, цвет из справочника `marka`.
- **Stage 3** — третье обогащение (перестановки, `ThirdEnrichment`).
- **Stage 4** — прогнозное обогащение (`FourthEnrichment`).

Файлы: `enrich_stage1..4.go`, `data_preparation.go`, `dislocation_processing.go`.

### 5.3. Атомарная замена и сохранение

Конвейер строит **новые** `newMap` + `newIndexMaps`, и только в конце атомарно
подменяет их в памяти под двойным локом. Затем:

- `SaveNewMapToDB` → `SwapTablesInDB` — сохранение в БД с атомарной заменой
  таблиц дислокации.
- Режим истории зависит от источника: для АСУ-источников
  (`isASUSource`) сохранение **без истории** (`SaveNewMapToDBSilent`);
  для ручных — **с историей**.

### 5.4. Защитные механизмы и побочные очереди

- **Контроль потери данных:** если новых записей на ≥30 % меньше текущих —
  обновление прерывается (защита от битого входного файла).
- **Проверка контекста** на каждом длинном шаге (отмена прерывает безопасно,
  данные уже в памяти считаются успехом).
- **Очередь бросов** (`ProcessBrosQueue`) — брошенные поезда.
- **Очередь статуса 10** (`processStatus10Queue`) — прибытие/выгрузка вагонов.
- **Статус 5** (`dislocation_status5.go`).
- После успешного обновления — экспорт полной дислокации в Excel
  (`exporter.ExportFullDislocation`) и обновление дневной истории
  (`BatchUpdateHistoryFromActual`).

> Примечание: агрегация справок в БД **упразднена** — портовый отчёт строится
> напрямую из памяти (`actualMap`) при каждом запросе (см. `port_report_service.go`).

---

## 6. Входной конвейер данных (как данные попадают в систему)

Два источника:

### 6.1. Автоматический — внешняя АСУ через ESAT API

`FileSyncService` (`file_sync_service.go`) по расписанию (`FILE_SYNC_SCHEDULE`,
cron) опрашивает ESAT API (`ESAT_API_BASE_URL`, ключ `ESAT_API_KEY`),
скачивает JSON-файлы и запускает обработку дислокации.
Триггеры: `startup` / `schedule` / `manual` (алиасы «Старт»/«Расписание»/«Оператор»).
Источник помечается как `json_files`. Может быть отключён `DISABLE_FILE_SYNC=true`.

### 6.2. Ручной — загрузка файлов оператором

`FileUploadHandler` (`file_upload.go`) принимает `POST /api/upload`
(multipart), сохраняет файл, разрешает конфликты (force-upload),
запускает парсинг с таймаутом 30 с.

### 6.3. Парсеры

- **`parse_lk.go` (LKParser)** — Excel-файлы ЛК. Находит строку заголовка по
  «Номер вагона», определяет индексы колонок по заголовкам (сначала точное
  совпадение, затем частичное), генерирует детерминированный `ID` по формуле
  `вагон/станция/дата`. Источник `lk_files`.
- **`parse_json.go`** — JSON из АСУ.
- **План подвода** — два семейства парсеров со своими «старым» и «новым»
  форматами:
  - `plan_ma_*` (МА): `plan_ma_parser_old/new`, `plan_ma_analytics`, `plan_ma_service`.
  - `plan_nk_*` (NK): `plan_nk_parser_old/new`, `plan_nk_analytics`,
    `plan_nk_synonyms`, `plan_nk_dislocation`, `plan_nk_converter` и др.
    Содержит зашитые константы скорости выгрузки (`SpeedNMTP_Nk`, `SpeedUgTerm_Nk`).
  Результат — `PlanDataMa`, далее `plan_storage.go`, `plan_converter.go`.

---

## 7. Прикладные подсистемы

| Подсистема | Назначение | Ключевые файлы |
|---|---|---|
| Дислокация | см. раздел 5 | `dislocation*.go`, `enrich_stage*.go` |
| План подвода | парсинг и аналитика планов МА/NK | `plan_*`, `plan_ma_*`, `plan_nk_*` |
| SMS-план | генерация SMS-плана по портам из cargo_work + дислокации, с кэшем | модуль `sms_plan/`, `sms_plan_cache*` |
| Грузовая работа | повагонная/грузовая работа (vigr) по портам | `cargowork*.go` |
| Портовый отчёт | строится из памяти дислокации; есть вариант НМТП | `port_report*.go` |
| Брошенные поезда | журнал, коды причин, аналитика | `bros*.go` |
| Склад | складские остатки (+ вариант Аттис) | `stock*.go` |
| Судовые партии | импорт/парсинг, суточные отчёты | `vessel_call*.go` |
| Перестановки | перестановки + «вселенная станций» | `rearrangement*.go`, `stations_univers.go` |
| Карта/тайлы | работа с БД тайлов | `map_*.go`, `tile_*.go` |
| История | дневная история вагонов, history-таблицы | `history*.go`, `info_history_repository.go`, `vagon_history.go` |
| Уведомления | внутренние уведомления + очистка | `notification*.go` |
| Бэкап | резервные копии | `backup*.go` |
| Массовое обновление | bulk-операции | `bulk_update*.go` |
| Справочники (RAM) | словари в оперативной памяти | `directories*.go`, `dislocation_directory.go` |
| Аналитика | скорости GT, снапшоты плана, портовая аналитика | `analytics.go`, `gt_*`, `port_analytics_service.go` |

---

## 8. Внешние интеграции и фоновые задания

- **ESAT API** — вход дислокации (см. 6.1).
- **Telegram-бот** (`telegram_service.go`) — уведомления
  (`TELEGRAM_BOT_TOKEN`, `TELEGRAM_CHAT_ID`).
- **MAX-бот** (`max_service.go`, `max_chat*.go`) — отправка отчётов и повагонки.
  Есть **авто-экспорт** (`auto_export_service.go`) по cron (`AUTO_EXPORT_CRON`),
  с выбором чатов по именам и фильтром данных.
- **Email/SMTP** (Yandex) — рассылки (`EMAIL_*`).
- **Yandex.Disk** (`yandex_disk.go`) — выгрузка данных по cron/интервалу.
- **Очистка уведомлений** — планировщик старше 24 ч при старте и далее.

Все планировщики используют общий канал остановки (`stopCleanup`),
завершаются корректно по сигналу.

---

## 9. Аутентификация и авторизация

- **JWT** (access + refresh, TTL из `JWT_ACCESS_TTL` / `JWT_REFRESH_TTL`).
  `middleware/auth.go` (`JWTAuth`) только проверяет токен и кладёт
  `userID/role/login/fullName` в контекст. Проверка прав — отдельно.
- **Ролевая иерархия** (`middleware/require_role.go`), от низшей к высшей:
  `client` (30) < `port` (50) < `port_dispatcher` (60) < `client_dispatcher` (70)
  < `operator` (80) < `admin` (100).
- Middleware прав: `RequireRole(minRole)`, `RequirePermission(db, "resource.action")`
  (operator и выше проходят автоматически; права — в `permission_repository.go`),
  `RequireSpecificUsers(...)` — для суперчувствительных маршрутов.
- Права задаются **прямо в роутере** через chi-группы, без отдельной
  большой карты маршрутов.
- **LockdownMode** (`lockdown_mode.go`) — синглтон аварийной блокировки входа;
  один экземпляр передаётся в login/refresh/user-хендлеры.
- Прочее: `audit.go`, `security.go`, `rate_limit.go`, `user_blocklist.go`,
  `token_repository.go`.

Публичные маршруты (login, refresh, `/health`, `/api/time`) регистрируются
вне защищённой группы.

---

## 10. Слой API (роутер)

`api/router.go` (chi). Характерные группы маршрутов:
`/api/dislocation/*` (+ `bros`, `redirection`, `rearrangement`),
`/api/rearrangement/stations/*`, `/api/vessel-calls/*`, `/api/cargowork/vigr/{port}/*`,
`/api/directories/*`, `/api/port-report*`, `/api/file-sync/*`, `/api/upload`,
`/api/admin/lockdown/*`, `/api/admin/sessions`, `/api/admin/backups` и др.

Паттерн доступа: чтение обычно от `client` и выше, мутации — от `operator`,
отдельные операции — через `RequirePermission` или `RequireSpecificUsers`.

---

## 11. Frontend (кратко)

- `App.tsx` — `react-router` с маршрутами по ролям: `/admin/*`, `/operator/*`,
  `/port/*`, `/client/*`. Защита — `PrivateRoute`, перенаправление по роли —
  `RoleBasedRedirect`, тонкий контроль — `AccessControl`.
- Провайдеры контекста: `AuthContext`, `DataProvider`, `NotificationContext`,
  `FileUploaderContext`, `ModalContext`.
- Компоненты завязаны на порты в нескольких местах (имена портов, чаты MAX
  и т.п. зашиты на клиенте — каталогизируется отдельно).

---

## 12. Базы данных (ключевые таблицы)

**`gtport_prod`:**
- Дислокация: `disl_actual` (+ swap-таблицы, история).
- Справочники: `stations`, `marka`, `cargo_operations`, `ports`.
- Планы/отчёты: `cargo_work`, `sms_plan`, `sms_plan_cache`
  (с колонками `port_at` / `port_ut` / `port_gut` — схема «знает» про 3 порта).
- Брошенные: `bros`, журналы, коды причин.
- Доступ: `users`, `permissions`, токены.
- Прочее: `vessel_calls`, склад, `notifications`.

**`gtport_tiles`:** тайлы карты.

> Уже существующая таблица `ports` и паттерн `DirectoryCache` (загрузка
> справочников в RAM при старте) — потенциальная опора для будущей
> универсализации, но сейчас большая часть порт-специфики живёт в коде,
> а не в данных.

---

## 13. Поток данных (сводно)

```
[АСУ / ESAT API] --(cron poll)--> FileSyncService --+
                                                    |
[Оператор] --(POST /api/upload)--> FileUploadHandler+--> Парсеры (LK/JSON/План)
                                                    |
                                                    v
                                   DislocationService (RAM: actualMap + индексы)
                                                    |
                              Stage 0/2/3/4 обогащение (DirectoryCache)
                                                    |
                         атомарная замена в RAM  +  SaveNewMapToDB/SwapTablesInDB
                                                    |
                   +--------------------+-----------+-----------+
                   v                    v                       v
          Портовый отчёт         SMS-план / cargo_work     История / бросы
          (из памяти)            (с кэшем)                  (очереди)
                   |
                   v
   Каналы вывода: Excel-экспорт, Telegram, MAX (+авто-экспорт), Email, Yandex.Disk
```

---

## 14. Известные особенности и точки внимания

- **DI неоднороден:** ручная проводка в `main.go`/`router.go` против
  формального контейнера в `sms_plan`.
- **Состояние в RAM:** дислокация целиком в памяти — рестарт перезагружает её
  из `disl_actual`. Многоэтапная обработка тяжёлая, защищена контролем потери
  данных и проверками контекста.
- **Три порта зашиты на всех слоях** — модель (`PortType`), схема
  (`sms_plan_cache`), сервисы (`switch` по порту), парсеры (форматы/маппинги),
  фронт. Это главный предмет будущей универсализации.
- **Форматы входных файлов хрупкие:** парсеры опираются на тексты заголовков и
  «старый/новый» варианты форм; добавление нового предприятия = новый формат.
- **Секреты в env** — конфигурация содержит боевые токены/пароли;
  при форке проекта их следует вынести и ротировать.

---

*Документ — реконструкция по коду; отдельные внутренние детали могли остаться
за рамками. Обновлять при изменениях архитектуры. Следующий шаг —
`HARDCODE_INVENTORY.md`.*
