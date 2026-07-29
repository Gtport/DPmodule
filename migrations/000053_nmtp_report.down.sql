-- Откат 000053: справочники раскладки НМТП-отчёта.
SET search_path TO dpport;

DELETE FROM dpport.list_tables WHERE name IN ('nmtp_column', 'nmtp_mark');
DROP TABLE IF EXISTS dpport.nmtp_column;
DROP TABLE IF EXISTS dpport.nmtp_mark;
ALTER TABLE dpport.ports DROP COLUMN IF EXISTS nmtp_norm;
