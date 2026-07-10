-- Откат 000015: возврат к модели «одна станция = один план» (000014).
SET search_path TO dpport;

DROP TABLE IF EXISTS dpport.plan_nitka;
DROP TABLE IF EXISTS dpport.plan;

-- Восстанавливаем форму 000014 (plan_code PK, без истории).
CREATE TABLE IF NOT EXISTS dpport.plan (
    plan_code   text PRIMARY KEY,
    source_file text NOT NULL DEFAULT '',
    loaded_at   timestamp without time zone,
    nitki       integer NOT NULL DEFAULT 0,
    matched     integer NOT NULL DEFAULT 0,
    stamped     integer NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS dpport.plan_nitka (
    id             bigserial PRIMARY KEY,
    plan_code      text NOT NULL DEFAULT '',
    ord            integer NOT NULL DEFAULT 0,
    "index"        text NOT NULL DEFAULT '',
    index_pp       text NOT NULL DEFAULT '',
    plan_msk       timestamp without time zone,
    plan_jd        timestamp without time zone,
    fact_msk       timestamp without time zone,
    otkl           text NOT NULL DEFAULT '',
    wagons         integer NOT NULL DEFAULT 0,
    activ          integer NOT NULL DEFAULT 0,
    matched        boolean NOT NULL DEFAULT false,
    matched_wagons integer NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_plan_nitka_code_ord
    ON dpport.plan_nitka (plan_code, ord);
