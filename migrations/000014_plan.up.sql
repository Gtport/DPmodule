-- ============================================================================
--  000014_plan — хранение плана подвода (сетка ниток для фронта).
--
--  План подвода — внешнее расписание прибытия «ниток» (плановых поездов) в порт.
--  Разбор (internal/parser/plan) + сопоставление вагонов (internal/service/planmatch)
--  уже проставляют результат в dislocation (index_pp/plan_msk). Эти таблицы хранят
--  САМУ сетку плана — чтобы фронт показывал расписание и статус сопоставления.
--
--  Модель «одна станция плана = один актуальный план»: при загрузке нового файла
--  строки для plan_code перезаписываются (delete+insert ниток, upsert заголовка) —
--  тот же принцип атомарной замены, что и у снимка дислокации.
--
--  Идемпотентно (CREATE TABLE IF NOT EXISTS). Время — МСК naive, без таймзоны.
-- ============================================================================

SET search_path TO dpport;

-- Заголовок плана: одна строка на plan_code (ma/nk/…), сводка последней загрузки.
CREATE TABLE IF NOT EXISTS dpport.plan (
    plan_code   text PRIMARY KEY,                       -- код станции плана
    source_file text NOT NULL DEFAULT '',               -- имя загруженного файла
    loaded_at   timestamp without time zone,            -- когда загружен/применён (МСК)
    nitki       integer NOT NULL DEFAULT 0,             -- всего ниток в плане
    matched     integer NOT NULL DEFAULT 0,             -- ниток сопоставлено с вагонами
    stamped     integer NOT NULL DEFAULT 0              -- вагонов проставлено плановое прибытие
);

-- Нитки плана: строки расписания. Перезаписываются целиком при загрузке plan_code.
CREATE TABLE IF NOT EXISTS dpport.plan_nitka (
    id             bigserial PRIMARY KEY,
    plan_code      text NOT NULL DEFAULT '',            -- к какому плану относится
    ord            integer NOT NULL DEFAULT 0,          -- порядок следования в файле
    "index"        text NOT NULL DEFAULT '',            -- индекс поезда из плана
    index_pp       text NOT NULL DEFAULT '',            -- нормализованная метка нитки
    plan_msk       timestamp without time zone,         -- плановое прибытие (МСК, правило ≥18)
    plan_jd        timestamp without time zone,         -- плановое время как в плане (без сдвига)
    fact_msk       timestamp without time zone,         -- фактическое прибытие (если есть)
    otkl           text NOT NULL DEFAULT '',            -- отклонение факт−план «±HH:MM»
    wagons         integer NOT NULL DEFAULT 0,          -- всего вагонов поезда (Кол.ваг)
    activ          integer NOT NULL DEFAULT 0,          -- вагонов «наших» причалов (цель матча)
    matched        boolean NOT NULL DEFAULT false,      -- нитка сопоставлена с агрегацией
    matched_wagons integer NOT NULL DEFAULT 0           -- вагонов застолблено этой ниткой
);

CREATE INDEX IF NOT EXISTS idx_plan_nitka_code_ord
    ON dpport.plan_nitka (plan_code, ord);
