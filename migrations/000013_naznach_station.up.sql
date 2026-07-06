-- ============================================================================
--  000013_naznach_station — настроечная таблица «перестановок назначения» (§3.17).
--
--  Разрешает «парадокс потоков»: вагон физически идёт на станцию назначения
--  dest_station (напр. МЫС АСТАФЬЕВА = ГУТ-2), но коммерческий поток (площадка
--  внутри порта, поле naznach) зависит от станции ОТПРАВЛЕНИЯ origin_station.
--  Обобщает хардкод имени станции из gtlogic: у каждой dest_station свой список.
--
--  Логика (Stage 2, enrichFromNaznachStation): Naznach = GruzpolS по умолчанию;
--  если (stan_nazn, station_nach) есть в таблице (enabled, непустой naznach) —
--  берём оттуда. Пустая таблица → всегда GruzpolS. sms_1/2 из исходной выгрузки
--  НЕ тащим (метки модуля уведомлений); univers сохраняем.
--
--  Идемпотентно (CREATE TABLE IF NOT EXISTS). Сид — scripts/seed_directories.sql
--  из _reference/seed/naznach_station.csv (per-deployment, вне git).
-- ============================================================================

SET search_path TO dpport;

CREATE TABLE IF NOT EXISTS dpport.naznach_station (
    id             bigserial PRIMARY KEY,
    dest_station   text NOT NULL,                        -- станция назначения-триггер
    origin_station text NOT NULL,                        -- станция отправления (ключ)
    naznach        text NOT NULL DEFAULT '',             -- площадка назначения (результат)
    univers        boolean NOT NULL DEFAULT false,       -- признак «универсальный»
    enabled        boolean NOT NULL DEFAULT true,        -- включена ли перестановка
    created_at     timestamp without time zone NOT NULL DEFAULT now(),
    updated_at     timestamp without time zone NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_naznach_station_key
    ON dpport.naznach_station (dest_station, origin_station);
