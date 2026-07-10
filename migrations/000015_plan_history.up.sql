-- ============================================================================
--  000015_plan_history — история планов подвода + расширение сетки для таблицы
--  как в оригинале GTport (столбцы портов, «Состав», станция операции, комментарий,
--  строка «Остаток на 18:00»).
--
--  Что меняется относительно 000014:
--   • plan теперь хранит НЕСКОЛЬКО загрузок на станцию (история). PK — суррогатный
--     id; plan_code больше не уникален. Фронт показывает список загрузок и по
--     умолчанию — самую свежую (loaded_at DESC).
--   • plan_nitka привязана к конкретной загрузке (plan_id) и несёт данные под
--     таблицу оригинала: station_oper («Дислокация»), comment («Примечание»),
--     sostav (готовая строка сматченных групп), ports (JSONB — обобщённый набор
--     ячеек портов из листьев файла, без хардкода терминалов), is_ostatok (флаг
--     служебной строки «Остаток на 18:00»).
--
--  ВНИМАНИЕ: таблицы plan/plan_nitka хранят реконструируемую view-сетку плана
--  (НЕ бизнес-историю vagon_history). Поэтому здесь допустимо пересоздание
--  (drop+create) — данные восстановимы повторной загрузкой файла. Это осознанное
--  отступление от правила «миграции только добавляющие», согласовано с владельцем.
--
--  Идемпотентно. Время — МСК naive, без таймзоны.
-- ============================================================================

SET search_path TO dpport;

DROP TABLE IF EXISTS dpport.plan_nitka;
DROP TABLE IF EXISTS dpport.plan;

-- Заголовок загрузки плана: одна строка на загрузку (история по plan_code).
CREATE TABLE IF NOT EXISTS dpport.plan (
    id          bigserial PRIMARY KEY,
    plan_code   text NOT NULL DEFAULT '',            -- код станции плана (ma/nk/…)
    source_file text NOT NULL DEFAULT '',            -- имя загруженного файла
    loaded_at   timestamp without time zone,         -- когда загружен/применён (МСК)
    nitki       integer NOT NULL DEFAULT 0,          -- всего ниток (без служебных строк)
    matched     integer NOT NULL DEFAULT 0,          -- ниток сопоставлено с вагонами
    stamped     integer NOT NULL DEFAULT 0           -- вагонов проставлено плановое прибытие
);

-- Свежесть загрузки в рамках станции — по loaded_at (фронт берёт первую).
CREATE INDEX IF NOT EXISTS idx_plan_code_loaded
    ON dpport.plan (plan_code, loaded_at DESC);

-- Нитки конкретной загрузки. Удаляются каскадом при удалении заголовка.
CREATE TABLE IF NOT EXISTS dpport.plan_nitka (
    id             bigserial PRIMARY KEY,
    plan_id        bigint NOT NULL REFERENCES dpport.plan(id) ON DELETE CASCADE,
    plan_code      text NOT NULL DEFAULT '',            -- денормализация (для выборок/логов)
    ord            integer NOT NULL DEFAULT 0,          -- порядок следования в файле
    "index"        text NOT NULL DEFAULT '',            -- индекс поезда из плана
    index_pp       text NOT NULL DEFAULT '',            -- нормализованная метка нитки
    station_oper   text NOT NULL DEFAULT '',            -- станция текущей операции («Дислокация»)
    plan_msk       timestamp without time zone,         -- плановое прибытие (МСК, правило ≥18)
    plan_jd        timestamp without time zone,         -- плановое время как в плане (без сдвига)
    fact_msk       timestamp without time zone,         -- фактическое прибытие (если есть)
    otkl           text NOT NULL DEFAULT '',            -- отклонение факт−план «±HH:MM»
    wagons         integer NOT NULL DEFAULT 0,          -- всего вагонов поезда (Кол.ваг)
    activ          integer NOT NULL DEFAULT 0,          -- вагонов «наших» причалов (цель матча)
    ports          jsonb   NOT NULL DEFAULT '[]',       -- ячейки портов: [{"label":..,"count":..}]
    sostav         text NOT NULL DEFAULT '',            -- сматченные группы («Состав»)
    comment        text NOT NULL DEFAULT '',            -- «Примечание» (столбец «Комментарий»)
    matched        boolean NOT NULL DEFAULT false,      -- нитка сопоставлена с агрегацией
    matched_wagons integer NOT NULL DEFAULT 0,          -- вагонов застолблено этой ниткой
    is_ostatok     boolean NOT NULL DEFAULT false       -- служебная строка «Остаток на 18:00»
);

CREATE INDEX IF NOT EXISTS idx_plan_nitka_plan_ord
    ON dpport.plan_nitka (plan_id, ord);
