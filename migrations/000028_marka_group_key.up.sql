-- ============================================================================
--  000028_marka_group_key — перестройка словаря marka под групповой ключ.
--
--  Ключ (okpo, station_kod, cargo_group) вместо (okpo, station_kod, cargo_kod):
--  новый код груза внутри знакомой группы у известного отправителя матчится без
--  правки словаря. Идентичность груза (код → группа/имя/метка) — словарь cargo
--  (000027); marka оставляет только бизнес-атрибуцию: shipper/client/sms_1/sms_3.
--  sms_2 не хранится — расчётный (sms_1 + cargo_sms словаря).
--
--  ПЕРЕСОЗДАНИЕ таблицы (осознанный отход от «только добавляющих» миграций):
--  marka — справочник, данные заливаются заново seed-скриптом:
--  scripts/gen_marka_seed.py (схлопывание старого экспорта 87 → 45 строк:
--  sms_1 ← старый sms_3, sms_3 ← старый sms_2 где однозначно) →
--  _reference/seed/marka.csv → scripts/seed_directories.sql.
-- ============================================================================

DROP TABLE dpport.marka;

CREATE TABLE dpport.marka (
    okpo         bigint NOT NULL,              -- ОКПО грузоотправителя
    station_kod  bigint NOT NULL,              -- код станции отправления
    cargo_group  text   NOT NULL,              -- группа груза (= cargo.cargo_group)
    shipper      text NOT NULL DEFAULT '',     -- → Gruzotpr (имя грузоотправителя)
    client       text NOT NULL DEFAULT '',     -- → Client
    sms_1        text NOT NULL DEFAULT '',     -- → Sms1 (метка отправитель+станция+группа)
    sms_3        text NOT NULL DEFAULT '',     -- → Sms3 (регион/направление; '' если неоднозначно)
    PRIMARY KEY (okpo, station_kod, cargo_group)
);
