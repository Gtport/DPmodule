-- Откат 000012: peregruz → info_3 (идемпотентно, по тем же таблицам).
SET search_path TO dpport;

DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['dislocation','dislocation_new','vagon_history','status6','status9'] LOOP
        IF EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_schema = 'dpport' AND table_name = t AND column_name = 'peregruz')
           AND NOT EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_schema = 'dpport' AND table_name = t AND column_name = 'info_3') THEN
            EXECUTE format('ALTER TABLE dpport.%I RENAME COLUMN peregruz TO info_3', t);
        END IF;
    END LOOP;
END $$;
