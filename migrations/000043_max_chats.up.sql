-- ============================================================================
--  000043_max_chats — справочник чатов мессенджера MAX и маршруты рассылки форм.
--
--  Транспорт (адаптер MAX) уже есть; здесь — КУДА слать. Две таблицы:
--    · max_chat  — справочник чатов (имя → числовой chat_id провайдера). Перенос
--                  gtport max_chats. Реальные chat_id — per-deployment, живут в
--                  _reference/seed/max_chat.csv (вне git), грузятся seed-скриптом.
--    · max_route — маршрут «тип формы × терминал → чат». Уносит порт-хардкод из
--                  фронта gtport (MAX_CHATS_CONFIG: АЭ→at, ГУТ-2→gut, ...): теперь
--                  data-driven и правится в Админе, а не в коде.
--
--  Почему отдельная таблица маршрутов, а не колонки в ports: ports НЕ в реестре
--  админ-редактора (list_tables), а маршруты диспетчеру нужно править руками. К
--  тому же это транспортная привязка, а не свойство причала — держим раздельно.
--
--  Обе — в админ-редактор (одноколоночный PRIMARY KEY: max_chat.name / max_route.id).
--  Идемпотентно (CREATE TABLE IF NOT EXISTS), только добавляющая.
-- ============================================================================

SET search_path TO dpport;

-- ── 1. Справочник чатов MAX ─────────────────────────────────────────────────
--  name — короткий код чата (at/gut/ut/oper/at_o/...), ключ маршрутов.
--  chat_id — числовой идентификатор чата в MAX (строкой: бывает длиннее int64
--  и с ведущим минусом у групп). is_active — временно отключить чат без удаления.
CREATE TABLE IF NOT EXISTS dpport.max_chat (
    name        text PRIMARY KEY,               -- код чата (маршруты ссылаются сюда)
    chat_id     text    NOT NULL DEFAULT '',     -- id чата в MAX (число строкой)
    description text    NOT NULL DEFAULT '',     -- человекочитаемое имя чата
    is_active   boolean NOT NULL DEFAULT true
);

INSERT INTO dpport.list_tables (name, name_ru, editable) VALUES
    ('max_chat', 'Чаты MAX', true)
ON CONFLICT (name) DO NOTHING;

COMMENT ON COLUMN dpport.max_chat.name        IS 'Код чата';
COMMENT ON COLUMN dpport.max_chat.chat_id     IS 'ID чата в MAX';
COMMENT ON COLUMN dpport.max_chat.description IS 'Название';
COMMENT ON COLUMN dpport.max_chat.is_active   IS 'Активен';

-- ── 2. Маршруты рассылки форм ───────────────────────────────────────────────
--  report   — тип формы/рассылки: 'spravki' (справки терминала), 'oper'
--             (оперативка терминала), 'plan' (сводный план подвода). Не enum —
--             новые формы добавляются строкой, без миграции.
--  terminal — ports.name_s (АЭ/УТ-1/ГУТ-2); пусто — рассылка НЕ по одному
--             терминалу (сводная форма 'plan' идёт в общий чат).
--  chat_name— max_chat.name (мягкая ссылка, как port_cargo_line.cargo_key).
--
--  Разрешение маршрута = строки с совпадающим (report, terminal) и enabled.
--  Одна форма терминала может уходить в несколько чатов — отсюда не UNIQUE по
--  (report, terminal), а по тройке с chat_name.
CREATE TABLE IF NOT EXISTS dpport.max_route (
    id         bigserial PRIMARY KEY,
    report     text    NOT NULL,                 -- 'spravki' | 'oper' | 'plan'
    terminal   text    NOT NULL DEFAULT '',      -- ports.name_s ('' — сводная)
    chat_name  text    NOT NULL,                 -- max_chat.name
    sort_order integer NOT NULL DEFAULT 0,
    enabled    boolean NOT NULL DEFAULT true,
    CONSTRAINT max_route_key UNIQUE (report, terminal, chat_name)
);

CREATE INDEX IF NOT EXISTS idx_max_route_lookup
    ON dpport.max_route (report, terminal, sort_order);

INSERT INTO dpport.list_tables (name, name_ru, editable) VALUES
    ('max_route', 'Маршруты рассылки MAX', true)
ON CONFLICT (name) DO NOTHING;

COMMENT ON COLUMN dpport.max_route.id         IS '№';
COMMENT ON COLUMN dpport.max_route.report     IS 'Форма (spravki/oper/plan)';
COMMENT ON COLUMN dpport.max_route.terminal   IS 'Терминал (пусто — сводная)';
COMMENT ON COLUMN dpport.max_route.chat_name  IS 'Чат';
COMMENT ON COLUMN dpport.max_route.sort_order IS 'Порядок';
COMMENT ON COLUMN dpport.max_route.enabled    IS 'Включён';
