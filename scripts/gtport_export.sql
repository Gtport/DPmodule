-- ============================================================================
--  gtport_export.sql — выгрузка данных старого GTport для разового переноса
--  истории в dpport (10 месяцев прода, см. scripts/import_gtport_history.sql
--  и import_gtport_bros.sql; сверка и правила — отчёт «Сверка GTport → DPport»
--  от 20.08.2026).
--
--  Запускается на ЛОКАЛЬНОЙ копии базы старого GTport (`gtport_src` в WSL,
--  восстановлена из pg_dump боевого gtport_prod), из корня репозитория:
--    psql "postgres://gtport_app@localhost:5433/gtport_src?sslmode=disable" \
--         -v ON_ERROR_STOP=1 -f scripts/gtport_export.sql
--
--  Результат:
--    _reference/seed_gtport/vagon_history.csv  — история вагонов (с логом операций)
--    _reference/seed_gtport/bros.csv           — брошенные поезда
--    _reference/seed_gtport/bros_journal.csv   — журнал брошенных
--    /home/alex/projects/files/vigr_{at,ut,gut}.csv — суточные листы, свежая
--        версия ПОВЕРХ файлов переноса 22.07: их читает штатный
--        scripts/import_vigr.sql (тот же путь, формат и ON CONFLICT DO UPDATE).
--
--  Формат единый с import_vigr.sql: CSV с заголовком, NULL как строка 'NULL'.
-- ============================================================================

-- История вагонов: 39 колонок старой схемы, порядок зафиксирован и обязан
-- совпадать с temp-таблицей t_hist в import_gtport_history.sql.
\copy (SELECT id, vagon, invoice_main, invoice, index_main, index_pp, date_nach_d, station_nach, gruzotpr, zayavka, stan_nazn, gruzpol_s, naznach, cargo_s, cargo_group, ves, client, status, date_dostav, date_prib, date_prib_d, plan_msk, plan_jd, otkl, delay, date_vigr, place_vigr, shipments, frost, history, rod_vag_uch, info_1, info_2, info_3, sms_1, sms_2, color, created_at, updated_at FROM public.vagon_history ORDER BY id) TO '_reference/seed_gtport/vagon_history.csv' WITH (FORMAT csv, HEADER true, NULL 'NULL')

\copy (SELECT id, id_index, index_0, index_1, station_br, doroga_br, date_br, gruzpol_s, date_pod, date_pod_fact, prog_0, prog_1, to_go, plan, plan_history, status_br, reason, comment, sostav, vagon_count, created_at, updated_at FROM public.bros ORDER BY id) TO '_reference/seed_gtport/bros.csv' WITH (FORMAT csv, HEADER true, NULL 'NULL')

\copy (SELECT bros_id, date, reason, comment, zayavka_nomer, zayavka_date, date_pod, reason_text, plan_pod, is_agreed, created_at, created_by FROM public.bros_journal ORDER BY bros_id, date) TO '_reference/seed_gtport/bros_journal.csv' WITH (FORMAT csv, HEADER true, NULL 'NULL')

-- Суточные листы: SELECT * непригоден (import_vigr.sql ждёт конкретный порядок
-- колонок модели) — перечисляем как в его temp-таблицах t_std / t_gut.
\copy (SELECT date, ost_18, ost_st, prib, plan, vigr_fact, vigr_stan, ost, useful_formation, total_formation, downtime, analytics_json, train_structure_json, prim, effectiv, perepokaz, created_at, updated_at, glin_load, glin_plan, glin_ost, pek_load, pek_plan, pek_ost, ruda_load, ruda_plan, ruda_ost, kont_load, kont_plan, kont_ost, proch_load, proch_plan, proch_ost FROM public.vigr_at ORDER BY date) TO '/home/alex/projects/files/vigr_at.csv' WITH (FORMAT csv, HEADER true, NULL 'NULL')

\copy (SELECT date, ost_18, ost_st, prib, plan, vigr_fact, vigr_stan, ost, useful_formation, total_formation, downtime, analytics_json, train_structure_json, prim, effectiv, perepokaz, created_at, updated_at, glin_load, glin_plan, glin_ost, pek_load, pek_plan, pek_ost, ruda_load, ruda_plan, ruda_ost, kont_load, kont_plan, kont_ost, proch_load, proch_plan, proch_ost FROM public.vigr_ut ORDER BY date) TO '/home/alex/projects/files/vigr_ut.csv' WITH (FORMAT csv, HEADER true, NULL 'NULL')

\copy (SELECT date, coal_ost_18, coal_y_ost_st, coal_prib, coal_plan, coal_vigr_fact, coal_vigr_stan, coal_ost, coal_useful_formation, coal_total_formation, coal_downtime, coal_analytics_json, coal_train_structure_json, coal_effectiv, coal_perepokaz, metal_ost_18, metal_ost_st, metal_prib, metal_plan, metal_vigr_fact, metal_vigr_stan, metal_ost, metal_useful_formation, metal_total_formation, metal_downtime, metal_analytics_json, metal_train_structure_json, metal_effectiv, metal_perepokaz, chugun_ost_18, chugun_ost_st, chugun_prib, chugun_plan, chugun_vigr_fact, chugun_vigr_stan, chugun_ost, chugun_useful_formation, chugun_total_formation, chugun_downtime, chugun_analytics_json, chugun_train_structure_json, chugun_effectiv, chugun_perepokaz, train_structure_json, prim, created_at, updated_at, glin_load, glin_plan, glin_ost, pek_load, pek_plan, pek_ost, ruda_load, ruda_plan, ruda_ost, kont_load, kont_plan, kont_ost, proch_load, proch_plan, proch_ost FROM public.vigr_gut ORDER BY date) TO '/home/alex/projects/files/vigr_gut.csv' WITH (FORMAT csv, HEADER true, NULL 'NULL')

-- Контроль объёмов:
SELECT 'vagon_history' AS t, count(*) FROM public.vagon_history
UNION ALL SELECT 'bros', count(*) FROM public.bros
UNION ALL SELECT 'bros_journal', count(*) FROM public.bros_journal
UNION ALL SELECT 'vigr_at', count(*) FROM public.vigr_at
UNION ALL SELECT 'vigr_ut', count(*) FROM public.vigr_ut
UNION ALL SELECT 'vigr_gut', count(*) FROM public.vigr_gut;
