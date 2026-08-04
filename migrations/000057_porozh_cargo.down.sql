-- Откат 000057: справочник кодов груза порожняковых рейсов.
SET search_path TO dpport;

DELETE FROM dpport.list_tables WHERE name = 'porozh_cargo';
DROP TABLE IF EXISTS dpport.porozh_cargo;
