-- ============================================================================
--  000058_gt_forecast_speed — плановая скорость выгрузки линии (прогноз ГТ).
--
--  Вкладка «Прогноз прибытия/выгрузки» (перенос страницы GT из gtport) моделирует
--  выгрузку двумя скоростями:
--    · ПЛАН  — реальная производственная скорость (сколько терминал реально
--              разгружает в сутки) — этой колонки в DPmodule не было;
--    · НОРМА — перерабатывающая способность линии — это существующий
--              port_cargo_line.pc (fallback ports.pc_* как в грузовой работе).
--
--  В gtport план жил в отдельной таблице gt_port_speeds (port, cargo_type,
--  plan_speed, norm_speed). Отдельная таблица не нужна: ключ (terminal,
--  cargo_key) линии выгрузки 1:1 совпадает с потоком симуляции, а norm_speed
--  дублировал способность. Поэтому план — колонка на существующей линии.
--
--  Сид — дефолты gtport (клиентский фолбэк DEFAULT_SPEEDS; боевая база старого
--  GTport из среды разработки недоступна, значения правятся в Админе —
--  port_cargo_line уже в реестре list_tables). NULL → берётся норма (pc).
--
--  Идемпотентно (IF NOT EXISTS, сид только по plan_speed IS NULL), только
--  добавляющая.
-- ============================================================================

ALTER TABLE dpport.port_cargo_line
    ADD COLUMN IF NOT EXISTS plan_speed integer;

COMMENT ON COLUMN dpport.port_cargo_line.plan_speed
    IS 'План выгрузки, ваг/сут (прогноз ГТ; пусто — как способность)';

UPDATE dpport.port_cargo_line SET plan_speed = 130
    WHERE terminal = 'АЭ'    AND kind = 'unload' AND cargo_key = ''       AND plan_speed IS NULL;
UPDATE dpport.port_cargo_line SET plan_speed = 250
    WHERE terminal = 'УТ-1'  AND kind = 'unload' AND cargo_key = ''       AND plan_speed IS NULL;
UPDATE dpport.port_cargo_line SET plan_speed = 120
    WHERE terminal = 'ГУТ-2' AND kind = 'unload' AND cargo_key = 'УГОЛЬ'  AND plan_speed IS NULL;
UPDATE dpport.port_cargo_line SET plan_speed = 100
    WHERE terminal = 'ГУТ-2' AND kind = 'unload' AND cargo_key = 'МЕТАЛЛ' AND plan_speed IS NULL;
UPDATE dpport.port_cargo_line SET plan_speed = 30
    WHERE terminal = 'ГУТ-2' AND kind = 'unload' AND cargo_key = 'ЧУГУН'  AND plan_speed IS NULL;
