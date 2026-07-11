-- ============================================================================
--  000016_sf — справочник синонимов станций формирования (для с.ф.).
--
--  С.ф. (сборные формирования) — нитки плана без реального индекса («с.ф.БИКИН»).
--  Таблица нормализует варианты написания синонима (sinonim) к канонической станции
--  формирования (station) и задаёт потолок вагонов (quantity). Источник правды при
--  подборе групп-кандидатов для с.ф. (аналог таблицы sf в gtlogic:
--  SFRecord{sinonim, station, quantity}). Матч: extractSynonim(IndexPp) == sinonim.
--
--  Идемпотентно (CREATE TABLE IF NOT EXISTS). Сид — scripts/seed_directories.sql
--  из _reference/seed/sf.csv (per-deployment, вне git).
-- ============================================================================

SET search_path TO dpport;

CREATE TABLE IF NOT EXISTS dpport.sf (
    id         bigserial PRIMARY KEY,
    sinonim    text NOT NULL,                        -- вариант написания синонима (ключ матча)
    station    text NOT NULL,                        -- каноническая станция формирования
    quantity   integer NOT NULL DEFAULT 0,           -- потолок вагонов на синоним
    enabled    boolean NOT NULL DEFAULT true,        -- включён ли синоним
    created_at timestamp without time zone NOT NULL DEFAULT now(),
    updated_at timestamp without time zone NOT NULL DEFAULT now()
);

-- Синоним не уникален: у одного синонима возможны несколько станций, и в выгрузке
-- встречаются точные дубли — уникальность НЕ навязываем, только индекс для поиска.
CREATE INDEX IF NOT EXISTS ix_sf_sinonim ON dpport.sf (sinonim);
