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
    timezone_offset     INTEGER DEFAULT 7,        -- сейчас зашито (+7 МСК, timeOffset)
    json_files_count    INTEGER DEFAULT 2,        -- «сколько файлов парсим»
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
  клиента, число файлов, расписание синка, флаги интеграций, пороги, смещение
  часового пояса, набор фич.

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
-- настроечный синглтон
INSERT INTO client_settings (id, client_name, timezone_offset, json_files_count)
VALUES (1, 'GTport (3 порта)', 7, 2)
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
- **Граница env / `client_settings`.** Финально утвердить, какие параметры из
  `_env` переезжают в таблицу, а какие остаются в env как секреты/per-deploy.

---

*Документ — проектные эскизы; имена полей и DDL уточняются при реализации
миграций. Источник истины по «как есть» — `ARCHITECTURE.md` и
`HARDCODE_INVENTORY.md`. Следующий шаг после согласования — написание первой
миграции (`client_settings` + расширение `ports` + seed раздела 5) и каркаса
`PortRegistry`.*
