-- Откат 000048: терминал в эпизодах задержек.
DROP INDEX IF EXISTS dpport.ix_vagon_delay_gruzpol;
ALTER TABLE dpport.vagon_delay DROP COLUMN IF EXISTS gruzpol_s;
