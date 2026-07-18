-- 000037_owner_field down: owner → perestanovka обратно (содержимое обнуляем —
-- perestanovka всегда была пустой), из vagon_history колонку убираем.

SET search_path TO dpport;

DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['dislocation','dislocation_new','status6','status9'] LOOP
        IF EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_schema = 'dpport' AND table_name = t AND column_name = 'owner')
           AND NOT EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_schema = 'dpport' AND table_name = t AND column_name = 'perestanovka') THEN
            EXECUTE format('ALTER TABLE dpport.%I RENAME COLUMN owner TO perestanovka', t);
            EXECUTE format($f$UPDATE dpport.%I SET perestanovka = ''$f$, t);
        END IF;
    END LOOP;
END $$;

ALTER TABLE dpport.vagon_history DROP COLUMN IF EXISTS owner;
