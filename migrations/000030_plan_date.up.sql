-- ============================================================================
--  000030_plan_date — дата плана в заголовке загрузки (таблица plan).
--
--  «На какую дату план» (самая ранняя ЖД-дата ниток) — для списка загрузок,
--  фильтра по дате и заголовка таблицы (как в gtport plan_date). Раньше дата
--  плана жила только в журнале (plan_upload.doc_ts) и в датах ниток.
--  Backfill — из сохранённых ниток (min(plan_jd), при пустом — min(plan_msk)).
-- ============================================================================

ALTER TABLE dpport.plan ADD COLUMN IF NOT EXISTS plan_date timestamp without time zone;

UPDATE dpport.plan p
SET plan_date = sub.d
FROM (
    SELECT plan_id, date_trunc('day', min(coalesce(plan_jd, plan_msk))) AS d
    FROM dpport.plan_nitka
    WHERE coalesce(plan_jd, plan_msk) IS NOT NULL
    GROUP BY plan_id
) sub
WHERE sub.plan_id = p.id AND p.plan_date IS NULL;
