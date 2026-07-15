-- Возврат структуры marka из 000002 (ключ по коду груза). Данные не
-- восстанавливаются — перезалить старым seed-файлом.
DROP TABLE dpport.marka;

CREATE TABLE dpport.marka (
    id           bigserial PRIMARY KEY,
    okpo         bigint NOT NULL,
    station_kod  bigint NOT NULL,
    cargo_kod    bigint NOT NULL,
    shipper      text NOT NULL DEFAULT '',
    cargo_s      text NOT NULL DEFAULT '',
    client       text NOT NULL DEFAULT '',
    cargo_group  text NOT NULL DEFAULT '',
    sms_1        text NOT NULL DEFAULT ''
);
CREATE INDEX ix_marka_key ON dpport.marka (okpo, station_kod, cargo_kod);
