ALTER TABLE dpport.dislocation
    DROP COLUMN IF EXISTS car_trusted_name,
    DROP COLUMN IF EXISTS car_trusted_okpo,
    DROP COLUMN IF EXISTS pereadr_type,
    DROP COLUMN IF EXISTS pereadr_port;

-- staging-таблица создаётся приложением в рантайме и может отсутствовать (см. up).
DO $$
BEGIN
    IF to_regclass('dpport.dislocation_new') IS NOT NULL THEN
        EXECUTE $f$
            ALTER TABLE dpport.dislocation_new
                DROP COLUMN IF EXISTS car_trusted_name,
                DROP COLUMN IF EXISTS car_trusted_okpo,
                DROP COLUMN IF EXISTS pereadr_type,
                DROP COLUMN IF EXISTS pereadr_port$f$;
    END IF;
END $$;

ALTER TABLE dpport.status9
    DROP COLUMN IF EXISTS car_trusted_name,
    DROP COLUMN IF EXISTS car_trusted_okpo,
    DROP COLUMN IF EXISTS pereadr_type,
    DROP COLUMN IF EXISTS pereadr_port;

ALTER TABLE dpport.status6
    DROP COLUMN IF EXISTS car_trusted_name,
    DROP COLUMN IF EXISTS car_trusted_okpo,
    DROP COLUMN IF EXISTS pereadr_type,
    DROP COLUMN IF EXISTS pereadr_port;

ALTER TABLE dpport.vagon_history
    DROP COLUMN IF EXISTS car_trusted_name,
    DROP COLUMN IF EXISTS car_trusted_okpo,
    DROP COLUMN IF EXISTS pereadr_type,
    DROP COLUMN IF EXISTS pereadr_port;
