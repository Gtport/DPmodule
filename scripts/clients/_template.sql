-- ============================================================================
--  scripts/clients/_template.sql — ШАБЛОН клиентского сида нового заказчика.
--
--  Скопировать в scripts/clients/<кодовое-имя>.sql, заполнить по данным клиента
--  (первоисточник — выгрузка дислокации из его ЛК РЖД), пустые разделы удалить.
--  Запускать из корня репозитория, ПОСЛЕ migrate up и seed_directories.sql
--  (тот сеет CSV-справочники: stations, ports, cargo, marka, ... — они тоже
--  per-deployment, каталог _reference/seed/ вне git):
--
--      psql "$PG_DSN" -v ON_ERROR_STOP=1 -f scripts/clients/<клиент>.sql
--
--  Правила: идемпотентно (повторный прогон не дублирует и не затирает правки
--  Админа); прогонять ПОСЛЕ КАЖДОГО пересева seed_directories.sql (TRUNCATE
--  ports теряет org_short/nmtp_norm). Образец заполнения — gtport.sql.
-- ============================================================================

SET search_path TO dpport;

-- ── 1. ОКПО грузополучателя — НЕ ЗДЕСЬ ──────────────────────────────────────
--  «Чей файл» приём ЛК определяет по реестру ports (колонка okpo), поэтому ОКПО
--  клиента заводится строкой в _reference/seed/ports.csv вместе с терминалом,
--  станцией и перерабатывающей способностью. Здесь только то, чего нет в CSV.
--  Разбор формата файла (маркеры, отсечка 18ч) общий — уже в миграции 000003.

-- ── 2. Имя клиента и пороги приёма ───────────────────────────────────────────
--  Дефолтные пороги (миграция 000003) рассчитаны на автозабор каждые ~10 минут.
--  Если дислокация грузится РУКАМИ раз в сутки — ослабить max_staleness_minutes
--  (например 720), иначе гард отвергнет выгрузку, снятую больше часа назад.
UPDATE dpport.client_settings
   SET client_name = '<Имя клиента>',
       -- ingest_policy = jsonb_set(ingest_policy,
       --     '{dislocation,max_staleness_minutes}', '720'),
       updated_at = now()
 WHERE id = 1 AND client_name IN ('', 'DPport');

-- ── 3. Клиенты провайдера АСУ (только если интеграция АСУ будет включена) ────
-- UPDATE dpport.data_source
--    SET config = jsonb_set(config, '{clients}', '["<код-клиента>"]'),
--        updated_at = now()
--  WHERE id = 'asu' AND config->'clients' = '[]'::jsonb;

-- ── 4. Станции плана подвода (ТОЛЬКО если у станции ЕСТЬ план) ───────────────
--  Нет плана — раздел удалить целиком: пустая plan_profile сама выключает
--  плановую машинерию (экран «План подвода», карточки, статус-панель планов).
-- INSERT INTO dpport.plan_profile
--     (station_code, station_name, mode, plan_code, match_requires_naznach, our_terminals) VALUES
--     ('<код ЕСР>', '<ИМЯ СТАНЦИИ>', 'planned', '<код-плана>', false, '["<КЛЮЧИ КОЛОНОК>"]'::jsonb)
-- ON CONFLICT (station_code) DO NOTHING;
--
-- INSERT INTO dpport.nitka_schedule (station_code, slot_time, sort_order) VALUES
--     ('<код ЕСР>','00:01',1), ('<код ЕСР>','06:00',2)
-- ON CONFLICT (station_code, slot_time) DO NOTHING;

-- ── 5. Короткие имена организаций для статус-панелей ─────────────────────────
--  Пусто — подпись собирается из имён терминалов (это нормально для старта).
-- UPDATE dpport.ports SET org_short = '<ОРГ>'
--     WHERE provider_client = '<код-клиента>' AND org_short = '';

-- ── 6. Маршруты MAX (только если рассылка MAX будет включена) ────────────────
--  Чаты (max_chat) сеются из CSV seed_directories; здесь — какие формы в какие
--  чаты уходят. Формы: podhod / pogruzka / vygruzka / nmtp; terminal='' — сводка.
-- INSERT INTO dpport.max_route (report, terminal, chat_name, sort_order, enabled)
-- SELECT 'podhod', t.terminal, t.chat, t.so, true
-- FROM (VALUES ('<Терминал>', '<чат>', 10)) AS t(terminal, chat, so)
-- WHERE EXISTS (SELECT 1 FROM dpport.max_chat c WHERE c.name = t.chat)
-- ON CONFLICT ON CONSTRAINT max_route_key DO NOTHING;

-- ── 7. Отчёт по форме порта (nmtp_*) — только если клиенту нужна такая форма ─
--  Пустая nmtp_column = карточка «Отчёты НМТП» не показывается. Образец
--  раскладки — gtport.sql (guard WHERE NOT EXISTS по терминалу обязателен:
--  у nmtp_column нет уникального ключа).

-- ── 8. Пресеты отчётов (клиентские варианты «Подхода») — опционально ─────────
-- INSERT INTO dpport.report_preset (report, name, clients, sort_order)
-- VALUES ('podhod', '<Название>', '<КЛИЕНТ1|КЛИЕНТ2>', 10)
-- ON CONFLICT ON CONSTRAINT report_preset_key DO NOTHING;
