-- Откат 000007: удаление метки совместного среза.
SET search_path TO dpport;

ALTER TABLE dpport.data_source DROP COLUMN IF EXISTS co_arrival_group;
