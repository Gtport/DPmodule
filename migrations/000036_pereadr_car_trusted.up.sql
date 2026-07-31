-- ============================================================================
--  000036_pereadr_car_trusted — поля переадресации и доверенного лица вагона.
--
--  Переадресация (решение владельца, взамен info_1/info_2 gtport):
--    pereadr_type — '' нет / 'own' на свой терминал / 'ext' на внешний порт;
--    pereadr_port — имя внешнего порта (только при 'ext').
--  Операторские поля: поток РЖД их не присылает, переносятся carry-over'ом,
--  снимаются только явной отменой переадресации.
--
--  Доверенное лицо (третий набор сведений о собственнике из АСУ,
--  carTrustedOKPO/carTrustedName — ключи новой версии эндпоинта):
--    car_trusted_name / car_trusted_okpo.
--
--  Колонки добавляются во все четыре таблицы-близнеца раскладки dislocation
--  (снимок, swap-таблица, кандидаты, доноры) и в бизнес-историю vagon_history.
-- ============================================================================

ALTER TABLE dpport.dislocation
    ADD COLUMN IF NOT EXISTS car_trusted_name text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS car_trusted_okpo text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS pereadr_type     text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS pereadr_port     text NOT NULL DEFAULT '';

-- dislocation_new — staging «варианта Б»: её создаёт приложение в рантайме
-- (ensureStaging, CREATE TABLE LIKE dislocation), а не миграции. На боевой базе
-- она есть, на свежем стенде до первой заливки снимка её НЕТ — поэтому под
-- guard, как в 000012 и 000037. Без него накат с нуля падает здесь с
-- «relation dpport.dislocation_new does not exist».
DO $$
BEGIN
    IF to_regclass('dpport.dislocation_new') IS NOT NULL THEN
        EXECUTE $f$
            ALTER TABLE dpport.dislocation_new
                ADD COLUMN IF NOT EXISTS car_trusted_name text NOT NULL DEFAULT '',
                ADD COLUMN IF NOT EXISTS car_trusted_okpo text NOT NULL DEFAULT '',
                ADD COLUMN IF NOT EXISTS pereadr_type     text NOT NULL DEFAULT '',
                ADD COLUMN IF NOT EXISTS pereadr_port     text NOT NULL DEFAULT ''$f$;
    END IF;
END $$;

ALTER TABLE dpport.status9
    ADD COLUMN IF NOT EXISTS car_trusted_name text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS car_trusted_okpo text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS pereadr_type     text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS pereadr_port     text NOT NULL DEFAULT '';

ALTER TABLE dpport.status6
    ADD COLUMN IF NOT EXISTS car_trusted_name text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS car_trusted_okpo text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS pereadr_type     text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS pereadr_port     text NOT NULL DEFAULT '';

ALTER TABLE dpport.vagon_history
    ADD COLUMN IF NOT EXISTS car_trusted_name text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS car_trusted_okpo text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS pereadr_type     text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS pereadr_port     text NOT NULL DEFAULT '';
