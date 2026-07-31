-- ============================================================================
--  db_bootstrap.sql — подготовка базы под DPmodule. Идемпотентно.
--
--  Создаёт роль приложения, свою схему внутри УЖЕ СУЩЕСТВУЮЩЕЙ базы и выдаёт
--  права. Рассчитан на стенд, где база общая с чужими приложениями (каждое в
--  своей схеме): ничего глобального не трогает — search_path и TimeZone
--  выставляются НА РОЛЬ В ЭТОЙ БАЗЕ, соседние схемы и роли не затрагиваются.
--
--  Запускается ПОД СУПЕРПОЛЬЗОВАТЕЛЕМ, подключившись к целевой базе. Файл
--  читается локально, команды выполняются на сервере — переносить его на
--  сервер не нужно.
--
--  С ноутбука (Windows), сервер удалённый:
--      psql -h 176.53.160.9 -U postgres -d kgdm -v ON_ERROR_STOP=1 -f scripts/db_bootstrap.sql
--  С самого сервера:
--      sudo -u postgres psql -d kgdm -v ON_ERROR_STOP=1 -f scripts/db_bootstrap.sql
--
--  Необязательные переменные (дефолты ниже):
--      -v app_role=gtport_app  -v app_schema=dpport
--
--  ⚠️ Пароль роли скрипт НЕ ставит: в командной строке он утёк бы в историю и
--  в логи сервера, а спецсимволы ещё и портятся оболочкой (bash раскрывает $
--  даже в двойных кавычках, PowerShell — тоже). Задать отдельно, интерактивно:
--      psql -h 176.53.160.9 -U postgres -d kgdm -c "\password gtport_app"
--  Скрипт в конце сам скажет, задан пароль или нет.
--
--  ⚠️ Имя схемы менять нельзя: dpport зашито в миграции (SET search_path TO
--  dpport, CREATE TABLE dpport.*) и в SQL репозиториев. Имя базы — свободное.
-- ============================================================================

\if :{?app_role}
\else
\set app_role gtport_app
\endif

\if :{?app_schema}
\else
\set app_schema dpport
\endif

\echo ''
\echo '=== bootstrap: база' :DBNAME '| роль' :app_role '| схема' :app_schema
\echo ''

-- 1. Роль приложения ---------------------------------------------------------
-- CREATE ROLE IF NOT EXISTS в Postgres нет — guard через \gexec.
SELECT format('CREATE ROLE %I LOGIN', :'app_role')
 WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'app_role')\gexec

-- 2. Схема приложения --------------------------------------------------------
SELECT format('CREATE SCHEMA IF NOT EXISTS %I AUTHORIZATION %I', :'app_schema', :'app_role')\gexec
SELECT format('GRANT ALL ON SCHEMA %I TO %I', :'app_schema', :'app_role')\gexec

-- 3. Право CREATE на самой базе ----------------------------------------------
-- Нужно миграции 000001: она начинается с CREATE SCHEMA IF NOT EXISTS dpport,
-- а Postgres проверяет привилегию ДО проверки существования — без этого права
-- накат падает с «permission denied for database», даже когда схема уже есть.
-- После наката можно отозвать (приложению в работе не нужно):
--     REVOKE CREATE ON DATABASE <база> FROM <роль>;
-- но тогда выдавать заново перед каждой миграцией, добавляющей схему.
SELECT format('GRANT CREATE ON DATABASE %I TO %I', current_database(), :'app_role')\gexec

-- 4. search_path — на роль в этой базе ---------------------------------------
-- Приоритет в Postgres: роль+база > роль > база > postgresql.conf. Поэтому
-- настройка сильнее общей настройки базы и при этом не видна соседям.
-- Зачем нужна: 12 из 55 миграций не задают схему явно и полагаются на
-- search_path соединения; golang-migrate по нему же кладёт schema_migrations.
SELECT format('ALTER ROLE %I IN DATABASE %I SET search_path TO %I, public',
              :'app_role', current_database(), :'app_schema')\gexec

-- 5. TimeZone — на роль в этой базе ------------------------------------------
-- Инвариант проекта: всё время — московское naive (timestamp without time zone).
-- Колонок с зоной в схеме нет, но DEFAULT now() укладывает в timestamp время
-- сессии: при UTC штампы разошлись бы с clock.Now() на 3 часа, причём молча.
SELECT format('ALTER ROLE %I IN DATABASE %I SET TimeZone TO ''Europe/Moscow''',
              :'app_role', current_database())\gexec

-- 6. Контроль ----------------------------------------------------------------
\echo ''
\echo '--- роль (has_password=f → задать паролем через \\password)'
SELECT rolname, rolcanlogin, (rolpassword IS NOT NULL) AS has_password, rolvaliduntil
  FROM pg_authid WHERE rolname = :'app_role';

\echo '--- настройки роли в этой базе'
SELECT r.rolname, d.datname, s.setconfig
  FROM pg_db_role_setting s
  JOIN pg_roles r ON r.oid = s.setrole
  JOIN pg_database d ON d.oid = s.setdatabase
 WHERE r.rolname = :'app_role' AND d.datname = current_database();

\echo '--- схемы базы (наша должна принадлежать роли приложения)'
SELECT nspname AS schema, pg_get_userbyid(nspowner) AS owner
  FROM pg_namespace
 WHERE nspname NOT LIKE 'pg\_%' AND nspname <> 'information_schema'
 ORDER BY 1;

\echo ''
\echo '=== готово. Дальше: пароль (если has_password=f) и накат миграций scripts/db_migrate.sh'
\echo ''
