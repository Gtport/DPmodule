-- ============================================================================
--  000034_admin_column_labels — русские подписи колонок для админ-редактора.
--
--  Подписи — ДАННЫЕ, а не хардкод фронта: хранятся комментариями к колонкам
--  (COMMENT ON COLUMN), редактор читает их из pg_description вместе со схемой.
--  Новый справочник в редакторе получает подписи так же — комментариями.
-- ============================================================================

COMMENT ON COLUMN dpport.marka.id          IS '№';
COMMENT ON COLUMN dpport.marka.okpo        IS 'ОКПО отправителя';
COMMENT ON COLUMN dpport.marka.station_kod IS 'Код станции';
COMMENT ON COLUMN dpport.marka.station     IS 'Станция погрузки';
COMMENT ON COLUMN dpport.marka.cargo_group IS 'Группа груза';
COMMENT ON COLUMN dpport.marka.shipper     IS 'Грузоотправитель';
COMMENT ON COLUMN dpport.marka.client      IS 'Клиент';
COMMENT ON COLUMN dpport.marka.sms_1       IS 'СМС-1 (метка)';
COMMENT ON COLUMN dpport.marka.sms_3       IS 'СМС-3 (регион)';
COMMENT ON COLUMN dpport.marka.color       IS 'Цвет';
COMMENT ON COLUMN dpport.marka.sprav_1     IS 'Справочно 1';

COMMENT ON COLUMN dpport.stations.kod       IS 'Код ЕСР';
COMMENT ON COLUMN dpport.stations.kod_4     IS 'Код (4 зн.)';
COMMENT ON COLUMN dpport.stations.name      IS 'Станция';
COMMENT ON COLUMN dpport.stations.road      IS 'Дорога';
COMMENT ON COLUMN dpport.stations.latitude  IS 'Широта';
COMMENT ON COLUMN dpport.stations.longitude IS 'Долгота';
COMMENT ON COLUMN dpport.stations.is_bam    IS 'БАМ';
