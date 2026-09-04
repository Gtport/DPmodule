-- 000064: паспорт сохранённого расчёта прогноза ГТ (аналитика «обстановка»,
--  решение владельца 05.09.2026, docs/ANALYTICS.md §7.1).
--
--  Снапшот — «что знали в момент расчёта». Чтобы снапшоты можно было сравнивать
--  между собой и с фактом, у каждого нужен паспорт:
--    computed_at — момент расчёта (МСК naive), а не дата плана;
--    kind        — 'manual' (кнопка диспетчера) | 'auto' (ежедневный крон
--                  gt_snapshot.cron). Крон НЕ перезаписывает ручной снапшот
--                  тех же суток: ручной богаче (журнал правок сеанса);
--    meta        — jsonb: скорости линий (план/норма) на день расчёта, час
--                  отсечки, горизонт, тумблер нормы, число what-if правок,
--                  число поездов. Без него пересмотр норм в справочнике меняет
--                  прошлое незаметно.
--
--  Ключ (plan_date, station) не меняется: один снапшот на сутки и станцию.
--  Идемпотентно (IF NOT EXISTS), только добавляющая.
SET search_path TO dpport;

ALTER TABLE dpport.gt_forecast_snapshot
    ADD COLUMN IF NOT EXISTS computed_at timestamp without time zone;
ALTER TABLE dpport.gt_forecast_snapshot
    ADD COLUMN IF NOT EXISTS kind text NOT NULL DEFAULT 'manual';
ALTER TABLE dpport.gt_forecast_snapshot
    ADD COLUMN IF NOT EXISTS meta jsonb;

COMMENT ON COLUMN dpport.gt_forecast_snapshot.computed_at IS 'Момент расчёта (МСК naive)';
COMMENT ON COLUMN dpport.gt_forecast_snapshot.kind        IS 'manual — кнопка диспетчера; auto — ежедневный крон';
COMMENT ON COLUMN dpport.gt_forecast_snapshot.meta        IS 'Паспорт расчёта: скорости линий, час отсечки, горизонт, правки';
