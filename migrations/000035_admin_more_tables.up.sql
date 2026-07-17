-- ============================================================================
--  000035_admin_more_tables — в админ-редактор добавляются справочники
--  sf (синонимы станций формирования), naznach_station (перестановки
--  назначения) и cargo (словарь грузов ЕТСНГ): строки реестра + русские
--  подписи колонок (как в 000034).
-- ============================================================================

INSERT INTO dpport.list_tables (name, name_ru, editable) VALUES
    ('sf',              'С.Ф. (станции формирования сборных)', true),
    ('naznach_station', 'Назначения (перестановки станций)',   true),
    ('cargo',           'Грузы (словарь ЕТСНГ)',               true)
ON CONFLICT (name) DO NOTHING;

COMMENT ON COLUMN dpport.sf.id       IS '№';
COMMENT ON COLUMN dpport.sf.sinonim  IS 'Синоним (как в плане)';
COMMENT ON COLUMN dpport.sf.station  IS 'Станция формирования';
COMMENT ON COLUMN dpport.sf.quantity IS 'Потолок вагонов';
COMMENT ON COLUMN dpport.sf.enabled  IS 'Включена';

COMMENT ON COLUMN dpport.naznach_station.id             IS '№';
COMMENT ON COLUMN dpport.naznach_station.dest_station   IS 'Станция назначения';
COMMENT ON COLUMN dpport.naznach_station.origin_station IS 'Станция отправления';
COMMENT ON COLUMN dpport.naznach_station.naznach        IS 'Назначение (площадка)';
COMMENT ON COLUMN dpport.naznach_station.univers        IS 'Универсальная';
COMMENT ON COLUMN dpport.naznach_station.enabled        IS 'Включена';

COMMENT ON COLUMN dpport.cargo.cargo_kod   IS 'Код ЕТСНГ';
COMMENT ON COLUMN dpport.cargo.name        IS 'Наименование груза';
COMMENT ON COLUMN dpport.cargo.cargo_group IS 'Группа';
COMMENT ON COLUMN dpport.cargo.cargo_s     IS 'Краткое имя';
COMMENT ON COLUMN dpport.cargo.cargo_sms   IS 'Метка (СМС)';
