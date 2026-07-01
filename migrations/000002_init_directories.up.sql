-- ============================================================================
--  000002_init_directories — справочники обогащения дислокации.
--  Схема dpport. Грузятся в RAM при старте (DirectoryCache) и используются
--  обогащением: stations + cargo_operations → Stage 1; marka + ports → Stage 2.
--  (naznach_station для Stage 2 — отдельным заходом позже.)
--  Stage 3/4 справочников не требуют.
--
--  МИНИМАЛЬНЫЙ СРЕЗ: только ключи поиска и колонки, которые читает обогащение.
--  Прочие колонки gtlogic (dist_*, sprav_*, color, pc_*, front, param_* и т.п.)
--  опущены — дообогатим при необходимости.
--
--  Данные грузятся отдельно (вне git): scripts/seed_directories.sql ← _reference/seed/*.csv
-- ============================================================================

SET search_path TO dpport;

-- 1. stations (Stage 1) — ключи kod, kod_4
CREATE TABLE dpport.stations (
    kod        integer PRIMARY KEY,            -- код ЕСР станции
    kod_4      integer NOT NULL DEFAULT 0,     -- 4-значный код (2-й индекс) → Code4StanNazn
    name       text    NOT NULL DEFAULT '',    -- → StationNach/StanNazn/StationOper
    road       text    NOT NULL DEFAULT '',    -- → DorogaNach/DorogaOper
    latitude   double precision,               -- → Latitude
    longitude  double precision,               -- → Longitude
    is_bam     boolean NOT NULL DEFAULT false   -- признак БАМ (Байкало-Амурская магистраль)
);
CREATE INDEX ix_stations_kod_4 ON dpport.stations (kod_4);

-- 2. cargo_operations (Stage 1) — ключ kod
--    ВНИМАНИЕ: в gtlogic короткое имя операции лежало в колонке `status`
--    (поля модели были перепутаны). Здесь колонка честно называется oper_s;
--    при выгрузке из прода она берётся из `status` (см. scripts/seed_directories.sql).
CREATE TABLE dpport.cargo_operations (
    kod     integer PRIMARY KEY,          -- код операции (KOP_VMD)
    oper    text NOT NULL DEFAULT '',     -- полное имя → Dislocation.Oper
    oper_s  text NOT NULL DEFAULT ''      -- краткое имя → Dislocation.OperS
);

-- 3. marka (Stage 2) — ключ (okpo, station_kod, cargo_kod), НЕ уникален → суррогатный id
CREATE TABLE dpport.marka (
    id           bigserial PRIMARY KEY,
    okpo         bigint NOT NULL,              -- ОКПО грузоотправителя
    station_kod  bigint NOT NULL,              -- код станции отправления
    cargo_kod    bigint NOT NULL,              -- код груза (ЕТСНГ)
    shipper      text NOT NULL DEFAULT '',     -- → Gruzotpr (имя грузоотправителя)
    cargo_s      text NOT NULL DEFAULT '',     -- → CargoS (имя груза)
    client       text NOT NULL DEFAULT '',     -- → Client
    cargo_group  text NOT NULL DEFAULT '',     -- → CargoGroup
    sms_1        text NOT NULL DEFAULT ''       -- → CargoSms / Sms1
);
CREATE INDEX ix_marka_key ON dpport.marka (okpo, station_kod, cargo_kod);

-- 4. ports (Stage 2) — ключ (okpo, location), НЕ уникален → суррогатный id
CREATE TABLE dpport.ports (
    id            bigserial PRIMARY KEY,
    okpo          bigint NOT NULL,             -- ОКПО грузополучателя
    location      text NOT NULL DEFAULT '',    -- станция/локация (часть ключа)
    organisation  text NOT NULL DEFAULT '',    -- → Gruzpol (организация-грузополучатель)
    name_s        text NOT NULL DEFAULT '',    -- → GruzpolS (краткое имя причала: УТ-1/АЭ/ГУТ-2)
    name          text NOT NULL DEFAULT '',
    code          text NOT NULL DEFAULT ''
);
CREATE INDEX ix_ports_key ON dpport.ports (okpo, location);
