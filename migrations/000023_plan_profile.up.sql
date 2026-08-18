-- ============================================================================
--  000023_plan_profile — настроечная таблица станции плана + расписание ниток.
--
--  Расхардкодивание builtinProfiles (internal/parser/plan/profile.go) и задел под
--  Stage 4 (прогноз прибытия). Ключ — СТАНЦИЯ (station_code = ports.station_code):
--  терминалы одной станции делят настройки (АЭ+ГУТ-2 → 985702; УТ-1 → 984700).
--
--  mode:
--    'planned'  — у станции есть расписание ниток (nitka_schedule); Stage 4
--                 раскладывает поезда по слотам, план якорит нитки.
--    'capacity' — плана нет; прогноз только из перерабатывающей способности
--                 (ports.pc_*), не больше pc_рода × correction_coef в сутки.
--
--  correction_coef — поправочный коэффициент (ОДИН на станцию) для capacity-режима.
--  our_terminals   — ключевые слова "наших" колонок плана (вклад в Activ), из builtin.
--  nitka_schedule  — потолок прибытия станции: слоты общие для ВСЕХ портов (наших и
--                    чужих), станция больше физически не примет.
--
--  Владение таблицами передаём gtport_app (иначе созданные под суперпользователем
--  недоступны приложению — см. инцидент с владением снимка).
-- ============================================================================

SET search_path TO dpport;

CREATE TABLE IF NOT EXISTS dpport.plan_profile (
    station_code           text PRIMARY KEY,               -- = ports.station_code
    station_name           text NOT NULL,
    mode                   text NOT NULL DEFAULT 'planned', -- 'planned' | 'capacity'
    plan_code              text,                            -- ma/nk у плановых; NULL у беспланов.
    correction_coef        numeric NOT NULL DEFAULT 1.0,    -- поправочный коэф (capacity-режим)
    match_requires_naznach boolean NOT NULL DEFAULT false,
    our_terminals          jsonb   NOT NULL DEFAULT '[]'::jsonb,
    updated_at             timestamp without time zone DEFAULT now()
);

CREATE TABLE IF NOT EXISTS dpport.nitka_schedule (
    station_code text NOT NULL,               -- → plan_profile.station_code
    slot_time    time NOT NULL,               -- 00:01, 03:47, ...
    sort_order   int  NOT NULL DEFAULT 0,
    PRIMARY KEY (station_code, slot_time)
);

-- Профили станций и слоты — клиентские данные: сеются per-deployment скриптом
-- scripts/clients/<клиент>.sql (для трёх терминалов — gtport.sql). Пустая
-- plan_profile = у клиента нет плановых станций, плановая машинерия выключена.

-- Роль есть только на своих стендах; на управляемой базе (миграции идут от
-- выданной DevOps роли — она и так владелец) шаг пропускается.
DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'gtport_app') THEN
        ALTER TABLE dpport.plan_profile   OWNER TO gtport_app;
        ALTER TABLE dpport.nitka_schedule OWNER TO gtport_app;
    END IF;
END $$;
