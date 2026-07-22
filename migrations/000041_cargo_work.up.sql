-- ============================================================================
--  000041_cargo_work — «Грузовая работа» (перенос gtport vigr_at/vigr_ut/vigr_gut).
--
--  Суточный учётный лист терминала: что осталось с прошлых суток, что прибыло,
--  что выгружено, сколько порт мог переработать (аналитика) и насколько план
--  выполнен. Одна строка = одни ЖД-сутки × терминал × линия учёта.
--
--  УНИФИКАЦИЯ (уходим от gtport, где порт зашит в ИМЯ ТАБЛИЦЫ, а род груза —
--  в ИМЯ КОЛОНКИ: vigr_gut.coal_*/metal_*/chugun_*, ~60 колонок на три порта):
--    · терминал      → строковое поле (ports.name_s), одна таблица на все;
--    · род груза     → строка справочника port_cargo_line, а не колонка;
--    · способность   → число на линии (ваг/сут), а не switch getDailySpeed();
--    · «у АЭ нет погрузки» → просто нет строк kind='load', а не if (port=='at').
--
--  Время — Московское naive (жёсткий инвариант): даты — date по ЖД-суткам,
--  штампы — timestamp without time zone через clock.Now().
--
--  Идемпотентно (CREATE TABLE IF NOT EXISTS), только добавляющая.
-- ============================================================================

SET search_path TO dpport;

-- ── 1. Справочник линий учёта терминала ─────────────────────────────────────
--  Одна сущность на два вида учёта (решение владельца):
--    kind='unload' — колонки таблицы выгрузки. cargo_key = cargo.cargo_group
--                    ('' — терминал ведёт учёт одной строкой, без разбивки).
--    kind='load'   — строки таблицы погрузки. cargo_key — код рода погрузки
--                    (произвольный, с историей вагонов не связан: погрузка
--                    вбивается руками целиком).
--
--  pc — перерабатывающая способность ЛИНИИ, ваг/сут (вход движка аналитики).
--  NULL → падаем на ports.pc_coal/pc_metal/pc_other по роду, т.е. по умолчанию
--  цифры едины со Stage 4. Держим на линии, а не в ports, потому что род груза
--  в ports — это снова колонка-под-род (для ЧУГУНа её там нет).
CREATE TABLE IF NOT EXISTS dpport.port_cargo_line (
    id          bigserial PRIMARY KEY,
    terminal    text    NOT NULL,                  -- ports.name_s (АЭ/УТ-1/ГУТ-2)
    kind        text    NOT NULL DEFAULT 'unload', -- 'unload' | 'load'
    cargo_key   text    NOT NULL DEFAULT '',       -- unload: cargo_group ('' — без разбивки)
    label       text    NOT NULL DEFAULT '',       -- подпись колонки/строки в UI
    pc          integer,                           -- ваг/сут этой линии (NULL → ports.pc_*)
    sort_order  integer NOT NULL DEFAULT 0,
    enabled     boolean NOT NULL DEFAULT true,
    CONSTRAINT port_cargo_line_kind_chk CHECK (kind IN ('unload', 'load')),
    CONSTRAINT port_cargo_line_key UNIQUE (terminal, kind, cargo_key)
);

CREATE INDEX IF NOT EXISTS idx_port_cargo_line_terminal
    ON dpport.port_cargo_line (terminal, kind, sort_order);

-- Справочник — в админ-редактор (реестр list_tables, ключ id есть).
INSERT INTO dpport.list_tables (name, name_ru, editable) VALUES
    ('port_cargo_line', 'Линии учёта терминалов (грузовая работа)', true)
ON CONFLICT (name) DO NOTHING;

COMMENT ON COLUMN dpport.port_cargo_line.id         IS '№';
COMMENT ON COLUMN dpport.port_cargo_line.terminal   IS 'Терминал';
COMMENT ON COLUMN dpport.port_cargo_line.kind       IS 'Вид учёта (unload — выгрузка, load — погрузка)';
COMMENT ON COLUMN dpport.port_cargo_line.cargo_key  IS 'Группа груза (пусто — без разбивки)';
COMMENT ON COLUMN dpport.port_cargo_line.label      IS 'Подпись';
COMMENT ON COLUMN dpport.port_cargo_line.pc         IS 'Перераб. способность, ваг/сут';
COMMENT ON COLUMN dpport.port_cargo_line.sort_order IS 'Порядок';
COMMENT ON COLUMN dpport.port_cargo_line.enabled    IS 'Включена';

-- ── 2. Учётный лист выгрузки ────────────────────────────────────────────────
--  Авто-слой (пересобирается кнопкой «Пересчитать», ручное не трогает):
--    ost_18    — остаток предыдущих суток этой же линии (перенос);
--    ost_st    — «Остаток на 18:00» из плана подвода (plan_nitka.is_ostatok);
--    prib      — вехи прибытия vagon_history (date_prib_d × naznach × cargo_group);
--    vigr_stan — вехи выгрузки  vagon_history (date_vigr_d × place_vigr × …).
--                ⚠️ Отход от gtport (решение владельца): там станционная цифра
--                вбивалась руками. У нас это НАША цифра из АСУ, поэтому
--                perepokaz = vigr_stan − vigr_fact получает смысл расхождения
--                данных АСУ с фактом, заявленным портом;
--    useful/total_formation, downtime — движок суточной аналитики.
--
--  Ручной слой: plan, vigr_fact, prim. Расчётные: ost, effectiv, perepokaz —
--  считает СЕРВЕР (в gtport формулы жили и на клиенте, и в репозитории).
CREATE TABLE IF NOT EXISTS dpport.cargo_work (
    id          bigserial PRIMARY KEY,
    date_jd     date NOT NULL,                     -- учётные ЖД-сутки
    terminal    text NOT NULL,                     -- ports.name_s
    cargo_key   text NOT NULL DEFAULT '',          -- port_cargo_line.cargo_key (kind='unload')

    ost_18      integer NOT NULL DEFAULT 0,        -- остаток на 18:00, факт (авто: вчерашний ost)
    ost_st      integer NOT NULL DEFAULT 0,        -- остаток на станции (авто: план подвода)
    prib        integer NOT NULL DEFAULT 0,        -- прибыло (авто: история)
    useful_formation integer NOT NULL DEFAULT 0,   -- образование полезное (авто: аналитика)
    total_formation  integer NOT NULL DEFAULT 0,   -- образование полное   (авто: аналитика)
    downtime    text NOT NULL DEFAULT '',          -- простой порта «H:MM» (авто: аналитика)

    plan        integer NOT NULL DEFAULT 0,        -- план выгрузки (РУКАМИ)
    vigr_fact   integer NOT NULL DEFAULT 0,        -- выгрузка факт порта (РУКАМИ)
    vigr_stan   integer NOT NULL DEFAULT 0,        -- выгрузка по станции (авто: история)
    prim        text NOT NULL DEFAULT '',          -- комментарий (РУКАМИ)

    ost         integer NOT NULL DEFAULT 0,        -- расчёт: ost_18 + prib − vigr_fact
    effectiv    integer NOT NULL DEFAULT 0,        -- расчёт: vigr_fact / useful_formation × 100
    perepokaz   integer NOT NULL DEFAULT 0,        -- расчёт: vigr_stan − vigr_fact

    analytics_json       jsonb,                    -- снимок расчёта суток (операции, ожидания)
    train_structure_json jsonb,                    -- снимок состава поездов линии

    created_at  timestamp without time zone,       -- МСК naive (clock.Now())
    updated_at  timestamp without time zone,
    CONSTRAINT cargo_work_key UNIQUE (date_jd, terminal, cargo_key)
);

CREATE INDEX IF NOT EXISTS idx_cargo_work_date ON dpport.cargo_work (date_jd DESC);

-- ── 3. Учётный лист погрузки ────────────────────────────────────────────────
--  Целиком ручной (автоматики нет и в gtport). Состав строк — из
--  port_cargo_line с kind='load'; терминал без таких строк блока не показывает.
CREATE TABLE IF NOT EXISTS dpport.cargo_work_load (
    id          bigserial PRIMARY KEY,
    date_jd     date NOT NULL,
    terminal    text NOT NULL,
    cargo_key   text NOT NULL,                     -- port_cargo_line.cargo_key (kind='load')
    load_fact   integer NOT NULL DEFAULT 0,        -- погрузка
    plan        integer NOT NULL DEFAULT 0,        -- план
    ost         integer NOT NULL DEFAULT 0,        -- остаток
    created_at  timestamp without time zone,
    updated_at  timestamp without time zone,
    CONSTRAINT cargo_work_load_key UNIQUE (date_jd, terminal, cargo_key)
);

CREATE INDEX IF NOT EXISTS idx_cargo_work_load_date ON dpport.cargo_work_load (date_jd DESC);
