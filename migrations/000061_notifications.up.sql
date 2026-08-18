-- ============================================================================
--  000061_notifications — внутренние уведомления (перенос колокольчика gtport).
--
--  В gtport были таблицы notifications + user_notifications с рассылкой на
--  ЗАПИСИ (INSERT..SELECT по таблице users). У нас таблицы пользователей нет
--  (identity — JWT Keycloak), поэтому рассылка перенесена на ЧТЕНИЕ:
--  уведомление хранит АУДИТОРИЮ (oper/dicts/admin — соответствие наборам
--  internal/auth.Access*), а видимость вычисляется по ролям из claims текущего
--  пользователя. Состояние «прочитано» — ленивое: строка в notification_read
--  появляется только при отметке, ключ (username, notification_id).
--
--  dedup_key — персистентная замена RAM-мап дедупликации gtport (они
--  сбрасывались рестартом и спамили заново): частичный уникальный индекс
--  гасит повторную вставку того же события (ON CONFLICT DO NOTHING).
--  Крон-очистка (72 ч, config notifications.retention_hours) удаляет строки
--  и ОСВОБОЖДАЕТ dedup_key — неисправленная проблема справочника напомнит
--  о себе через трое суток (осознанное поведение-напоминание).
--
--  НЕ справочник: в реестр list_tables не заводится, пишет только конвейер.
--  Время — Московское naive. Идемпотентно (IF NOT EXISTS), только добавляющая.
-- ============================================================================
SET search_path TO dpport;

CREATE TABLE IF NOT EXISTS dpport.notifications (
    id               bigserial PRIMARY KEY,
    ntype            text NOT NULL DEFAULT 'info',
    title            text NOT NULL,
    message          text NOT NULL DEFAULT '',
    audience         text NOT NULL DEFAULT 'oper',
    target_username  text NOT NULL DEFAULT '',
    action_component text NOT NULL DEFAULT '',
    action_params    jsonb,
    dedup_key        text NOT NULL DEFAULT '',
    created_at       timestamp without time zone NOT NULL
);

COMMENT ON COLUMN dpport.notifications.ntype            IS 'Тип: info|warning|error|service (цвет/иконка в UI)';
COMMENT ON COLUMN dpport.notifications.title            IS 'Заголовок («Брошен поезд!»)';
COMMENT ON COLUMN dpport.notifications.message          IS 'Текст уведомления';
COMMENT ON COLUMN dpport.notifications.audience         IS 'Аудитория: all|oper|dicts|admin (наборы auth.Access*)';
COMMENT ON COLUMN dpport.notifications.target_username  IS 'Адресно одному пользователю (preferred_username); пусто = по аудитории';
COMMENT ON COLUMN dpport.notifications.action_component IS 'Deep-link фронта: bros|arrivals|unmatched|admin_dict';
COMMENT ON COLUMN dpport.notifications.action_params    IS 'Параметры deep-link (jsonb)';
COMMENT ON COLUMN dpport.notifications.dedup_key        IS 'Ключ дедупликации события; пусто = без дедупа';
COMMENT ON COLUMN dpport.notifications.created_at       IS 'Момент создания (МСК naive)';

CREATE UNIQUE INDEX IF NOT EXISTS uq_notifications_dedup
    ON dpport.notifications (dedup_key) WHERE dedup_key <> '';
CREATE INDEX IF NOT EXISTS idx_notifications_created
    ON dpport.notifications (created_at DESC);

CREATE TABLE IF NOT EXISTS dpport.notification_read (
    username        text NOT NULL,
    notification_id bigint NOT NULL REFERENCES dpport.notifications(id) ON DELETE CASCADE,
    read_at         timestamp without time zone NOT NULL,
    PRIMARY KEY (username, notification_id)
);

COMMENT ON COLUMN dpport.notification_read.username        IS 'Кто прочитал (preferred_username из JWT)';
COMMENT ON COLUMN dpport.notification_read.notification_id IS 'Прочитанное уведомление (каскад с notifications)';
COMMENT ON COLUMN dpport.notification_read.read_at         IS 'Когда прочитано (МСК naive)';

-- Роль есть только на своих стендах; на управляемой базе шаг пропускается.
DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'gtport_app') THEN
        ALTER TABLE dpport.notifications OWNER TO gtport_app;
        ALTER TABLE dpport.notification_read OWNER TO gtport_app;
    END IF;
END $$;
