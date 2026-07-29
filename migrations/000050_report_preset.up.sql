-- 000050: пресеты отчётных форм + маршруты MAX формы «Подход».
--
--  Перенос gtport «Подход Марис»: там клиентский фильтр отчёта был зашит в
--  кнопку фронта (client_filter='КЛЦ МАРИС|HORIZON COMMODITIES TRADING LIMITED').
--  Здесь вариант отчёта — строка справочника report_preset: имя карточки на
--  странице «Справки и отчёты» + список клиентов. Карточки генерятся из
--  пресетов; новый клиентский вариант = INSERT, без правки кода.
--
--  clients — текст с разделителем '|' (формат gtport): универсальный
--  админ-редактор (list_tables) правит его как обычную строку.

SET search_path TO dpport;

CREATE TABLE IF NOT EXISTS dpport.report_preset (
    id         bigserial PRIMARY KEY,
    report     text    NOT NULL,                 -- форма ('podhod'); не enum — новые формы строкой
    name       text    NOT NULL,                 -- подпись карточки («Марис»)
    clients    text    NOT NULL DEFAULT '',      -- фильтр клиентов, разделитель '|'
    sort_order integer NOT NULL DEFAULT 0,
    enabled    boolean NOT NULL DEFAULT true,
    CONSTRAINT report_preset_key UNIQUE (report, name)
);

INSERT INTO dpport.list_tables (name, name_ru, editable) VALUES
    ('report_preset', 'Пресеты отчётов (клиентские варианты)', true)
ON CONFLICT (name) DO NOTHING;

COMMENT ON COLUMN dpport.report_preset.id         IS '№';
COMMENT ON COLUMN dpport.report_preset.report     IS 'Форма (podhod/...)';
COMMENT ON COLUMN dpport.report_preset.name       IS 'Название карточки';
COMMENT ON COLUMN dpport.report_preset.clients    IS 'Клиенты (через |)';
COMMENT ON COLUMN dpport.report_preset.sort_order IS 'Порядок';
COMMENT ON COLUMN dpport.report_preset.enabled    IS 'Включён';

-- Пресет «Марис» — единственный клиентский вариант «Подхода» в gtport.
INSERT INTO dpport.report_preset (report, name, clients, sort_order)
VALUES ('podhod', 'Марис', 'КЛЦ МАРИС|HORIZON COMMODITIES TRADING LIMITED', 10)
ON CONFLICT ON CONSTRAINT report_preset_key DO NOTHING;

-- Маршруты MAX формы «Подход»: справка терминала → чат терминала (зеркало
-- формы 'spravki'), пресеты (terminal='') → оперативный чат, как слал gtport.
-- Guard WHERE EXISTS: сид только для реально заведённых чатов (на пустой
-- dev-базе строки не вставятся — это не ошибка).
INSERT INTO dpport.max_route (report, terminal, chat_name, sort_order, enabled)
SELECT 'podhod', t.terminal, t.chat, t.so, true
FROM (VALUES ('АЭ', 'at', 10), ('ГУТ-2', 'gut', 20), ('УТ-1', 'ut', 30), ('', 'oper', 40))
     AS t(terminal, chat, so)
WHERE EXISTS (SELECT 1 FROM dpport.max_chat c WHERE c.name = t.chat)
ON CONFLICT ON CONSTRAINT max_route_key DO NOTHING;
