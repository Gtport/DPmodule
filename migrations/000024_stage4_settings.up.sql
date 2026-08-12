-- ============================================================================
--  000024_stage4_settings — данные и пороги для Stage 4 (прогноз ProgMsk).
--
--  1. Расписание Находки (УТ-1, station_code 984700) — перенос ut1Schedule из
--     эталона enrich_stage4.go (Мыс Астафьева 985702 засеян в 000023).
--  2. slot_tolerance_h на plan_profile — допуск слота (перенос квирка «−6ч» УТ-1
--     в данные): слот может быть ≥ Rasch − допуск. Находке ставим 6ч, остальным 0.
--  3. Пороги Stage 4 в client_settings.extra.stage4: минимум вагонов для прогноза
--     (20; для брошенных 10) и штраф бросания (72ч).
-- ============================================================================

SET search_path TO dpport;

-- Расписание Находки и её slot_tolerance_h=6 — клиентские данные, переехали в
-- scripts/clients/gtport.sql. Здесь остаётся только общая схема и пороги.

ALTER TABLE dpport.plan_profile ADD COLUMN IF NOT EXISTS slot_tolerance_h numeric NOT NULL DEFAULT 0;

UPDATE dpport.client_settings
   SET extra = jsonb_set(COALESCE(extra, '{}'::jsonb), '{stage4}',
                 '{"min_vagon_count":20,"min_vagon_bros":10,"bros_penalty_h":72}'::jsonb)
 WHERE id = 1;
