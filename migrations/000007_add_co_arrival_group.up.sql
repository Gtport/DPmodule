-- ============================================================================
--  000007_add_co_arrival_group — метка совместного среза (TARGET.md §3.12).
--
--  Парадокс потоков: в одном физпоезде — вагоны разных юр.лиц; если данные взяты
--  разными временными срезами, один поезд «стоит на разных станциях». Источники
--  с общим непустым co_arrival_group должны приходить одним срезом; контроль
--  разрыва (ingest_policy.max_gap_minutes) применяется МЕЖДУ ними.
--
--  Для текущего клиента дислокация приходит одним каналом 'lk' (несколько ОКПО в
--  папке приёма) → группа 'dislocation'. При переходе на api_pull (своя ручка на
--  порт = своя строка data_source) та же метка свяжет их в одну co-arrival группу.
-- ============================================================================

SET search_path TO dpport;

ALTER TABLE dpport.data_source
    ADD COLUMN IF NOT EXISTS co_arrival_group text NOT NULL DEFAULT '';

UPDATE dpport.data_source
   SET co_arrival_group = 'dislocation', updated_at = now()
 WHERE id = 'lk';
