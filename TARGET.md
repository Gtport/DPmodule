# GTport Universal — Целевая модель (TARGET)

> Проект целевого состояния: во что превращается порт-специфичный хардкод из
> `HARDCODE_INVENTORY.md`, когда константы переезжают в БД. Документ описывает
> **сущности, схемы таблиц, связи** и **стратегию миграции** от текущего
> трёхпортового GTport к универсальному прототипу. DDL здесь — проектные
> эскизы (Postgres), не финальные миграции.
>
> Парные документы: `ARCHITECTURE.md` (как есть), `HARDCODE_INVENTORY.md` (что зашито).

---

## 0. Модель развёртывания: один клиент = один экземпляр

**Зафиксировано:** один клиент — один экземпляр приложения — одна БД — один
поток входных данных. У клиента может быть **несколько портов** (как в
оригинале их три), но всё в пределах клиента — единое хранилище. Мультиарендности
(нескольких клиентов в одном экземпляре) **нет** и не закладывается.

Следствия для архитектуры:

- **Единый `actualMap`.** Все вагоны клиента, независимо от порта назначения,
  лежат в одном `actualMap` (RAM). То же — одна БД дислокации и один
  `vagon_history`.
- **Порт — производный атрибут вагона, а не раздел хранилища.** Входной поток из
  АСУ содержит все вагоны; принадлежность порту определяется при обогащении
  (`naznach`/`gruzpol_s` из `marka`/станции). Отчёт по порту — это фильтр памяти
  по вторичным индексам (`NaznachMap`, `GruzpolMap`, `GruzpolSMap`), а не
  отдельное хранилище.
- **Почему именно так.** Раздельные `actualMap` по портам сломали бы две
  существующие функции: перенаправление вагонов между портами (вагон сменил бы
  хранилище) и совмещённые представления вроде «АЭ+ГУТ». Модель хранения уже
  порт-агностична — это правильно и менять не нужно.

**Вывод:** «несколько портов внутри клиента» **не меняет модель хранения**.
Универсализация касается только того, чтобы измерение «порт» (их число и
атрибуты) стало конфигурируемым, а не зашитым в `switch`-ветки. Поэтому
`enterprise_id` в схеме **не нужен** — ни в оперативных, ни в конфигурационных
таблицах. Роль «клиента» играет однострочная настроечная таблица (раздел 3.1).

---

## 1. Опора на существующее

Целевая модель **расширяет** то, что уже есть, не ломая:

- `ports` уже содержит: `code`, `name`, `name_s`, `okpo`, `location`, `at_work`,
  `pc_coal/pc_metal/pc_other/pc_total`, `front`, `color`, `param_s1/s2/n1/n2`.
  → расширяем недостающими полями.
- `max_chats` (БД) + `MaxChatRepository` → добавляем привязку к портам.
- `DirectoryCache` (RAM-кэш справочников) → расширяем новыми справочниками.
- Миграции = SQL-файлы в `migrations/`, выполняются при старте.

Принцип: **что было `switch`/`map` в коде — становится строками этих таблиц,
загружается в `DirectoryCache`, читается через реестр (см. раздел 4).**

---

## 2. Целевые сущности (обзор)

| Сущность | Заменяет (из инвентаря) | Статус |
|---|---|---|
| `client_settings` (синглтон) | базовые параметры клиента, часть `_env` | новая |
| `ports` (расширение) | 1.x, 2.1–2.3, 5.3, 6.1, косметика 4.2 | расширение |
| `port_views` + `port_view_members` | 3.1 (режимы «АЭ+ГУТ») | новые |
| `port_chats` | 4.1 (порт → чаты MAX) | новая |
| `cargo_split_profiles` + `cargo_split_buckets` | 2.1, 2.3, 6.2 (уголь/металл/чугун) | новые |
| `report_column_mappings` | 3.2, 4.3 (раскладка отчётов) | новая |
| `parser_profiles` (+ дочерние) | 5.1, 5.2 (форматы файлов) | новые, поздний этап |
| `data_source` | число/природа входных потоков (2 JSON + ЛК/ВГ + планы) | новая (§3.10) |
| `data_source_state` | «последняя загрузка» (была файлами на диске) | новая (§3.10) |
| `sms_plan_cache` (строковая модель) | 2.4 (колонки port_at/ut/gut) | миграция |

Нигде нет `enterprise_id` — все справочники принадлежат единственному клиенту БД.

---

## 3. Схемы таблиц (проектные эскизы)

### 3.1. `client_settings` — настроечная таблица (синглтон)

Однострочная таблица базовых параметров клиента. Типизированные поля + JSONB для
расширений без миграций.

```sql
CREATE TABLE client_settings (
    id                  INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1), -- синглтон
    client_name         TEXT NOT NULL,            -- 'GTport (3 порта)'
    -- timezone_offset УПРАЗДНЁН: часовых поясов нет (§3.11). Всё время — Московское
    --   naive; «сейчас» даёт единый clock-хелпер. Никаких смещений в БД.
    -- json_files_count УПРАЗДНЁН: производное от data_source (§3.10),
    --   = count(*) WHERE category='dislocation' AND ingest='api_pull' AND enabled
    sync_enabled        BOOLEAN DEFAULT true,     -- было DISABLE_FILE_SYNC (инверсно)
    sync_schedule       TEXT,                     -- было FILE_SYNC_SCHEDULE
    data_loss_threshold NUMERIC DEFAULT 30,       -- сейчас зашитые 30%
    features            JSONB DEFAULT '{}',       -- {telegram:true, max:true, yandex:false}
    extra               JSONB DEFAULT '{}',       -- расширения без миграций
    updated_at          TIMESTAMP DEFAULT now()
);
```

**env vs `client_settings` — что где хранить.** Принцип разделения:

- **env (на развёртывание, секреты):** строки подключения к БД, `JWT_SECRET`,
  токены/ключи (ESAT, Telegram, MAX, Yandex, SMTP-пароль), сертификаты.
  Секретам не место в БД.
- **`client_settings` (бизнес-параметры, админ меняет без редеплоя):** имя
  клиента, расписание синка, флаги интеграций, пороги приёма (`ingest_policy`),
  набор фич. (Смещения часового пояса нет — §3.11; число файлов — производное
  от `data_source`, §3.10.)

Сейчас многое из второй категории сидит в `_env` (`FILE_SYNC_SCHEDULE`,
`DISABLE_FILE_SYNC`, `AUTO_EXPORT_*`) — это кандидаты на переезд в таблицу.
Правило для спорных случаев: **env задаёт baseline по умолчанию,
`client_settings` переопределяет при наличии значения.**

> Замечание про `json_files_count`: число входных файлов во многом **следствие
> списка портов** (на порт/назначение — свой файл). Часть выводится из `ports`;
> в `client_settings` остаётся то, что не сводится к портам (общий лимит,
> поведение синка).

Загружается один раз при старте в конфиг-структуру (рядом с env), кэшируется,
перечитывается по запросу админа.

### 3.2. `ports` — расширение существующей таблицы

Добавляемые поля (через `ALTER TABLE ... ADD COLUMN IF NOT EXISTS`):

```sql
ALTER TABLE ports
    ADD COLUMN IF NOT EXISTS naznach        TEXT,    -- 'АЭ' / 'УТ-1' / 'ГУТ-2'
    ADD COLUMN IF NOT EXISTS display_name   TEXT,    -- 'Аттис' для АЭ
    ADD COLUMN IF NOT EXISTS short_name     TEXT,    -- 'АТТИС'
    ADD COLUMN IF NOT EXISTS alternate_name TEXT,    -- 'НМТП' (getAlternatePortName)
    ADD COLUMN IF NOT EXISTS plan_type      TEXT,    -- 'ma' / 'nk'
    ADD COLUMN IF NOT EXISTS file_code      TEXT,    -- 'AT'/'NMTP'/'TA'/'PZ' (имена файлов)
    ADD COLUMN IF NOT EXISTS unload_speed   NUMERIC, -- SpeedNMTP_Nk и т.п.
    ADD COLUMN IF NOT EXISTS cargo_split_profile_id INTEGER
        REFERENCES cargo_split_profiles(id),
    ADD COLUMN IF NOT EXISTS sort_order     INTEGER DEFAULT 0,
    ADD COLUMN IF NOT EXISTS is_active      BOOLEAN DEFAULT true;
```

> `code` и `color` уже есть. Теперь одна строка `ports` хранит **все** три
> «координаты» порта (`code`/`naznach`/`display_name`) и его атрибуты —
> устраняет тройное соответствие из инвентаря (п. 1.2). `enterprise_id` не нужен.

### 3.3. `port_views` — совмещённые представления

Заменяет режимы вроде «АЭ+ГУТ» (`GetGtPortSnapshot`, `filterTrainsByPort`):

```sql
CREATE TABLE port_views (
    id    SERIAL PRIMARY KEY,
    code  TEXT NOT NULL UNIQUE,   -- 'ae_gut'
    name  TEXT NOT NULL           -- 'АЭ+ГУТ'
);

CREATE TABLE port_view_members (
    view_id  INTEGER REFERENCES port_views(id) ON DELETE CASCADE,
    port_id  INTEGER REFERENCES ports(id),
    PRIMARY KEY (view_id, port_id)
);
```

### 3.4. `port_chats` — привязка порт → чаты MAX

`max_chats` (имена/ID чатов) остаётся; добавляем связь и роль чата:

```sql
CREATE TABLE port_chats (
    id         SERIAL PRIMARY KEY,
    port_id    INTEGER REFERENCES ports(id),
    chat_name  TEXT NOT NULL REFERENCES max_chats(name),
    chat_role  TEXT NOT NULL DEFAULT 'main',  -- 'main' | 'oper'
    UNIQUE (port_id, chat_name, chat_role)
);
```

> Убирает дублирование `MAX_CHATS_CONFIG` из 5 фронт-файлов: фронт получает
> привязку с API. Спец-ключи `all`/`MARIS` моделируются как `port_views` или
> отдельные именованные привязки.

### 3.5. `cargo_split_profiles` + `cargo_split_buckets`

Обобщает «уголь/металл/чугун» (зашитое как особенность ГУТ):

```sql
CREATE TABLE cargo_split_profiles (
    id    SERIAL PRIMARY KEY,
    code  TEXT NOT NULL UNIQUE,   -- 'metal_split' | 'single'
    name  TEXT NOT NULL
);

CREATE TABLE cargo_split_buckets (
    id                 SERIAL PRIMARY KEY,
    profile_id         INTEGER REFERENCES cargo_split_profiles(id) ON DELETE CASCADE,
    code               TEXT NOT NULL,   -- 'coal' | 'metal' | 'chugun'
    name               TEXT NOT NULL,   -- 'УГОЛЬ' | 'МЕТАЛЛ' | 'ЧУГУН'
    match_cargo_group  TEXT,            -- значение cargo_group, попадающее в bucket
    sort_order         INTEGER DEFAULT 0
);
```

- Порт АТ/УТ → профиль `single` (один bucket, без разбивки).
- Порт ГУТ → профиль `metal_split` (buckets coal/metal/chugun).
- `GetPribStats` и `processGUTData/processStandardPortData` читают профиль из
  порта вместо `switch naznach`.

> Замечание по 6.2: жёстко названные JSON-поля модели cargowork
> (`Coal*`/`Metal*`/`Chugun*`) — отдельная, более глубокая задача. На этапе
> прототипа профиль управляет **логикой разбивки**; рефактор самих полей модели
> в обобщённую структуру — кандидат на потом, помечен 🔴.

### 3.6. `report_column_mappings` — раскладка отчётов

Заменяет `getNmtpSprav1Mapping` (backend) и `sprav1Mapping*` (frontend) —
единый источник:

```sql
CREATE TABLE report_column_mappings (
    id            SERIAL PRIMARY KEY,
    port_id       INTEGER REFERENCES ports(id),
    report_type   TEXT NOT NULL,        -- 'nmtp' и т.п.
    sprav_id      INTEGER NOT NULL,     -- ключ sprav_1
    group_name    TEXT,                 -- 'СУЭК' | 'НОВОКУЗНЕЦК' | 'ЭЛЬГА'...
    column_index  INTEGER NOT NULL,
    name          TEXT,                 -- 'Чегдомын,\nНовая Чара,\nТаксимо'
    comment       TEXT,                 -- 'Г' | 'Д' | 'ДОМСШ'...
    display_name  TEXT,
    sort_order    INTEGER DEFAULT 0
);
CREATE INDEX ON report_column_mappings (port_id, report_type);
```

> Объёмный по числу строк, но прямолинейный перенос. API отдаёт это и
> backend-рендереру, и фронту — дубль устранён.

### 3.7. `sms_plan_cache` — миграция на строковую модель

Было: колонки `port_at`, `port_ut`, `port_gut`. Стало:

```sql
CREATE TABLE sms_plan_cache_v2 (
    calculation_date DATE NOT NULL,
    port_code        TEXT NOT NULL,
    data             JSONB NOT NULL DEFAULT '{}',
    created_at       TIMESTAMP DEFAULT now(),
    updated_at       TIMESTAMP DEFAULT now(),
    expires_at       TIMESTAMP,
    PRIMARY KEY (calculation_date, port_code)
);
```

`SmsPlanCacheRepository` упрощается: `switch port → колонка` исчезает, UPSERT
идёт по `(calculation_date, port_code)`. Схема перестаёт «знать» число портов.

### 3.8. `parser_profiles` (поздний этап, эскиз)

Самая сложная часть — конфигурация форматов входных файлов. Эскиз структуры:

```sql
CREATE TABLE parser_profiles (
    id            SERIAL PRIMARY KEY,
    kind          TEXT NOT NULL,    -- 'lk' | 'plan_ma' | 'plan_nk' | 'asu_json'
    variant       TEXT,             -- 'old' | 'new'
    header_marker TEXT,             -- 'Номер вагона' / 'План на'
    settings      JSONB DEFAULT '{}'
);

CREATE TABLE parser_column_map (   -- заголовок → внутреннее поле
    profile_id   INTEGER REFERENCES parser_profiles(id) ON DELETE CASCADE,
    field        TEXT NOT NULL,     -- 'vagon_c'
    header_text  TEXT NOT NULL,     -- 'Номер вагона'
    match_type   TEXT DEFAULT 'exact'  -- 'exact' | 'partial'
);

CREATE TABLE parser_synonyms (     -- синонимы станций (NK)
    profile_id  INTEGER REFERENCES parser_profiles(id) ON DELETE CASCADE,
    source      TEXT NOT NULL,
    target      TEXT NOT NULL
);
```

> Цель — превратить ветки `_old`/`_new` и зашитые словари `exactMatches`/
> `partialMatches`/синонимы в данные. Парсер становится конфигурируемым движком.
> **Реализуется в последнюю очередь** — после проверки модели на новом наборе
> портов.

### 3.9. Пороги приёма (temporal guardrails) — перенести с фронта на бэк

⚠️ **Важно при переносе фронта.** В GTport проверки временной согласованности
при загрузке дислокации живут во **фронтенде** (`client/src/components/`), а не в
бэке. Раз фронт переписываем заново — эти бизнес-правила легко потерять. Бэк
сейчас стережёт только наличие файлов, их парсинг и свежесть по каждому источнику
(remote новее local); **проверки «разрыва по времени» между файлами на бэке нет**,
а `parse_lk.go` при нескольких файлах — просто конкатенация без сверки окна.

Правила «как есть» (все — во фронте; жёсткая только про откат на старое):

| Правило | Порог | Источник в GTport | Тип |
|---|---|---|---|
| Разрыв между файлами (спред max−min времени формирования при 2+ файлах) | **15 мин** | `LKManager2.tsx:463` | мягкая (подтверждение) |
| Устаревание относительно времени сервера | **60 мин** | `LKManager2.tsx:502` | мягкая |
| Файл старше текущей дислокации (сравнение по терминалу: AT↔`d_attis`, NMTP↔`d_nmtp`) | — | `LKManager2.tsx:221,375` | **жёсткая** (не-админ); админу предупреждение |
| Дислокация слишком стара для плана (план позже самой старой из двух дислокаций) | **1 час** | `FileUploader.tsx:247` | жёсткая (только планы ma/nk/rb) |

**Целевое решение:** перенести эти проверки в **бэкенд, в слой приёма** (перед
слиянием в `ProcessDislocation`), а пороги сделать данными — полями профиля
источника/приёма (`data_source`/`parser_profiles.settings` JSONB), не хардкодом:

```jsonc
// settings профиля приёма
{
  "max_gap_minutes": 15,          // макс. разрыв между файлами одной загрузки
  "max_staleness_minutes": 60,    // устаревание относительно "сейчас"
  "reject_older_than_current": true,   // запрет отката на старую дислокацию…
  "reject_older_role_exempt": "administrator",  // …кроме этой роли (предупреждение)
  "plan_max_lag_hours": 1         // для плановых файлов
}
```

Плюс это чинит текущую дыру: жёсткие проверки, что живут только в UI, сейчас
обходятся запросом мимо фронта. Реализуется вместе со слоем приёма (после
`data_source`), не в парсере.

### 3.10. `data_source` — реестр каналов ввода (ключевая для универсальности)

Сейчас в GTport число и природа входных потоков **зашиты** (два JSON `at`/`nmtp`
из ESAT, Excel ЛК/ВГ, три плана). Чтобы предприятие настраивалось «под себя»
(один поток или несколько) **без правок кода**, каждый поток описывается строкой
`data_source`. Движок приёма читает таблицу и не содержит ни одного зашитого
`at`/`ma`/«Личный кабинет».

**Три слоя (не путать и не дублировать):**

```
data_source     → КАНАЛ:        откуда берём, когда, чем валидируем   (транспорт/приём)
   ├─ parser_profile_id → ФОРМАТ:      как читать байты (колонки, header, variant)  [§3.8]
   └─ (записи → порт)   → ИДЕНТИЧНОСТЬ: station/ОКПО → порт через справочники       [§3.2/§4]
```

`data_source` **ссылается** на `parser_profiles` (§3.8), а не поглощает его: один
формат («lk_new») может переиспользоваться несколькими каналами. Привязка записи
к порту — не здесь, а по данным (код станции, ОКПО) через реестр портов.

```sql
CREATE TABLE data_source (
    id                TEXT PRIMARY KEY,            -- 'json_at', 'lk', 'plan_ma'
    name              TEXT NOT NULL,               -- 'Аттис (JSON, ESAT)'
    enabled           BOOLEAN NOT NULL DEFAULT true,
    ingest            TEXT NOT NULL,               -- 'api_pull' | 'upload' (расширяемо: sftp, folder_watch)
    category          TEXT NOT NULL,               -- 'dislocation' | 'plan' | 'tech_state' | 'operation'
    parser_profile_id INTEGER REFERENCES parser_profiles(id),  -- формат (§3.8); profiles.kind = тип парсера
    config            JSONB NOT NULL DEFAULT '{}', -- транспорт + пер-файловая валидация
    sort_order        INTEGER NOT NULL DEFAULT 0,
    created_at        TIMESTAMP NOT NULL DEFAULT now(),
    updated_at        TIMESTAMP NOT NULL DEFAULT now()
);
```

> Смещения времени (`tz_offset_min`/`timezone_offset`) в модели **нет намеренно**:
> часовые пояса запрещены (§3.11). Данные приходят в Московском времени и хранятся
> как есть; «свежесть» считается против «сейчас по Москве» из clock-хелпера.

**`config` JSONB — контракт по типу приёма** (секреты только ссылками в env):

```jsonc
// api_pull v2 — HTTP-ручка на порт; метка формирования и count в ЗАГОЛОВКАХ ответа
{ "provider":"esat",
  "endpoint_ref":"env:ESAT_AT_URL",                 // своя ручка на каждый порт (Аттис/НМТП/любой)
  "auth":{"header":"X-API-Key","key_ref":"env:ESAT_API_KEY"},
  "success_field":"status", "success_value":"success",
  "headers":{ "count":"X-Count", "formation_ts":"X-Timestamp" }, // имена реальных заголовков — уточнить по API
  "min_body_kb":200, "max_body_mb":30 }

// upload (ЛК)
{ "detect":["Личный кабинет"],
  "subtype_marker":{"Дислокация вагонов":"lk","Техническое состояние":"vg"},
  "allowed_ext":["xlsx","xls"], "max_mb":10 }
```

> Переход с файлов на HTTP: `filename_regex`, `require_newer_than_local` и вся
> механика скачивания/rename **уходят**. Метка «когда сформировано» и `count`
> берутся из **HTTP-заголовков** ответа (`headers`), а не из имени файла и не из
> тела — значит свежесть и потерю данных можно проверить **до** парсинга тела, а
> число распарсенных записей сверить с `count` (integrity-check). «Разные ручки для
> разных портов» = разные строки `data_source`, код общий.

**Память о предыдущей выгрузке — `data_source_state`.** Раньше «что уже загружено»
помнили файлы на диске (`findNewestLocalFile`). Файлов нет → нужна явная память
последней принятой выгрузки на источник. Храним **метаданные** (метка + count), а не
сам прошлый JSON: сама предыдущая дислокация лежит снимком в `disl_actual`.

```sql
CREATE TABLE data_source_state (
    data_source_id    TEXT PRIMARY KEY REFERENCES data_source(id) ON DELETE CASCADE,
    last_formation_ts TIMESTAMP,   -- метка из заголовка последней ПРИНЯТОЙ выгрузки (Московское naive)
    last_count        INTEGER,     -- count из заголовка последней принятой
    last_success_at   TIMESTAMP,   -- когда приняли (clock.Now())
    last_status       TEXT,        -- 'ok' | 'skipped' | 'error'
    last_error        TEXT
);
```

> Отдельно от `data_source` намеренно: `data_source` — декларативный конфиг
> (сидируется, правит админ, идёт в git-сид), а это — изменчивое runtime-состояние
> (пишется каждую синхронизацию). Смешивать нельзя — сид затрёт состояние.
>
> Как используется (замена файловой логики): «новее?» = `header.ts >
> last_formation_ts` иначе `skipped`; потеря данных = падение `header.count` vs
> `last_count` ≥ `max_data_loss_pct` (до парсинга); разрыв/устаревание — из
> `ingest_policy` против «сейчас по Москве» (§3.11).

**Связь с существующими сущностями:**

- `client_settings.json_files_count` становится **производным** = число `enabled`
  источников `category='dislocation' AND ingest='api_pull'` (сейчас 2: at+nmtp).
  Как хранимое поле — упраздняем (см. правку §3.1).
- `ports.file_code` ('AT'/'NMTP') сейчас смешивает «имя файла» и «идентичность
  порта»: regex/имя файла уходит в `data_source`, `ports.file_code` остаётся лишь
  для сопоставления «данные → порт».
- Профиль парсера ЛК из кода (`parser.SourceProfile`: `HeaderMarker`,
  `DateCutoffHour`) ложится в **`parser_profiles`** (`header_marker` +
  `settings.date_cutoff_hour`), а `data_source` на него ссылается. То есть уже
  реализованный `SourceProfile` — это прото-строка `parser_profiles`, не
  `data_source`.

**Пороги приёма (§3.9) — разделение по природе:**

- пер-файловые (размер, устаревание файла, «старше текущей дислокации своего
  терминала») → `data_source.config`;
- межфайловые/на загрузку целиком (разрыв 15 мин между файлами одной загрузки,
  «план на 1ч позже дислокации») → **`client_settings`** (JSONB `ingest_policy`
  по категориям) — это про отношение источников, одному каналу не принадлежит.

```jsonc
// client_settings.extra.ingest_policy
{ "dislocation": { "max_gap_minutes":15, "max_staleness_minutes":60,
                   "reject_older_than_current":true, "reject_older_role_exempt":"administrator",
                   "max_data_loss_pct":30 },   // порог потери данных (был хардкод 30 в 3 местах)
  "plan":        { "plan_max_lag_hours":1 } }
```

> `max_data_loss_pct` — глобальный порог: обновление отклоняется, если новый набор
> меньше текущего снимка на ≥ N%. В GTport зашито `30` (продублировано в Stage 0/3/4).

Сид текущего клиента — в разделе 5.

### 3.11. Время: Московское naive, без часовых поясов (жёсткий инвариант)

**Правило (не нарушать):** всё время — в Excel, JSON, в БД, в API — трактуется как
**Московское без указания часового пояса**. Приложение **не использует часовые
пояса** и **не корректирует** время: отдаём ровно так, как пришло. Тип — наш
`LocalTime` (без `Z`, `timestamp without time zone`).

- **«Сейчас» — только по Москве.** Любое сравнение с текущим временем (свежесть,
  устаревание, «сегодня», окна) берёт «сейчас» из **единого clock-хелпера**
  `clock.Now()`. Это **единственное** место, где допустим `LoadLocation("Europe/Moscow")`;
  хелпер возвращает `LocalTime` (naive). Весь остальной код — TZ-free и не зависит
  от часового пояса сервера (VPS во Франкфурте ≠ Москва).
- **Запрещено в бизнес-коде:** `time.LoadLocation`, `time.FixedZone`, `.In(...)`,
  `time.Now().UTC()`, сдвиги-«корректировки» (`Add(±7h/±10h)`), `gocron` не в MSK.
  В GTport этого много (`GetVladivostokTime`=UTC+10, `+7`/`+10`, `Asia/Vladivostok`) —
  для нас это **список «не переносить»**, а не образец.
- **`CreatedAt/UpdatedAt`** и прочие «сейчас»-штампы — через `clock.Now()`, не
  через голый `time.Now()` (иначе получим время сервера).
- **Метка формирования выгрузки** — из тела ответа (`data_source.config.formation_ts_path`),
  как есть, без сдвигов; не из имени файла.
- **Бизнес-правило «час ≥ 18 → +1 сутки»** (дата рейса `DateNach`, стабильность ID) —
  это **не** часовой пояс, а операционные сутки; **остаётся** (допустимо хардкодом,
  сейчас — `DateCutoffHour` в профиле парсера). Единственное сохраняемое «правило дня».

Наш greenfield уже соответствует (LocalTime без `Z`; `+7`/`+10`/`LoadLocation` не
переносились). Остаётся ввести `clock.Now()` и провести через него штампы времени.

### 3.12. Свод модели настроек (по анализу `ports.csv` + пайплайна) — уточняет §3.2/§3.9/§3.10

Разбор реальной выгрузки `ports` (`new_go/ports.csv`) и пайплайна обогащения
(`enrich_stage1..4`) уточнил модель. **Шесть слоёв, каждый — одна ось изменчивости:**

| Слой | Что настраивает | Статус |
|---|---|---|
| `ports` (расширить, §3.2) | идентичность порта/терминала + физика (`pc_*`, план-код, фронт) | миграция-дельта |
| `data_source` (§3.10) | канал/транспорт потока; `co_arrival_group` | есть, доработать |
| `route_speed` (новая) | скорости по участкам до станции назначения, ключ `(станция, is_bam)` | спроектировать |
| `stations` (справочник) | код→название+дорога+расстояние (обогащение до идентификации) | есть (DirectoryCache) |
| `client_settings` (§3.1/§3.9) | общепрограмное: `ingest_policy`, `min_vagon_count`, penalty | есть, расширить |
| `parser_profiles` (§3.8) | формат: колонка/поле → модель; `date_cutoff_hour` | эскиз |

**Идентификация — по составному ключу `(ОКПО грузополучателя + станция назначения текстом)`.**
ОКПО **не уникален**: `1126022` (АО «Находкинский МТП») → и `ГУТ-2` (Мыс Астафьева),
и `УТ-1` (Находка). В greenfield индекс `ports(okpo, location)` это уже закладывает,
в gtlogic — `GetPortByCompositeKey(okpo, stanNazn)`. Порядок: `код станции → название
(справочник stations) → (название + ОКПО) → порт`.
→ **Следствие:** `okpo_map` в `data_source.config` — тупиковый (ОКПО→порт неоднозначен)
и **удаляется**; идентичность живёт только в `ports`. Контроль ЛК «чей файл» (Слайс 2/3)
переводится с `okpo_map` на этот путь.

**Потоки и парадокс совместного среза.** `data_source` = 1 поток РЖД = 1 ОКПО/юр.лицо
→ 1..N портов (Аттис→АЭ; Находкинский→УТ-1+ГУТ-2). В одном физпоезде — вагоны разных
юр.лиц; при разном временном срезе один поезд в двух потоках «стоит на разных станциях».
Добавляем `data_source.co_arrival_group TEXT`; правило разрыва `max_gap` (§3.9) применяется
**между** источниками одной группы — это межпотоковое, каналу не принадлежит.

**`ports` — дельта к фактической 000002** (уточняет эскиз §3.2). Сейчас `dpport.ports` =
`id, okpo, location, organisation, name_s, name, code` + `ix(okpo, location)`. Для слоя
настроек/физики добавляем:

```sql
ALTER TABLE dpport.ports
    ADD COLUMN plan_code    text    DEFAULT '',   -- param_s1: 'ma'/'nk'/'rb' (тип плана подвода)
    ADD COLUMN station_code text    DEFAULT '',   -- param_s2: код причальной станции
    ADD COLUMN pc_coal      integer,              -- перераб. способность, ваг/сут, уголь
    ADD COLUMN pc_metal     integer,              -- ... металл
    ADD COLUMN pc_other     integer,              -- ... прочее
    ADD COLUMN pc_total     integer,              -- суммарно
    ADD COLUMN front        integer,              -- фронт выгрузки
    ADD COLUMN color        text    DEFAULT '',   -- цвет отображения
    ADD COLUMN enabled      boolean DEFAULT true,  -- at_work
    ADD COLUMN sort_order   integer DEFAULT 0;
```

> Способность — **по роду груза** (`pc_coal/pc_metal/pc_other`), а не одиночный
> `unload_speed` из эскиза §3.2: этого требует формула интервалов ниже.

**Интервалы между поездами (Stage 4) — НЕ храним, считаем из `pc_*`.**
`interval_h = вагонов_в_поезде × 24 / pc_рода`. Проверено на `ports.csv` — совпадает
с хардкодом `enrich_stage4.go`:

| Порт / род | вагонов | pc/сут | расчёт | хардкод |
|---|---|---|---|---|
| УТ-1 | 72 | 432 | 72÷(432/24) | **4 ч** ✓ |
| АЭ | 63 | 144 | 63÷(144/24) | **10.5 ч** ✓ |
| ГУТ-2 уголь | ~74 | 170 | 74÷(170/24) | **≈10.5 ч** ✓ |
| ГУТ-2 металл | 60 | 90 | 60÷(90/24) | **16 ч** ✓ |

Разброс интервалов — это просто разные `pc` и размеры поездов; отдельного хранения не
нужно. **Общий путь приёма** (АЭ+ГУТ-2 конкурируют за нитки, УТ-1 независим) — производное
от **общей станции** (`location`), а не отдельный флаг.

**`route_speed` — новая таблица.** Заменяет хардкод скоростей `enrich_stage2.go`
(пороги остатка 1364/911 км; `StationNach`: УЛАК 20/20, ЧЕГДОМЫН 17/17, деф. 34/30/27,
последний участок 27). Новый параметр `is_bam` — альт.маршрут вагона со своими скоростями
на своих интервалах.

```sql
CREATE TABLE dpport.route_speed (
    id           bigserial PRIMARY KEY,
    station_nach text    NOT NULL,             -- станция отправления (по ней выбор набора)
    is_bam       boolean NOT NULL DEFAULT false,
    from_km      integer NOT NULL,             -- нижняя граница участка (км ДО назначения)
    speed        numeric NOT NULL,             -- км/ч на участке
    UNIQUE (station_nach, is_bam, from_km)
);
-- выбор: строка с наибольшим from_km ≤ остаток_расстояния.
-- default_to_go_h (=72), min_vagon_count (=10) — общепрограмные, в client_settings.
```

**Общепрограмные константы → `client_settings.extra`** (расширяет §3.1/§3.9):
`min_vagon_count`(=10), `bros_penalty_h`(=72), `default_to_go_h`(=72), пороги простоя
(`prost_dn≥1`/`prost_ch≥12`). Правило **«час ≥ 18 → +1 сутки»** остаётся в профиле
парсера (`date_cutoff_hour`, §3.10/§3.11) — это операционные сутки, не TZ; **не** переносим.

### 3.13. Статусы дислокации (Stage 1b) — ревизия правил gtlogic

Обогащение Stage 1 (перенос gtlogic `calculateDerivedFields`): после имён станций
(Stage 1a) на каждой записи считаются производные поля. Правила статусов **изменены**
относительно gtlogic (добавлены 6/9/12, у 10 — условие `date_prib`). Порядок — первое
совпадение выигрывает; **порожний признак проверяется первым**:

```
порожний (porozh_priznak == "1"):
    station_oper == stan_nazn (оба непусты)   → 12   порожний в порту (date_kon = time_op)
    иначе                                     →  6   порожний в пути (ВЫШЕ 0/1/4/5)
гружёный:
    station_oper == stan_nazn (оба непусты):
        date_prib непусто                     → 10   прибыл (date_kon = date_op_jd)
        иначе                                 →  9   кандидат в прибывшие → отд. таблица (отд. слайс)
    иначе:
        code_station_nach == code_station_oper:
            index == "Б/И"                    →  0
            иначе                             →  1
        code_oper == "92"                     →  5   брошен (id_status5 = ключ агрегации)
        station_oper ∉ {назн, отпр} и code_oper≠92
            и (prost_dn ≥ prost_dn_min  или  prost_ch ≥ prost_ch_min) → 4  (id_status4 = ключ)
        иначе                                 →  2   в пути
```

Прочие производные поля:
- `date_op` = дата из `time_op`; `date_op_jd` = `time_op` (+1 сутки если час ≥ `date_cutoff_hour`).
- `date_kon`: `10 → date_op_jd`; иначе (включая 12 — выгружен в порту) `→ time_op`.
- `delay` = просрочка по `date_dostav` в сутках (по «сейчас» МСК из `clock.Now()`).
- `id_disl` = `index / code_station_oper / oper_s / date_op(ДД.ММ.ГГГГ)` (непустые).
- **`id_status5`/`id_status4`** = ключ агрегации `index|code_station_oper|time_op`
  (перенос gtlogic `createBrosKey`/`Param_1`): `id_status5` для статуса 5, `id_status4`
  для статуса 4. Пусто, если нет любого компонента. Сами подсистемы учёта (таблица
  `bros`-аналог для 4/5, таблица кандидатов для 9) — **отдельными слайсами**.

Пороги `prost_dn_min=1`, `prost_ch_min=12` — в `client_settings.extra.status`.
`is_bam`/`AlternativeMove` на статус **не** влияет (только на скорости, Stage 2/4).

### 3.14. Кандидаты в прибытие — таблица `status9` (Stage 2)

Расширение относительно gtport: вагоны-кандидаты на прибытие выносятся в отдельную
таблицу `status9` (полная копия колонок `dislocation`, ключ — `vagon`), где ждут
подтверждения оператором. Два типа (колонка `status`):

- **9 — «живой кандидат»:** вагон в новом батче на станции назначения, гружёный,
  `date_prib` пусто (Stage 1 дал 9). **Остаётся и в снимке** `dislocation` (в сборном
  поезде мог поехать дальше — прибытие ещё не факт). Пишем при **первом появлении**
  (в актуальной мапе статус ∉ {9}); `InsertNew` не перезаписывает существующего
  кандидата (сохраняет операторские правки). Удаляется: оператором (отклонение) или
  **авто при смене статуса** в след. цикле (вагон в батче со статусом ≠ 9 → снять).
- **8 — «пропавший»:** был в актуальной, исчез из батча (статус ≠ 6 — порожний в пути
  считаем выбывшим). Пишем **всех** (невзирая на план `IndexPp`/`PlanMsk`) как копию
  актуальной записи + `Status=8`. **В снимок НЕ идёт.** Через UI **не удаляется**
  (защита от тех.ошибок РЖД): либо оператор подтверждает прибытие, либо вагон снова
  появляется в дислокации → авто-удаление. Статус-9 кандидат при собственной пропаже
  переводится в 8. Наполнение статусом 8 — под-слайс S2-1b.

**Разрешение (позже):** оператор в UI видит кандидатов, правит поля прибытия,
подтверждает → запись идёт в `vagon_history` со статусом 10 → удаляется из `status9`.
«Вечные» статус-8 (вагон выбыл навсегда) чистит **администратор** (не оператор);
для этого в таблице есть `created_at`/`updated_at`.

Статус 8 — артефакт **Stage 2** (для таблицы кандидатов), в дерево статусов §3.13
(`computeStatus` по новому батчу) не входит и в снимок не попадает.

### 3.15. Carry-over из актуального снимка (Stage 2, S2-2)

Перенос `enrichFromActual` из gtport: для вагона, найденного в актуальном снимке
(`ActualCache.FindVagonInActual`), новая запись обогащается данными из прошлого
цикла. Идёт ПОСЛЕ Stage 1 и ДО `reconcileCandidates` (может держать статус 4/5).
Marka — отдельный шаг S2-3.

- **Координаты** — из актуальной, если в новой пусты/нулевые.
- **Sticky 4/5** — пока станция операции та же, держим статус (брошен/долгий простой);
  смена станции → снятие в S2-4 (`stopBros`/`stopStop`).
- **Ветвление по статусу актуальной:**
  - `= 10` (прибыл) → **полная замена** на актуальную (`Index = IndexPp`), кроме
    свежего `prost_dn` и новых полей (если в актуальной пусто); `invoice_main`/
    `created_at` — из актуальной (стабильны);
  - `≠ 10` → **выборочный перенос**: `ID`, `index_main`/`index_last`
    (`determineIndex*`), `index_pp`, `gruzpol`/`gruzpolS`/`naznach`, `plan_jd/msk`,
    груз-поля (если `Gruzotpr` в актуальной непуст), `param1-3`, `invoice_main`
    стабилен; + `fixZeroRasstStanNazn` (нулевое расстояние при `StanNazn ≠
    StationOper` → берём из актуальной).
- **Новые поля** (`CarOwner*`/`CarTenant*`/`GtdNumber`/`FreightExactName`/`Zayavka`,
  которых не было в gtport) — **всегда из актуальной, если там непусто** (важно для
  запасного ЛК, где эти поля не приходят — не теряем полученное из основного JSON).
- **Новый вагон** (нет в актуальной) → первичная установка `index_main = index_last =
  index`, `invoice_main = invoice` (`initNewVagon`); груз — marka в S2-3.
- `created_at` из актуальной (первое появление вагона), `updated_at = clock.Now()`.

`determineIndexMain`: у актуальной пусто/`Б/И` → текущий `index`, иначе актуальный
(родительский индекс фиксируется). `determineIndexLast`: отслеживает предыдущий индекс.

### 3.16. Доноры перегруза — таблица `status6` (Stage 2)

Расширение (в gtport нет). **Сценарий перегруза:** вагон с грузом доехал до
промежуточной станции, сломался, груз перегрузили в другой вагон. Первый вагон
становится **порожним** (статус 6, к нам не доедет). Позже появляется вагон-приёмник
со станцией погрузки = станции поломки, но `marka` этот груз не знает (станция не наш
отправитель) — тогда данные груза берём у **донора** из `status6`.

- **Запись** (этот слайс) — при **переходе на статус 6** (в новом батче `6`, в
  актуальной вагон есть и `status ≠ 6`; новый вагон сразу 6 — НЕ фиксируем). Копия
  вагона в `status6` (`LIKE dislocation`, ключ `vagon`) хранит полную запись — для
  матча (`code_station_oper` + вес + срок доставки) и передачи груза. Идёт ПОСЛЕ
  carry-over (у записи полные данные) и ДО подмены снимка.
- **Обнуление:** `gruzpol_s = "0"`, `naznach = "0"` — **только в снимке** (вагон к нам
  не доедет, в выборки не попадает). В самой записи `status6` эти поля хранятся
  **реальными** — они нужны при передаче приёмнику (S2-3, §3.17). Груз-поля
  (`cargo_*`, вес, срок) сохранены всегда.
- **Матч-донорство** (S2-3, §3.17): новый вагон, для которого marka не нашла груз.
- **Удаление:** после использования донором (S2-3) + админ-очистка «зависших»
  (`created_at`/`updated_at`). При смене статуса самого донора запись НЕ удаляется.

---

### 3.17. Обогащение новых вагонов (Stage 2, S2-3)

Отдельный шаг **после** carry-over (§3.15): заполняет груз/назначение у **новых**
вагонов и у существующих с пустым грузом. Три независимых механизма + переименование.

**Поле `peregruz`** (бывш. `info_3`): переименовано во всех таблицах с `LIKE
dislocation` (`dislocation`, `vagon_history`, `status6`, `status9`) — семантика
«вагон получил груз перегрузом от донора». Правило для будущих запросов к
`vagon_history`: при выборке **«погруженные вагоны за дату»** записи с непустым
`peregruz` **исключать** (перегруз ≠ фактическая погрузка).

**Механизм 1 — `marka`.** Ключ `(gruzotpr_okpo, code_station_nach, code_cargo)`.
Заполняет груз-поля (`gruzotpr, cargo_s, cargo_sms, cargo_group, client, sms_1..3,
sprav_1..3, color`). Порядок поиска: **полное** совпадение по ключу → иначе, если ОКПО
в `marka` отсутствует, **частичное** по `(станция+груз)`. Нет совпадения → «нет марки»
(лог/статистика; уведомления — позже). Одна унифицированная функция для новых и для
существующих-с-пустым-грузом. Возвращает «заполнен ли груз» — для перехода к донорству.

**Механизм 2 — `naznach` (настроечная таблица `naznach_station`).** Поле `naznach`
(«фактическое назначение», площадка внутри порта) по умолчанию = `gruzpol_s`. Таблица
разрешает «парадокс потоков»: физически один порт (напр. Мыс Астафьева = ГУТ-2), а
коммерческий поток зависит от **станции отправления**. Обобщено без хардкода имени:

```
dpport.naznach_station(
  id, dest_station, origin_station, naznach, univers bool, enabled bool,
  created_at, updated_at, UNIQUE(dest_station, origin_station))
```

Логика: `naznach = gruzpol_s`; если `(stan_nazn, station_nach)` есть в таблице
(enabled, непустой `naznach`) → берём оттуда. Пустая таблица → всегда `gruzpol_s`.
Любая станция назначения из таблицы включает перестановку (у каждой свой список).
Сид — в `_reference/seed/` (per-deployment). `sms_1/2` из выгрузки не тащим (метки
для модуля уведомлений); `univers` тащим.

**Механизм 3 — донорство `status6`.** Только если marka **не** дала груз новому
вагону. Ищем донора в `Status6Cache`: `donor.code_station_oper == new.code_station_nach`
**И** `|donor.ves − new.ves| ≤ 0.1` **И** `donor.date_dostav == new.date_dostav` (точно).
Совпали 3/3 → приёмник **наследует груз и назначение донора, оставаясь собой
физически**. Переносим: груз (`gruzotpr/gruzotpr_okpo/code_cargo/cargo_*/ves/client/
sms_*/sprav_*/color`), назначение (`gruzpol/gruzpol_s/naznach/code_stan_nazn/
code4_stan_nazn/stan_nazn`), отправление (`code_station_nach/station_nach/doroga_nach`),
`date_dostav`. **НЕ** переносим (остаётся приёмника): `vagon/id`, позицию
(`code_station_oper/station_oper/doroga_oper/lat/lon`), операцию (`code_oper/oper/
oper_s`), время/статус (`time_op/date_op/date_op_jd/status/prost_*/delay`), индексы,
накладные, таймстемпы. Номер вагона-донора → в `peregruz`. Использованного донора
**удаляем** из `status6` (RAM+БД).

**Порядок в конвейере** (`LKProcessor.Process`):
`applyCarryOver → S2-3 marka+naznach → applyStatus6Transition → S2-3 донорство →
reconcileCandidates → ReplaceActual`. Донорство **после** `applyStatus6Transition` —
чтобы донор, ставший порожним в этом же батче, был доступен приёмнику сразу.

---

### 3.18. Расчёт хода до порта (Stage 3, S2-5)

Построчный прогноз физического прибытия. Первый («лёгкий») слой прогноза; тяжёлый
Stage 4 (нитки порта: `plan_*`, `prog_*`, `mistake`) — отдельная подсистема позже.
Поля: `ToGo`, `RaschMsk`, `RaschJd`. Data-driven перенос `calculateToGo/RaschMsk/RaschJd`
из gtlogic (хардкод участков/скоростей → таблица `route_speed`, §3.12).

**Пропуск.** Для статусов **≥ 9** (9 кандидат в прибытие, 10 прибыл, 12 порожний в
порту) прогноз НЕ считаем — вагон уже в порту/у цели.

1. **`ToGo`** (часы хода):
   - `RasstStanNazn` nil/0 → `ToGo = 72.0` (дефолт 3 суток);
   - иначе идём по участкам профиля `route_speed(StationNach, isBam)` от дальнего к
     ближнему (сортировка по убыванию `FromKm`): `ToGo += (остаток − FromKm)/Speed`,
     `остаток = FromKm`. `isBam := alternative_move ≠ 0` — маркер альтернативного пути,
     проставляется на **Stage 1** (`enrichStations`) по станции **операции**
     (`stations.is_bam`, текущий участок маршрута); профиль не найден → дефолт 72.
     ⚠️ **Сознательное отклонение от gtlogic:** там скорость выбиралась по станции
     **отправления** (`switch StationNach`, весь путь по одной скорости). У нас —
     по текущему положению (на БАМ медленно, сошёл на магистраль — быстро). БАМ-профили
     `route_speed(is_bam=true)` и пометка `stations.is_bam` — отдельная настройка данных
     (без изменения кода); пока их нет, всё считается по `is_bam=false` (паритет gtlogic).
2. **`RaschMsk`** = `TimeOp + ToGo(ч) + ProstDn(сут) + ProstCh(ч)`; **+12 ч** если
   статус 0 (операционный буфер). Пустой/нулевой `TimeOp` → не считаем.
3. **`RaschJd`** = `RaschMsk`, **+24 ч** если час ≥ cutoff (те же операционные ЖД-сутки,
   что и `date_op_jd`).

**Место в конвейере:** после донорства S2-3 (перегруз-приёмник использует станцию
отправления донора для профиля скоростей) и ДО `reconcileCandidates`/подмены снимка.

---

## 4. Слой доступа: реестр портов (ключевой для рефакторинга)

Чтобы заменить десятки `switch port {...}`, нужен **один реестр**, который
кэширует справочники и отдаёт атрибуты порта. Расширяем существующий
`DirectoryCache`/`DirectoryService` либо вводим `PortRegistry`. Поскольку клиент
один, реестр глобален — без параметра предприятия.

```go
// Эскиз. Загружается при старте, как DirectoryCache.
type PortRegistry interface {
    ByCode(code string) (PortConfig, bool)        // 'at' → конфиг
    ByNaznach(naznach string) (PortConfig, bool)  // 'АЭ' → конфиг
    All() []PortConfig
    View(code string) (PortView, bool)            // 'ae_gut'
    Naznachs() []string                           // для SQL IN (...)
}

type PortConfig struct {
    Code, Naznach, DisplayName, ShortName, AlternateName string
    PlanType    string
    Color       string
    UnloadSpeed float64
    CargoSplit  CargoSplitProfile
    Chats       []PortChat
}
```

Тогда хардкод-вызовы превращаются в чтение:

```go
// БЫЛО:
switch port {
case models.PortAT: naznach = "АЭ"
case models.PortUT: naznach = "УТ-1"
case models.PortGUT: naznach = "ГУТ-2"
}

// СТАЛО:
pc, _ := registry.ByCode(string(port))
naznach := pc.Naznach
```

А SQL `IN ('АЭ','УТ-1','ГУТ-2')` строится из `registry.Naznachs()`.

---

## 5. Как текущие 3 порта ложатся на новую модель (seed)

Прод-данные GTport — это и есть единственный клиент. Сидовая миграция (без
`enterprise_id`):

```sql
-- настроечный синглтон (пороги приёма §3.9/§3.10; без timezone — §3.11)
INSERT INTO client_settings (id, client_name, extra)
VALUES (1, 'GTport (3 порта)',
  '{"ingest_policy":{"dislocation":{"max_gap_minutes":15,"max_staleness_minutes":60,
    "reject_older_than_current":true,"reject_older_role_exempt":"administrator",
    "max_data_loss_pct":30},
    "plan":{"plan_max_lag_hours":1}}}')
ON CONFLICT (id) DO NOTHING;

-- каналы ввода (§3.10). parser_profile_id проставляется на этапе парсеров.
-- Без смещений времени (§3.11); JSON — HTTP-ручка на порт, метка формирования в теле.
INSERT INTO data_source (id, name, ingest, category, config) VALUES
 ('json_at',  'Аттис (JSON, ESAT)',         'api_pull','dislocation',
   '{"provider":"esat","endpoint_ref":"env:ESAT_AT_URL","auth":{"header":"X-API-Key","key_ref":"env:ESAT_API_KEY"},"success_field":"status","success_value":"success","headers":{"count":"X-Count","formation_ts":"X-Timestamp"},"min_body_kb":200,"max_body_mb":30}'),
 ('json_nmtp','НМТП (JSON, ESAT)',          'api_pull','dislocation',
   '{"provider":"esat","endpoint_ref":"env:ESAT_NMTP_URL","auth":{"header":"X-API-Key","key_ref":"env:ESAT_API_KEY"},"success_field":"status","success_value":"success","headers":{"count":"X-Count","formation_ts":"X-Timestamp"},"min_body_kb":200,"max_body_mb":30}'),
 ('lk',       'Дислокация из ЛК РЖД',        'upload',  'dislocation',
   '{"detect":["Личный кабинет"],"subtype_marker":{"Дислокация вагонов":"lk"},"allowed_ext":["xlsx","xls"],"max_mb":10}'),
 ('vg',       'Техническое состояние (ЛК)',  'upload',  'tech_state',
   '{"detect":["Техническое состояние"],"allowed_ext":["xlsx","xls"],"max_mb":10}'),
 ('plan_ma',  'План подвода — Мыс Астафьева','upload',  'plan', '{"allowed_ext":["xlsx","xls"],"max_mb":10}'),
 ('plan_nk',  'План подвода — Находка',      'upload',  'plan', '{"allowed_ext":["xlsx","xls"],"max_mb":10}'),
 ('plan_rb',  'План подвода — Рыбники',      'upload',  'plan', '{"allowed_ext":["xlsx","xls"],"max_mb":10}')
ON CONFLICT (id) DO NOTHING;

-- проставить новые атрибуты существующим портам
UPDATE ports SET naznach='АЭ',   display_name='Аттис', plan_type='ma',
                 file_code='AT'   WHERE code='at';
UPDATE ports SET naznach='УТ-1', display_name='УТ-1',  plan_type='nk',
                 file_code='NMTP' WHERE code='ut';
UPDATE ports SET naznach='ГУТ-2',display_name='ГУТ-2', plan_type='ma'
                 WHERE code='gut';

-- профили разбивки груза
INSERT INTO cargo_split_profiles (code, name) VALUES
  ('single','Без разбивки'), ('metal_split','Уголь/Металл/Чугун');
INSERT INTO cargo_split_buckets (profile_id, code, name, match_cargo_group) VALUES
  ((SELECT id FROM cargo_split_profiles WHERE code='metal_split'),'coal','УГОЛЬ','УГОЛЬ'),
  ((SELECT id FROM cargo_split_profiles WHERE code='metal_split'),'metal','МЕТАЛЛ','МЕТАЛЛ'),
  ((SELECT id FROM cargo_split_profiles WHERE code='metal_split'),'chugun','ЧУГУН','ЧУГУН');
UPDATE ports SET cargo_split_profile_id =
  (SELECT id FROM cargo_split_profiles WHERE code='metal_split') WHERE code='gut';
UPDATE ports SET cargo_split_profile_id =
  (SELECT id FROM cargo_split_profiles WHERE code='single') WHERE code IN ('at','ut');

-- порт → чаты MAX
INSERT INTO port_chats (port_id, chat_name, chat_role) VALUES
  ((SELECT id FROM ports WHERE code='at'),  'at',  'main'),
  ((SELECT id FROM ports WHERE code='at'),  'at_o','oper'),
  ((SELECT id FROM ports WHERE code='gut'), 'gut', 'main'),
  ((SELECT id FROM ports WHERE code='ut'),  'ut',  'main');

-- представление АЭ+ГУТ
INSERT INTO port_views (code, name) VALUES ('ae_gut','АЭ+ГУТ');
INSERT INTO port_view_members (view_id, port_id) VALUES
  ((SELECT id FROM port_views WHERE code='ae_gut'),(SELECT id FROM ports WHERE code='at')),
  ((SELECT id FROM port_views WHERE code='ae_gut'),(SELECT id FROM ports WHERE code='gut'));
```

`report_column_mappings` наполняется переносом `getNmtpSprav1Mapping` строка в
строку (генерируется скриптом из существующего Go-кода).

**Критерий корректности seed:** после наполнения справочников реестр должен
отдавать ровно те же значения, что сейчас возвращают `switch`-и. Проверяется
тестом «старый switch == новый registry» до удаления `switch`.

---

## 6. Стратегия миграции (strangler, безопасно для прода-форка)

Каждый шаг оставляет приложение рабочим; `switch` удаляется только после того,
как реестр доказал эквивалентность.

1. **Схема + seed.** Применить миграции (новые таблицы/колонки + `client_settings`),
   залить seed по разделу 5. Поведение не меняется — данные просто появились.
2. **Реестр.** Ввести `PortRegistry`, загрузить в кэш при старте. Старые
   `switch` пока на месте.
3. **Тесты эквивалентности.** Для каждого `switch` — тест «switch == registry».
4. **Замена по подсистемам** (порядок из инвентаря, раздел 8):
   косметика/чаты → `switch naznach/plan_type` → схема кэша → раскладка
   отчётов → cargo-split → парсеры. После замены и зелёных тестов — удалить
   `switch`.
5. **Проверка на другом наборе портов.** После шагов 1–3 изменить состав `ports`
   (например, 2 порта с другими именами) на тестовой БД и прогнать сценарии, не
   трогая парсеры (для нового набора — ручной ввод/один простой формат).
6. **Парсеры.** В последнюю очередь — `parser_profiles`, когда модель устоялась.

---

## 7. Открытые вопросы для решения до реализации

- **Модель cargowork (6.2).** Рефакторить ли жёсткие поля `Coal*/Metal*/Chugun*`
  в обобщённую структуру сейчас или оставить, управляя только логикой через
  профиль. Рекомендация: оставить поля, обобщить логику (меньше риск).
- **`report_column_mappings`.** Нужен ли UI-редактор раскладки для нового набора
  портов, или достаточно SQL-наполнения на этапе прототипа.
- **Версионирование форматов парсеров.** Как сосуществуют `old`/`new` варианты в
  `parser_profiles` (поле `variant` + правило выбора).
- **`data_source` vs `ports.file_code`.** Финально развести «имя/regex файла»
  (в `data_source.config`) и «сопоставление данные→порт» (в `ports`), чтобы не
  осталось дублирующего источника правды по имени файла (§3.10).
- **Граница env / `client_settings`.** Финально утвердить, какие параметры из
  `_env` переезжают в таблицу, а какие остаются в env как секреты/per-deploy.

**Решено (анализ `ports.csv` + пайплайна, §3.12):**
- Идентификация порта — **составной ключ `(ОКПО + станция назначения)`**; `okpo_map`
  из `data_source.config` удаляется.
- Справочник станций (код→название) — **отдельный** `stations` (уже в DirectoryCache),
  не плоско в `ports`.
- Интервалы Stage 4 — **не хранить**, считать из `pc_*` (`interval_h = ваг × 24 / pc_рода`).

**Осталось уточнить по §3.12:**
- **`вагонов_в_поезде` в формуле интервала** — брать фактическое число вагонов поезда
  (`TrainInfo.VagonCount`) или нормативное? Проверка выше сошлась на фактическом —
  подтвердить при переносе Stage 4.
- **`is_bam`** — из какого поля дислокации берётся признак альт.маршрута (входящие
  данные vs справочник маршрута станции).
- **`route_speed` для нового клиента** — пороги/скорости участков специфичны для
  Дальнего Востока; для другого набора портов нужен способ задать свои (UI vs seed).
- **`co_arrival_group`** — как движок обработки реагирует на нарушение (блок vs
  предупреждение) и роль-исключение.

---

*Документ — проектные эскизы; имена полей и DDL уточняются при реализации
миграций. Источник истины по «как есть» — `ARCHITECTURE.md` и
`HARDCODE_INVENTORY.md`. Следующий шаг после согласования — написание первой
миграции (`client_settings` + расширение `ports` + seed раздела 5) и каркаса
`PortRegistry`.*
