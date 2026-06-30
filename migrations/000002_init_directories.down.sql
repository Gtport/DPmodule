-- Откат 000002_init_directories: удаление справочников обогащения.
-- Между собой FK не связаны — порядок произволен.
DROP TABLE IF EXISTS dpport.ports;
DROP TABLE IF EXISTS dpport.marka;
DROP TABLE IF EXISTS dpport.cargo_operations;
DROP TABLE IF EXISTS dpport.stations;
