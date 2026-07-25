-- Read-only роль для разведки боевой базы с локальной машины через SSH-туннель.
--
-- Выполняется НА VPS суперпользователем, один раз. Файл подаём через stdin, а не
-- через -f: каталог /home/alex закрыт правами 750, и postgres прочитать его не может
-- («psql: error: … Permission denied»). Через stdin файл читает shell пользователя.
--
--     sudo -u postgres psql -d dpport -v ro_pass=ПАРОЛЬ < scripts/dev_pg_readonly.sql
--
-- ПАРОЛЬ заменить на настоящий и передать БЕЗ одинарных кавычек: psql заквотирует
-- значение сам (:'ro_pass'), а лишние кавычки войдут в состав пароля. Команду начать
-- с пробела — тогда пароль не попадёт в историю bash.
--
-- Смысл: через туннель ходим этой ролью, а не gtport_app. Тогда случайный UPDATE
-- или DROP из локального psql/DBeaver физически не пройдёт — база боевая, диспетчер
-- работает в ней прямо сейчас. Запись в боевую — только через приложение на сервере.

\set ON_ERROR_STOP on

-- Создание роли — не DO-блоком: psql НЕ подставляет свои переменные внутри строк
-- в долларовых кавычках, поэтому :'ro_pass' уехал бы в PL/pgSQL дословно и упал
-- с «syntax error at or near ":"». Собираем команду запросом и выполняем \gexec.
SELECT format('CREATE ROLE dpport_ro LOGIN PASSWORD %L', :'ro_pass')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'dpport_ro')
\gexec

-- Пароль выставляем всегда: повторный прогон скрипта = смена пароля роли.
SELECT format('ALTER ROLE dpport_ro LOGIN PASSWORD %L', :'ro_pass')
\gexec

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
