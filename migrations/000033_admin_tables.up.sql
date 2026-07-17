-- ============================================================================
--  000033_admin_tables — админ-редактор справочников (перенос эталона gtport).
--
--  list_tables — реестр редактируемых таблиц: универсальный CRUD и страница
--  «Админ» работают только с перечисленными здесь таблицами (editable=true).
--  Новый справочник в редакторе = одна INSERT-строка, без правки кода.
--
--  marka.id — суррогатный ключ для редактора: естественный ключ таблицы
--  составной (okpo, station_kod, cargo_group), а универсальному CRUD нужен
--  одноколоночный идентификатор строки (как в старом GTport).
-- ============================================================================

CREATE TABLE IF NOT EXISTS dpport.list_tables (
    name     text PRIMARY KEY,   -- имя таблицы в схеме dpport
    name_ru  text NOT NULL,      -- подпись для владельца
    editable boolean NOT NULL DEFAULT true
);

INSERT INTO dpport.list_tables (name, name_ru, editable) VALUES
    ('marka',    'Marka (бизнес-атрибуция грузов)', true),
    ('stations', 'Станции', true)
ON CONFLICT (name) DO NOTHING;

ALTER TABLE dpport.marka ADD COLUMN IF NOT EXISTS id bigserial;
CREATE UNIQUE INDEX IF NOT EXISTS marka_id_key ON dpport.marka (id);
