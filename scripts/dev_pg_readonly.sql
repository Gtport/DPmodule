-- Read-only роль для разведки боевой базы с локальной машины через SSH-туннель.
--
-- Выполняется НА VPS суперпользователем, один раз:
--     sudo -u postgres psql -d dpport -v ro_pass="'ПАРОЛЬ'" -f scripts/dev_pg_readonly.sql
--
-- Смысл: через туннель ходим этой ролью, а не gtport_app. Тогда случайный UPDATE
-- или DROP из локального psql/DBeaver физически не пройдёт — база боевая, диспетчер
-- работает в ней прямо сейчас. Запись в боевую — только через приложение на сервере.

\set ON_ERROR_STOP on

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'dpport_ro') THEN
    EXECUTE format('CREATE ROLE dpport_ro LOGIN PASSWORD %L', :'ro_pass');
  ELSE
    EXECUTE format('ALTER ROLE dpport_ro LOGIN PASSWORD %L', :'ro_pass');
  END IF;
END $$;

-- Явно отбираем право создавать что-либо и писать.
GRANT CONNECT ON DATABASE dpport TO dpport_ro;
GRANT USAGE ON SCHEMA dpport, public TO dpport_ro;
GRANT SELECT ON ALL TABLES IN SCHEMA dpport TO dpport_ro;
GRANT SELECT ON ALL SEQUENCES IN SCHEMA dpport TO dpport_ro;

-- Таблицы, созданные будущими миграциями, тоже должны быть видны без повторного GRANT.
ALTER DEFAULT PRIVILEGES FOR ROLE gtport_app IN SCHEMA dpport GRANT SELECT ON TABLES TO dpport_ro;
ALTER DEFAULT PRIVILEGES FOR ROLE gtport_app IN SCHEMA dpport GRANT SELECT ON SEQUENCES TO dpport_ro;

-- Страховка: даже BEGIN; UPDATE … в этой роли отвалится «read-only transaction».
ALTER ROLE dpport_ro SET default_transaction_read_only = on;
ALTER ROLE dpport_ro SET search_path = dpport, public;

\echo 'Роль dpport_ro готова: только SELECT, транзакции read-only по умолчанию.'
