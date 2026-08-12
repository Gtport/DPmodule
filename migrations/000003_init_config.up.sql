-- ============================================================================
--  000003_init_config — настроечные таблицы (TARGET.md §3.10/§3.11).
--  Схема dpport. Грузятся в RAM при старте (ConfigCache).
--
--   • data_source     — реестр каналов ввода (откуда/чем/как валидируем).
--   • client_settings — синглтон клиентских параметров (пороги приёма ingest_policy).
--
--  Без смещений часового пояса (§3.11): всё время Московское naive.
--  Сид конфигурации — здесь же (идемпотентно), это не bulk-данные.
-- ============================================================================

SET search_path TO dpport;

-- 1. data_source — реестр каналов ввода. config (JSONB) — транспорт и пер-файловая
--    валидация; parser_profile_id пока без FK (таблица parser_profiles будет позже).
CREATE TABLE dpport.data_source (
    id                text PRIMARY KEY,              -- 'lk', 'json_at', 'plan_ma'
    name              text NOT NULL DEFAULT '',      -- человекочитаемое
    enabled           boolean NOT NULL DEFAULT true,
    ingest            text NOT NULL,                 -- 'upload' | 'api_pull'
    category          text NOT NULL,                 -- 'dislocation' | 'plan' | 'tech_state'
    parser_profile_id integer,                       -- FK на parser_profiles — позже
    config            jsonb NOT NULL DEFAULT '{}',   -- маркеры/ОКПО-мэппинг/пороги файла
    sort_order        integer NOT NULL DEFAULT 0,
    created_at        timestamp NOT NULL DEFAULT now(),
    updated_at        timestamp NOT NULL DEFAULT now()
);

-- 2. client_settings — синглтон (id=1). ingest_policy (JSONB) — пороги приёма по
--    категориям (разрыв, устаревание, откат на старое, потеря данных). Без
--    timezone_offset (§3.11) и без json_files_count (производное от data_source).
CREATE TABLE dpport.client_settings (
    id            integer PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    client_name   text NOT NULL DEFAULT '',
    ingest_policy jsonb NOT NULL DEFAULT '{}',
    extra         jsonb NOT NULL DEFAULT '{}',
    updated_at    timestamp NOT NULL DEFAULT now()
);

-- ─────────────────────────── нейтральный сид ядра ───────────────────────────
-- Миграции — общий код всех клиентов, поэтому здесь только строки, без которых
-- приложение не работает, с НЕЙТРАЛЬНЫМИ значениями; клиентское (имя клиента,
-- пороги под его ритм загрузки) доливает scripts/clients/<клиент>.sql.
-- Формат выгрузки ЛК (маркеры, расширения, отсечка 18ч) общий для всех клиентов
-- ЛК РЖД — остаётся здесь. ОКПО кабинетов не здесь: «чей файл» определяется по
-- реестру ports (прежний okpo_map упразднён миграцией 000006).
INSERT INTO dpport.data_source (id, name, ingest, category, config) VALUES
 ('lk', 'Дислокация из ЛК РЖД', 'upload', 'dislocation',
   '{"detect":["Личный кабинет"],
     "subtype_marker":{"Дислокация вагонов":"lk"},
     "allowed_ext":["xlsx","xls"], "max_mb":10,
     "header_marker":"Номер вагона", "date_cutoff_hour":18}')
ON CONFLICT (id) DO NOTHING;

-- Пороги приёма (§3.9): разрыв 15м, устаревание 60м, откат на старое (кроме админа),
-- потеря данных 30%; для планов — план не позже дислокации на 1ч.
INSERT INTO dpport.client_settings (id, client_name, ingest_policy) VALUES
 (1, 'DPport',
   '{"dislocation":{"max_gap_minutes":15,"max_staleness_minutes":60,
     "reject_older_than_current":true,"reject_older_role_exempt":"administrator",
     "max_data_loss_pct":30},
     "plan":{"plan_max_lag_hours":1}}')
ON CONFLICT (id) DO NOTHING;
