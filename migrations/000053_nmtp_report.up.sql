-- 000053: отчёт «Подход вагонов» по форме порта (НМТП) — справочники раскладки.
--
--  Перенос gtport PortReportNmtp с унификацией (решения владельца 29.07.2026):
--  в gtport колонки формы задавал числовой код sprav_1 в словаре marka и
--  захардкоженный маппинг sprav_1 → колонка. В DPmodule sprav_1 пуст (осознанно
--  не переносился, миграция 000032), а колонка описывается ПРАВИЛАМИ по
--  натуральным полям вагона: клиент + станции погрузки + марка груза.
--
--  nmtp_column — колонка формы терминала: три подписи шапки (клиент/станции/
--  марка — как в файле порта) + правила матчинга. Вагон идёт в самую
--  СПЕЦИФИЧНУЮ совпавшую колонку (больше заданных правил — раньше проверка;
--  при равенстве — по sort_order), поэтому «СЛЯБЫ» побеждает «ПРОКАТ» без
--  марки, а sort_order остаётся чистым порядком отображения. Не сматчившиеся
--  не теряются — падают в автоколонку «прочее» (падать громко, а не молча).
--  Списки в правилах — текст с разделителем '|' (формат gtport, редактор
--  list_tables правит как строку); пустое правило = любое значение.
--
--  nmtp_mark — словарь марок угля для нормализатора: код ЕТСНГ марку не
--  различает (весь концентрат — 161043), марка живёт только в свободном
--  тексте freight_exact_name («КОНЦЕНТРАТ УГОЛЬНЫЙ Ж», «...МАРКА ГЖ+Ж.»).
--  Нормализатор ищет известные марки в имени груза (длинные первыми, по
--  границам слова); фолбэк — cargo_sms словаря cargo (Д/Г/Т, металл: ЗАГ...).
--
--  ports.nmtp_norm — «Норма» блока «Нагрузка на ж/д сеть» (вагонов на сети;
--  из файлов порта: ГУТ-2 2600, УТ-1 3500). 0 — блок не считается.

SET search_path TO dpport;

CREATE TABLE IF NOT EXISTS dpport.nmtp_column (
    id             bigserial PRIMARY KEY,
    terminal       text    NOT NULL,             -- терминал (ports.name_s)
    sort_order     integer NOT NULL DEFAULT 0,   -- порядок колонок и порядок матчинга
    group_label    text    NOT NULL DEFAULT '',  -- шапка ур.1: клиент («КЛЦ МАРИС»)
    station_label  text    NOT NULL DEFAULT '',  -- шапка ур.2: станции («ЧЕЛУТАЙ»)
    mark_label     text    NOT NULL DEFAULT '',  -- шапка ур.3: марка («ДОМСШ»)
    match_clients  text    NOT NULL DEFAULT '',  -- клиенты через '|'; пусто — любые
    match_stations text    NOT NULL DEFAULT '',  -- станции погрузки через '|'; пусто — любые
    match_marks    text    NOT NULL DEFAULT '',  -- марки через '|'; пусто — любые
    enabled        boolean NOT NULL DEFAULT true
);
CREATE INDEX IF NOT EXISTS ix_nmtp_column_terminal ON dpport.nmtp_column (terminal, sort_order);

CREATE TABLE IF NOT EXISTS dpport.nmtp_mark (
    mark       text PRIMARY KEY,                -- каноническое имя марки (ГЖ+Ж, ОС+К, Д...)
    sort_order integer NOT NULL DEFAULT 0
);

ALTER TABLE dpport.ports
    ADD COLUMN IF NOT EXISTS nmtp_norm integer NOT NULL DEFAULT 0;

COMMENT ON COLUMN dpport.nmtp_column.id             IS '№';
COMMENT ON COLUMN dpport.nmtp_column.terminal       IS 'Терминал';
COMMENT ON COLUMN dpport.nmtp_column.sort_order     IS 'Порядок';
COMMENT ON COLUMN dpport.nmtp_column.group_label    IS 'Шапка: клиент';
COMMENT ON COLUMN dpport.nmtp_column.station_label  IS 'Шапка: станции';
COMMENT ON COLUMN dpport.nmtp_column.mark_label     IS 'Шапка: марка';
COMMENT ON COLUMN dpport.nmtp_column.match_clients  IS 'Клиенты (через |, пусто — любые)';
COMMENT ON COLUMN dpport.nmtp_column.match_stations IS 'Станции погрузки (через |, пусто — любые)';
COMMENT ON COLUMN dpport.nmtp_column.match_marks    IS 'Марки (через |, пусто — любые)';
COMMENT ON COLUMN dpport.nmtp_column.enabled        IS 'Включена';
COMMENT ON COLUMN dpport.nmtp_mark.mark             IS 'Марка';
COMMENT ON COLUMN dpport.nmtp_mark.sort_order       IS 'Порядок';
COMMENT ON COLUMN dpport.ports.nmtp_norm            IS 'НМТП: норма вагонов на сети (0 — не считать)';

INSERT INTO dpport.list_tables (name, name_ru, editable) VALUES
    ('nmtp_column', 'НМТП: колонки формы подхода', true),
    ('nmtp_mark',   'НМТП: словарь марок груза', true)
ON CONFLICT (name) DO NOTHING;

-- Словарь марок, нормы nmtp_norm и раскладки колонок (ГУТ-2/УТ-1/АЭ) —
-- клиентские данные, переехали в scripts/clients/gtport.sql. Пустая
-- nmtp_column = карточка «Отчёты НМТП» у клиента не показывается.
