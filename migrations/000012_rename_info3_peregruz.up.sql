-- ============================================================================
--  000012_rename_info3_peregruz — переименование info_3 → peregruz (§3.17).
--
--  Семантика: peregruz хранит номер вагона-донора, у которого приёмник забрал
--  груз при перегрузе (S2-3 донорство status6). Пустое поле = обычная погрузка.
--  Правило запросов к vagon_history: «погруженные за дату» с непустым peregruz
--  ИСКЛЮЧАТЬ (перегруз ≠ фактическая погрузка).
--
--  Затрагиваются все таблицы с раскладкой LIKE dislocation: dislocation,
--  dislocation_new (staging, может отсутствовать), vagon_history, status6,
--  status9. Идемпотентно: переименование только если есть info_3 и ещё нет
--  peregruz; отсутствующие таблицы пропускаются.
-- ============================================================================

SET search_path TO dpport;

DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['dislocation','dislocation_new','vagon_history','status6','status9'] LOOP
        IF EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_schema = 'dpport' AND table_name = t AND column_name = 'info_3')
           AND NOT EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_schema = 'dpport' AND table_name = t AND column_name = 'peregruz') THEN
            EXECUTE format('ALTER TABLE dpport.%I RENAME COLUMN info_3 TO peregruz', t);
        END IF;
    END LOOP;
END $$;
