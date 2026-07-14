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

INSERT INTO dpport.nitka_schedule (station_code, slot_time, sort_order) VALUES
    ('984700','01:41',1),('984700','05:38',2),('984700','08:43',3),('984700','12:08',4),
    ('984700','15:53',5),('984700','17:30',6),('984700','19:10',7),('984700','21:00',8),
    ('984700','23:00',9)
ON CONFLICT (station_code, slot_time) DO NOTHING;

ALTER TABLE dpport.plan_profile ADD COLUMN IF NOT EXISTS slot_tolerance_h numeric NOT NULL DEFAULT 0;
UPDATE dpport.plan_profile SET slot_tolerance_h = 6 WHERE station_code = '984700';

UPDATE dpport.client_settings
   SET extra = jsonb_set(COALESCE(extra, '{}'::jsonb), '{stage4}',
                 '{"min_vagon_count":20,"min_vagon_bros":10,"bros_penalty_h":72}'::jsonb)
 WHERE id = 1;
