-- ============================================================================
--  seed_directories.sql — загрузка справочников обогащения в схему dpport.
--
--  Запускать ПОСЛЕ `migrate up` (таблицы из 000002_init_directories должны быть).
--  CSV лежат в _reference/seed/ (вне git, per-deployment). Пути относительные —
--  запускать из корня репозитория:
--
--      psql "$PG_DSN" -v ON_ERROR_STOP=1 -f scripts/seed_directories.sql
--
--  Идемпотентно: TRUNCATE + перезаливка. RESTART IDENTITY сбрасывает bigserial
--  (id у marka/ports), чтобы повторный прогон не наращивал счётчики.
--  naznach_station пока не грузим (отдельным заходом).
-- ============================================================================

SET search_path TO dpport;

TRUNCATE stations, cargo_operations, marka, ports RESTART IDENTITY;

\copy stations(kod,kod_4,name,road,latitude,longitude) FROM '_reference/seed/stations.csv' WITH (FORMAT csv, HEADER true)
\copy cargo_operations(kod,oper,oper_s) FROM '_reference/seed/cargo_operations.csv' WITH (FORMAT csv, HEADER true)
\copy marka(okpo,station_kod,cargo_kod,shipper,cargo_s,client,cargo_group,sms_1) FROM '_reference/seed/marka.csv' WITH (FORMAT csv, HEADER true)
\copy ports(okpo,location,organisation,name_s,name,code) FROM '_reference/seed/ports.csv' WITH (FORMAT csv, HEADER true)

-- Контроль загрузки:
SELECT 'stations' AS tbl, count(*) FROM stations
UNION ALL SELECT 'cargo_operations', count(*) FROM cargo_operations
UNION ALL SELECT 'marka', count(*) FROM marka
UNION ALL SELECT 'ports', count(*) FROM ports;
