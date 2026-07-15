-- ============================================================================
--  000026_stage4_unified_maxlen — единый алгоритм Stage 4 + лимит длины состава.
--
--  Реш.: в GTport два пути раскладки (staircase УТ-1 / excel АЭ-ГУТ-2) — случайность
--  (оптимизирован был один путь). Переходим на ЕДИНУЮ «лестницу»-очередь причала для
--  всех станций → переключатель distribution_method больше не нужен, удаляем.
--
--  Добавляем max_train_length — станционный лимит длины состава: в дислокации поезд до
--  71 ваг, но причал ограничен, и формула интервала «переваривания» берёт min(вагонов,
--  лимит). Наши причалы (984700 УТ-1, 985702 АЭ/ГУТ-2) — 64 вагона.
-- ============================================================================

SET search_path TO dpport;

ALTER TABLE dpport.plan_profile DROP COLUMN IF EXISTS distribution_method;

ALTER TABLE dpport.plan_profile
    ADD COLUMN IF NOT EXISTS max_train_length integer NOT NULL DEFAULT 0;

UPDATE dpport.plan_profile SET max_train_length = 64 WHERE station_code IN ('984700', '985702');
