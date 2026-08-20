-- ============================================================================
--  import_gtport_bros.sql — досинхронизация «Брошенных» из старого GTport.
--
--  Наша dpport.bros уже несёт историю с 25.10.2025 (копия при запуске DP);
--  переносится ДЕЛЬТА (сверка 20.08.2026): ~19 поездов, ~74 обновлённых в
--  gtport позже наших, ~78 записей журнала. Правила (утверждены 20.08.2026):
--    • отсутствующие у нас — INSERT; активные в gtport вставляются ЗАКРЫТЫМИ
--      (status_br=false), иначе первый applyBros «поднимет» их днём импорта;
--    • существующие — операторские поля из gtport, если он правился позже;
--      status_br наших строк не трогается (активными владеет конвейер);
--    • журнал: gtport главнее на совпадающих (bros_id, date), с бэкапом.
--
--  Вход: _reference/seed_gtport/bros.csv, bros_journal.csv (gtport_export.sql).
--  Запуск из корня репозитория: psql "$PG_DSN" -v ON_ERROR_STOP=1 -f scripts/import_gtport_bros.sql
--  Идемпотентно; повторный прогон меняет 0 строк (второй раз updated_at равны).
-- ============================================================================

\set ON_ERROR_STOP on

BEGIN;

CREATE TEMP TABLE t_bros (
  id text, id_index text, index_0 text, index_1 text, station_br text,
  doroga_br text, date_br text, gruzpol_s text, date_pod text,
  date_pod_fact text, prog_0 text, prog_1 text, to_go text, plan text,
  plan_history text, status_br text, reason text, comment text, sostav text,
  vagon_count text, created_at text, updated_at text
) ON COMMIT DROP;

CREATE TEMP TABLE t_brosj (
  bros_id text, date text, reason text, comment text, zayavka_nomer text,
  zayavka_date text, date_pod text, reason_text text, plan_pod text,
  is_agreed text, created_at text, created_by text
) ON COMMIT DROP;

\copy t_bros FROM '_reference/seed_gtport/bros.csv' WITH (FORMAT csv, HEADER true, NULL 'NULL')
\copy t_brosj FROM '_reference/seed_gtport/bros_journal.csv' WITH (FORMAT csv, HEADER true, NULL 'NULL')

-- Гварды типов схемы: наш reason varchar(20), id varchar(120).
SELECT 'ГВАРД: reason длиннее 20' AS what, count(*) FROM t_bros WHERE length(reason) > 20
UNION ALL SELECT 'ГВАРД: id длиннее 120', count(*) FROM t_bros WHERE length(id) > 120;

-- ── Бэкап затрагиваемых строк (один раз) ────────────────────────────────────
DO $$
BEGIN
  IF to_regclass('dpport.bros_backup_gtport') IS NULL THEN
    CREATE TABLE dpport.bros_backup_gtport AS
    SELECT d.* FROM dpport.bros d
    WHERE EXISTS (SELECT 1 FROM t_bros s
                  WHERE s.id = d.id AND s.updated_at::timestamp > d.updated_at);
    CREATE TABLE dpport.bros_journal_backup_gtport AS
    SELECT d.* FROM dpport.bros_journal d
    WHERE EXISTS (SELECT 1 FROM t_brosj s
                  WHERE s.bros_id = d.bros_id AND s.date::date = d.date);
  END IF;
END $$;

SELECT 'бэкап bros' AS what, count(*) FROM dpport.bros_backup_gtport
UNION ALL SELECT 'бэкап bros_journal', count(*) FROM dpport.bros_journal_backup_gtport;

-- ── Вставка недостающих поездов (активные gtport — закрытыми) ───────────────
INSERT INTO dpport.bros (
  id, id_index, index_0, index_1, station_br, doroga_br, date_br, gruzpol_s,
  date_pod, date_pod_fact, prog_0, prog_1, to_go, plan, plan_history,
  status_br, reason, comment, sostav, vagon_count, created_at, updated_at)
SELECT
  s.id, coalesce(s.id_index,''), coalesce(s.index_0,''), coalesce(s.index_1,''),
  coalesce(s.station_br,''), coalesce(s.doroga_br,''), s.date_br::date,
  coalesce(s.gruzpol_s,''), s.date_pod::date, s.date_pod_fact::date,
  s.prog_0::timestamp, s.prog_1::timestamp, s.to_go::numeric, s.plan::date,
  coalesce(s.plan_history,''), false,
  coalesce(s.reason,''), coalesce(s.comment,''), coalesce(s.sostav,''),
  coalesce(s.vagon_count,'0')::int, s.created_at::timestamp, s.updated_at::timestamp
FROM t_bros s
WHERE NOT EXISTS (SELECT 1 FROM dpport.bros d WHERE d.id = s.id)
ON CONFLICT (id) DO NOTHING;

-- ── Обновление: gtport правился позже наших ─────────────────────────────────
--  Только операторское наполнение; status_br и фактический подъём наших
--  активных бросков не трогаем (их ведёт applyBros).
UPDATE dpport.bros d SET
  reason  = CASE WHEN coalesce(s.reason,'')  <> '' THEN s.reason  ELSE d.reason  END,
  comment = CASE WHEN coalesce(s.comment,'') <> '' THEN s.comment ELSE d.comment END,
  date_pod = coalesce(s.date_pod::date, d.date_pod),
  plan     = coalesce(s.plan::date, d.plan),
  plan_history = CASE WHEN coalesce(s.plan_history,'') <> '' THEN s.plan_history ELSE d.plan_history END,
  date_pod_fact = CASE WHEN NOT d.status_br THEN coalesce(s.date_pod_fact::date, d.date_pod_fact)
                       ELSE d.date_pod_fact END,
  index_1 = CASE WHEN NOT d.status_br AND d.index_1 = '' AND coalesce(s.index_1,'') <> ''
                 THEN s.index_1 ELSE d.index_1 END,
  updated_at = timezone('Europe/Moscow', now())::timestamp
FROM t_bros s
WHERE s.id = d.id AND s.updated_at::timestamp > d.updated_at;

-- ── Журнал: недостающие записи + gtport главнее на совпадающих днях ─────────
INSERT INTO dpport.bros_journal (
  bros_id, date, reason, comment, zayavka_nomer, zayavka_date, date_pod,
  reason_text, plan_pod, is_agreed, created_at, created_by)
SELECT
  s.bros_id, s.date::date, coalesce(s.reason,''), coalesce(s.comment,''),
  s.zayavka_nomer, s.zayavka_date::date, s.date_pod::date,
  s.reason_text, s.plan_pod::date, s.is_agreed::boolean,
  coalesce(s.created_at::timestamp, s.date::date::timestamp),
  coalesce(s.created_by,'')
FROM t_brosj s
WHERE EXISTS (SELECT 1 FROM dpport.bros d WHERE d.id = s.bros_id)
ON CONFLICT (bros_id, date) DO UPDATE SET
  reason = excluded.reason, comment = excluded.comment,
  zayavka_nomer = excluded.zayavka_nomer, zayavka_date = excluded.zayavka_date,
  date_pod = excluded.date_pod, reason_text = excluded.reason_text,
  plan_pod = excluded.plan_pod, is_agreed = excluded.is_agreed;

-- ── Контроль ─────────────────────────────────────────────────────────────────
SELECT 'bros: всего' AS what, count(*) FROM dpport.bros
UNION ALL SELECT 'bros: активных', count(*) FROM dpport.bros WHERE status_br
UNION ALL SELECT 'bros_journal: всего', count(*) FROM dpport.bros_journal;

COMMIT;
