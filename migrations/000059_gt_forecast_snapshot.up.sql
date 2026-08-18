-- ============================================================================
--  000059_gt_forecast_snapshot — сохранённые планы прогноза ГТ (перенос
--  gtport gt_saved_plans).
--
--  Диспетчер фиксирует рассчитанный план вкладки «Прогноз прибытия/выгрузки»
--  на дату: очередь поездов, диаграммы выгрузки, свободные нитки и журнал
--  what-if правок сеанса. Снапшот — данные UI «как посчитано на момент
--  сохранения» (архив read-only + сырьё CSV-аналитики «прогноз vs факт»),
--  НЕ справочник: в реестр list_tables не заводится, управление — из диалога
--  вкладки.
--
--  Ключ — (plan_date, station): один план на дату и причальную станцию
--  (в gtport page_mode 'АЭ+ГУТ'/'УТ-1' — у нас режим задаёт код станции,
--  без хардкода имён). Повторное сохранение перезаписывает (upsert).
--
--  Содержимое — jsonb-слепки ответа simulate и входа сеанса: сервер их не
--  разбирает при показе архива (фронт рисует как есть), аналитика читает
--  trains/flows/free_slots. Время — Московское naive.
--
--  Идемпотентно (IF NOT EXISTS), только добавляющая.
-- ============================================================================

CREATE TABLE IF NOT EXISTS dpport.gt_forecast_snapshot (
    id          bigserial PRIMARY KEY,
    plan_date   date NOT NULL,                -- на какие сутки сохранён план
    station     text NOT NULL,                -- код причальной станции (режим вкладки)
    start_date  date NOT NULL,                -- стартовые сутки расчёта
    days_count  integer NOT NULL DEFAULT 10,  -- горизонт, суток
    request     jsonb,                        -- вход сеанса (скорости, правки, тумблер)
    trains      jsonb,                        -- очередь поездов (ответ simulate)
    flows       jsonb,                        -- диаграммы Ганта по потокам
    free_slots  jsonb,                        -- свободные нитки горизонта
    journal     jsonb,                        -- журнал what-if правок сеанса
    saved_by    text NOT NULL DEFAULT '',
    created_at  timestamp without time zone,
    updated_at  timestamp without time zone,
    CONSTRAINT gt_forecast_snapshot_key UNIQUE (plan_date, station)
);

CREATE INDEX IF NOT EXISTS idx_gt_forecast_snapshot_date
    ON dpport.gt_forecast_snapshot (plan_date DESC, station);

-- Роль есть только на своих стендах; на управляемой базе шаг пропускается.
DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'gtport_app') THEN
        ALTER TABLE dpport.gt_forecast_snapshot OWNER TO gtport_app;
    END IF;
END $$;
