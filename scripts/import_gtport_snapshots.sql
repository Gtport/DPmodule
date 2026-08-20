-- ============================================================================
--  import_gtport_snapshots.sql — заливка сохранённых прогнозов старого GTport
--  в dpport.gt_forecast_snapshot. Вход готовит cmd/gtsnapconv (142 снапшота
--  26.05–19.08.2026 конвертированы без отказов, прогон 20.08.2026).
--
--  Исключение из правила «gtport главнее»: конвертация с потерями, поэтому
--  наши РОДНЫЕ снапшоты не перезаписываются — ON CONFLICT DO NOTHING;
--  конфликтные пары (дата × станция) печатаются в отчёт.
--
--  Запуск из корня репозитория: psql "$PG_DSN" -v ON_ERROR_STOP=1 -1 -f scripts/import_gtport_snapshots.sql
--  Идемпотентно.
-- ============================================================================

CREATE TEMP TABLE t_snap (
  plan_date text, station text, start_date text, days_count text,
  request text, trains text, flows text, free_slots text, journal text,
  saved_by text, created_at text, updated_at text
) ON COMMIT DROP;

\copy t_snap FROM '_reference/seed_gtport/gt_forecast_snapshot.csv' WITH (FORMAT csv, HEADER true)

-- Конфликтные пары (наш родной снапшот против конвертированного) — в отчёт:
SELECT 'конфликт (наш остаётся): ' || s.plan_date || ' / ' || s.station AS what
FROM t_snap s
JOIN dpport.gt_forecast_snapshot d
  ON d.plan_date = s.plan_date::date AND d.station = s.station;

INSERT INTO dpport.gt_forecast_snapshot (
  plan_date, station, start_date, days_count,
  request, trains, flows, free_slots, journal,
  saved_by, created_at, updated_at)
SELECT
  s.plan_date::date, s.station, s.start_date::date, s.days_count::int,
  s.request::jsonb, s.trains::jsonb, s.flows::jsonb, s.free_slots::jsonb, s.journal::jsonb,
  s.saved_by, s.created_at::timestamp, s.updated_at::timestamp
FROM t_snap s
ON CONFLICT (plan_date, station) DO NOTHING;

SELECT station, count(*), min(plan_date), max(plan_date) FROM dpport.gt_forecast_snapshot GROUP BY 1 ORDER BY 1;
