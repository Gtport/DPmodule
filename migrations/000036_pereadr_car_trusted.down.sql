ALTER TABLE dpport.dislocation
    DROP COLUMN IF EXISTS car_trusted_name,
    DROP COLUMN IF EXISTS car_trusted_okpo,
    DROP COLUMN IF EXISTS pereadr_type,
    DROP COLUMN IF EXISTS pereadr_port;

ALTER TABLE dpport.dislocation_new
    DROP COLUMN IF EXISTS car_trusted_name,
    DROP COLUMN IF EXISTS car_trusted_okpo,
    DROP COLUMN IF EXISTS pereadr_type,
    DROP COLUMN IF EXISTS pereadr_port;

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
