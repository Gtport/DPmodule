-- ============================================================================
--  import_gtport_history.sql — разовый перенос истории вагонов старого GTport
--  (10 месяцев прода, 02.11.2025–20.08.2026) в dpport.vagon_history +
--  разворот текстового лога операций в dpport.vagon_operation.
--
--  Правила утверждены владельцем 20.08.2026 (отчёт «Сверка GTport → DPport»):
--    • вставляются рейсы, которых у нас нет (дедуп по trip_key);
--    • на пересечении gtport ГЛАВНЕЕ, но только по закрытым в gtport рейсам
--      (есть date_vigr) и по прибытиям, которых у нас нет; живые рейсы
--      конвейера не трогаются; пустые значения gtport ничего не затирают;
--    • наша date_prib при обоих прибывших НЕ перезаписывается (канон единого
--      штампа поезда, фикс «поезда рассыпаются» от 06.08.2026);
--    • перед UPDATE затрагиваемые строки уходят в бэкап-таблицу
--      dpport.vagon_history_backup_gtport (создаётся один раз, повторный
--      прогон её не пересобирает).
--
--  Вход: _reference/seed_gtport/vagon_history.csv (см. scripts/gtport_export.sql).
--  Запуск из корня репозитория (локально или на VPS):
--    psql "$PG_DSN" -v ON_ERROR_STOP=1 -f scripts/import_gtport_history.sql
--  Идемпотентно: NOT EXISTS + ON CONFLICT DO NOTHING; повторный прогон
--  добавляет 0 строк. Гонка с живым конвейером закрыта ON CONFLICT.
-- ============================================================================

\set ON_ERROR_STOP on

-- Граница нашей истории: первый рейс DPport создан 06.07.2026. Старые
-- незакрытые рейсы gtport (статус < 10 без выгрузки) старше границы помечаются
-- not_arrived, чтобы не замусорить «В пути» и «Не выгруж.».
\set dp_history_start '''2026-07-06'''

BEGIN;

-- ── 1. Стейджинг: CSV как есть, 39 колонок старой схемы (all-text) ───────────
CREATE TEMP TABLE t_hist (
  id text, vagon text, invoice_main text, invoice text, index_main text,
  index_pp text, date_nach_d text, station_nach text, gruzotpr text,
  zayavka text, stan_nazn text, gruzpol_s text, naznach text, cargo_s text,
  cargo_group text, ves text, client text, status text, date_dostav text,
  date_prib text, date_prib_d text, plan_msk text, plan_jd text, otkl text,
  delay text, date_vigr text, place_vigr text, shipments text, frost text,
  history text, rod_vag_uch text, info_1 text, info_2 text, info_3 text,
  sms_1 text, sms_2 text, color text, created_at text, updated_at text
) ON COMMIT DROP;

\copy t_hist FROM '_reference/seed_gtport/vagon_history.csv' WITH (FORMAT csv, HEADER true, NULL 'NULL')

ALTER TABLE t_hist ADD COLUMN tk bigint;
UPDATE t_hist SET tk = vagon::bigint * 100000 + (date_nach_d::date - DATE '1970-01-01')
 WHERE vagon ~ '^\d+$' AND date_nach_d IS NOT NULL;
CREATE INDEX ON t_hist (tk);
ANALYZE t_hist;

SELECT 'стейджинг: строк' AS what, count(*) FROM t_hist
UNION ALL SELECT 'отсев (trip_key не вычислим)', count(*) FROM t_hist WHERE tk IS NULL;

-- ── 2. Бэкап затрагиваемых наших строк (один раз, до первой правки) ─────────
DO $$
BEGIN
  IF to_regclass('dpport.vagon_history_backup_gtport') IS NULL THEN
    CREATE TABLE dpport.vagon_history_backup_gtport AS
    SELECT d.* FROM dpport.vagon_history d
    WHERE EXISTS (
      SELECT 1 FROM t_hist s
      WHERE s.tk = d.trip_key
        AND (s.date_vigr IS NOT NULL OR (s.status = '10' AND coalesce(d.status, 0) < 10))
    );
  END IF;
END $$;

SELECT 'бэкап: строк' AS what, count(*) FROM dpport.vagon_history_backup_gtport;

-- ── 3. Пересечение: gtport главнее (только закрытые рейсы и наши дыры) ──────
--  СТРОГО ДО вставки и только по строкам из бэкапа: иначе UPDATE зацепил бы
--  свежевставленные gtport-строки и перештамповал их updated_at.
UPDATE dpport.vagon_history d SET
  -- терминалы: перезапись только валидным значением из реестра (отсекает
  -- опечатки вида «ГУТ-3» и пустоту)
  naznach = CASE WHEN coalesce(s.naznach,'') <> '' AND s.naznach IN (SELECT name_s FROM dpport.ports)
                 THEN s.naznach ELSE d.naznach END,
  place_vigr = CASE WHEN s.date_vigr IS NOT NULL AND coalesce(s.place_vigr,'') <> ''
                    THEN s.place_vigr ELSE d.place_vigr END,
  date_vigr   = CASE WHEN s.date_vigr IS NOT NULL THEN s.date_vigr::date::timestamp ELSE d.date_vigr END,
  date_vigr_d = CASE WHEN s.date_vigr IS NOT NULL THEN s.date_vigr::date::timestamp ELSE d.date_vigr_d END,
  status = CASE WHEN s.date_vigr IS NOT NULL THEN 12
                WHEN s.status = '10' AND coalesce(d.status, 0) < 10 THEN 10
                ELSE d.status END,
  -- прибытие подставляем только там, где у нас его не было: при обоих
  -- прибывших наш единый штамп поезда вернее date_kon старой системы
  date_prib   = CASE WHEN coalesce(d.status,0) < 10 AND s.date_prib IS NOT NULL
                     THEN s.date_prib::timestamp ELSE d.date_prib END,
  date_prib_d = CASE WHEN coalesce(d.status,0) < 10 AND s.date_prib_d IS NOT NULL
                     THEN s.date_prib_d::date::timestamp ELSE d.date_prib_d END,
  delay = CASE WHEN coalesce(d.status,0) < 10 AND s.delay IS NOT NULL THEN s.delay::int ELSE d.delay END,
  otkl  = CASE WHEN coalesce(d.status,0) < 10 AND coalesce(s.otkl,'') <> '' THEN s.otkl ELSE d.otkl END,
  -- дозаполнение пустых (наши значения полнее по формату — не перетираем)
  index_pp   = CASE WHEN d.index_pp = ''   AND coalesce(s.index_pp,'') <> ''   THEN s.index_pp   ELSE d.index_pp END,
  index_main = CASE WHEN d.index_main = '' AND coalesce(s.index_main,'') <> '' THEN s.index_main ELSE d.index_main END,
  freight_exact_name = CASE WHEN d.freight_exact_name = '' AND coalesce(s.info_1,'') <> ''
                            THEN s.info_1 ELSE d.freight_exact_name END,
  gtd_number = CASE WHEN d.gtd_number = '' AND coalesce(s.info_2,'') <> '' THEN s.info_2 ELSE d.gtd_number END,
  frost = coalesce(d.frost, s.frost::int),
  not_arrived = CASE WHEN s.date_vigr IS NOT NULL OR s.status = '10' THEN false ELSE d.not_arrived END,
  updated_at = timezone('Europe/Moscow', now())::timestamp
FROM t_hist s
WHERE s.tk = d.trip_key
  AND (s.date_vigr IS NOT NULL OR (s.status = '10' AND coalesce(d.status, 0) < 10))
  AND EXISTS (SELECT 1 FROM dpport.vagon_history_backup_gtport b WHERE b.trip_key = d.trip_key);

-- ── 4. Вставка рейсов, которых у нас нет ────────────────────────────────────
--  Маппинг: info_1→freight_exact_name (марка), info_2→gtd_number (ГТД),
--  info_3→peregruz; статус 12 выводится из date_vigr (в gtport его не было);
--  date_vigr в gtport — ЖД-сутки без времени → согласованная пара
--  date_vigr/date_vigr_d. zayavka/shipments/rod_vag_uch в gtport пусты по
--  всей базе. created_at/updated_at — родные штампы gtport (провенанс).
INSERT INTO dpport.vagon_history (
  id, vagon, invoice_main, invoice, index_main, index_pp, date_nach_d,
  station_nach, gruzotpr, stan_nazn, gruzpol_s, naznach, cargo_s, cargo_group,
  freight_exact_name, gtd_number, ves, client, status, date_dostav,
  plan_msk, plan_jd, otkl, delay, date_prib, date_prib_d,
  date_vigr, date_vigr_d, place_vigr, frost, sms_1, sms_2, color, peregruz,
  not_arrived, created_at, updated_at)
SELECT
  s.id, s.vagon, coalesce(s.invoice_main,''), coalesce(s.invoice,''),
  coalesce(s.index_main,''), coalesce(s.index_pp,''), s.date_nach_d::timestamp,
  coalesce(s.station_nach,''), coalesce(s.gruzotpr,''), coalesce(s.stan_nazn,''),
  coalesce(s.gruzpol_s,''), coalesce(s.naznach,''), coalesce(s.cargo_s,''), s.cargo_group,
  coalesce(s.info_1,''), coalesce(s.info_2,''), s.ves::numeric, coalesce(s.client,''),
  CASE WHEN s.date_vigr IS NOT NULL THEN 12 ELSE s.status::int END,
  s.date_dostav::timestamp, s.plan_msk::timestamp, s.plan_jd::timestamp,
  coalesce(s.otkl,''), s.delay::int, s.date_prib::timestamp, s.date_prib_d::timestamp,
  s.date_vigr::date::timestamp, s.date_vigr::date::timestamp,
  coalesce(s.place_vigr,''), s.frost::int,
  coalesce(s.sms_1,''), coalesce(s.sms_2,''), coalesce(s.color,''), coalesce(s.info_3,''),
  (s.date_vigr IS NULL AND coalesce(s.status,'0')::int < 10
     AND s.date_nach_d::date < :dp_history_start::date),
  coalesce(s.created_at::timestamp, s.updated_at::timestamp, s.date_nach_d::timestamp),
  coalesce(s.updated_at::timestamp, s.created_at::timestamp, s.date_nach_d::timestamp)
FROM t_hist s
WHERE s.tk IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM dpport.vagon_history d WHERE d.trip_key = s.tk)
ON CONFLICT DO NOTHING;

-- ── 5. Трейл: лог операций gtport → vagon_operation ─────────────────────────
--  Формат элемента: «ДД.ММ.ГГГГ ЧЧ:ММ/код_станции/код_операции/остаток_км»,
--  элементы через «;». Остаток км наша таблица не хранит. Заполняются только
--  рейсы БЕЗ трейла (у кого уже есть операции из запроса 601 — не трогаем:
--  ReplaceForTrip владеет ими целиком).
INSERT INTO dpport.vagon_operation (trip_key, date_op, kop_vmd, stan_op, index_poezd)
SELECT DISTINCT ON (s.tk, op.date_op)
  s.tk, op.date_op, op.kop, op.stan, ''
FROM t_hist s
CROSS JOIN LATERAL (
  SELECT to_timestamp(split_part(e, '/', 1), 'DD.MM.YYYY HH24:MI')::timestamp AS date_op,
         split_part(e, '/', 3) AS kop,
         split_part(e, '/', 2) AS stan
  FROM unnest(string_to_array(s.history, ';')) AS e
  WHERE e ~ '^\d{2}\.\d{2}\.\d{4} \d{2}:\d{2}/'
) op
WHERE s.tk IS NOT NULL
  AND length(op.kop) BETWEEN 1 AND 3
  AND length(op.stan) BETWEEN 1 AND 6
  AND EXISTS (SELECT 1 FROM dpport.vagon_history d WHERE d.trip_key = s.tk)
  AND NOT EXISTS (SELECT 1 FROM dpport.vagon_operation o WHERE o.trip_key = s.tk)
ON CONFLICT DO NOTHING;

-- ── 6. Контроль ──────────────────────────────────────────────────────────────
SELECT 'vagon_history: всего' AS what, count(*) FROM dpport.vagon_history
UNION ALL SELECT 'из них недоехавшие (not_arrived)', count(*) FROM dpport.vagon_history WHERE not_arrived
UNION ALL SELECT 'vagon_operation: всего', count(*) FROM dpport.vagon_operation
UNION ALL SELECT 'отсев CSV (в отчёт владельцу)', count(*) FROM t_hist WHERE tk IS NULL;

SELECT date_trunc('month', date_nach_d)::date AS mes, count(*)
FROM dpport.vagon_history GROUP BY 1 ORDER BY 1;

COMMIT;
