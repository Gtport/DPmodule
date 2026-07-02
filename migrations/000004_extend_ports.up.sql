-- ============================================================================
--  000004_extend_ports — слой настроек/физики порта (TARGET.md §3.12).
--
--  Расширяет справочник dpport.ports (000002) полями, которые в gtlogic были
--  колонками таблицы ports (см. new_go/ports.csv): тип плана подвода, код
--  причальной станции, перерабатывающая способность по роду груза (pc_*), фронт,
--  цвет, признак активности, порядок. Идентичность (okpo, location) не трогаем —
--  индекс ix_ports_key уже есть.
--
--  Только добавление колонок (идемпотентно). Значения портов приходят через
--  seed (scripts/seed_directories.sql ← _reference/seed/ports.csv), НЕ хардкодом
--  в миграции — состав портов меняется под клиента.
--
--  pc_* и front — nullable: NULL = род груза не обрабатывается этим терминалом
--  (в gtlogic такие ячейки пустые). interval_h = вагонов × 24 / pc_рода (§3.12).
-- ============================================================================

SET search_path TO dpport;

ALTER TABLE dpport.ports
    ADD COLUMN IF NOT EXISTS plan_code    text    NOT NULL DEFAULT '',  -- param_s1: 'ma'/'nk'/'rb'
    ADD COLUMN IF NOT EXISTS station_code text    NOT NULL DEFAULT '',  -- param_s2: код причальной станции
    ADD COLUMN IF NOT EXISTS pc_coal      integer,                      -- перераб. способность, ваг/сут, уголь
    ADD COLUMN IF NOT EXISTS pc_metal     integer,                      -- ... металл
    ADD COLUMN IF NOT EXISTS pc_other     integer,                      -- ... прочее
    ADD COLUMN IF NOT EXISTS pc_total     integer,                      -- суммарно
    ADD COLUMN IF NOT EXISTS front        integer,                      -- фронт выгрузки
    ADD COLUMN IF NOT EXISTS color        text    NOT NULL DEFAULT '',  -- цвет отображения
    ADD COLUMN IF NOT EXISTS enabled      boolean NOT NULL DEFAULT true, -- at_work
    ADD COLUMN IF NOT EXISTS sort_order   integer NOT NULL DEFAULT 0;
