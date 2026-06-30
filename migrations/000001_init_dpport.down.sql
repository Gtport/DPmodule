-- Откат 000001_init_dpport: удаление бизнес-таблиц схемы dpport.
-- Порядок учитывает FK: vagon_operation ссылается на vagon_history(trip_key).
-- Схему dpport НЕ удаляем — в ней живёт таблица версий migrate (schema_migrations).
DROP TABLE IF EXISTS dpport.vagon_operation;
DROP TABLE IF EXISTS dpport.vagon_history;
DROP TABLE IF EXISTS dpport.dislocation;
