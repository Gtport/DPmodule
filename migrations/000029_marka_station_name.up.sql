-- ============================================================================
--  000029_marka_station_name — имя станции погрузки в словаре marka.
--
--  Информационная колонка для владельца (словарь правится руками, голый
--  station_kod нечитаем) — как в старом GTport. Обогащение её НЕ использует
--  (поиск по коду). Backfill из справочника stations; при расхождении с кодом
--  авторитетен station_kod.
-- ============================================================================

ALTER TABLE dpport.marka ADD COLUMN IF NOT EXISTS station text NOT NULL DEFAULT '';

UPDATE dpport.marka m
SET station = s.name
FROM dpport.stations s
WHERE s.kod = m.station_kod AND m.station = '';
