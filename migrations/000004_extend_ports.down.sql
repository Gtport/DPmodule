-- Откат 000004_extend_ports: удаление колонок слоя настроек/физики порта.
SET search_path TO dpport;

ALTER TABLE dpport.ports
    DROP COLUMN IF EXISTS plan_code,
    DROP COLUMN IF EXISTS station_code,
    DROP COLUMN IF EXISTS pc_coal,
    DROP COLUMN IF EXISTS pc_metal,
    DROP COLUMN IF EXISTS pc_other,
    DROP COLUMN IF EXISTS pc_total,
    DROP COLUMN IF EXISTS front,
    DROP COLUMN IF EXISTS color,
    DROP COLUMN IF EXISTS enabled,
    DROP COLUMN IF EXISTS sort_order;
