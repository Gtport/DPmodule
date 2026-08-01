-- 000056: аккаунты личного кабинета РЖД (cargolk.rzd.ru) для автовыгрузки
-- дислокации — «робот ЛК».
--
--  Раньше диспетчер выгружал дислокацию из ЛК руками и грузил файл в модалку
--  приёма. Робот делает то же самое сам: входит в кабинет, заказывает отчёт
--  «Дислокация вагонов по списку» (SPV4664) по ОКПО грузополучателя и отдаёт
--  полученный xlsx тому же приёму (ingest=upload источника data_source id='lk').
--
--  Почему отдельная таблица, а не config источника 'lk': у каждого потока
--  (порта) свой аккаунт ЛК, а data_source не в реестре list_tables — его правят
--  только SQL-ом. Логины должен править владелец из админ-редактора, потому что
--  потоков со временем станет больше двух.
--
--  Пароля здесь НЕТ и не будет: он вводится диспетчером в момент запуска и
--  живёт только в памяти процесса (см. handler/lk_robot.go).
--
--  Сами логины — per-deployment, как chat_id чатов MAX (000043): заводятся в
--  базе на каждом стенде, в git не едут. Пример заполнения:
--    INSERT INTO dpport.lk_account (okpo, login, name, sort_order)
--    VALUES (<ОКПО>, '<логин ЛК>', '<подпись потока>', 10);

SET search_path TO dpport;

CREATE TABLE IF NOT EXISTS dpport.lk_account (
    id         bigserial PRIMARY KEY,
    okpo       bigint  NOT NULL,                 -- ОКПО грузополучателя; связь с ports.okpo (не уникален: 1 ОКПО → N терминалов)
    login      text    NOT NULL,                 -- логин в ЛК РЖД; свой на каждый поток
    name       text    NOT NULL DEFAULT '',      -- подпись потока для модалки («Аттис», «НМТП»)
    sort_order integer NOT NULL DEFAULT 0,
    enabled    boolean NOT NULL DEFAULT true,
    CONSTRAINT lk_account_okpo_key UNIQUE (okpo)
);

INSERT INTO dpport.list_tables (name, name_ru, editable) VALUES
    ('lk_account', 'Аккаунты ЛК РЖД (автовыгрузка)', true)
ON CONFLICT (name) DO NOTHING;

COMMENT ON COLUMN dpport.lk_account.id         IS '№';
COMMENT ON COLUMN dpport.lk_account.okpo       IS 'ОКПО грузополучателя';
COMMENT ON COLUMN dpport.lk_account.login      IS 'Логин в ЛК РЖД';
COMMENT ON COLUMN dpport.lk_account.name       IS 'Поток (подпись)';
COMMENT ON COLUMN dpport.lk_account.sort_order IS 'Порядок';
COMMENT ON COLUMN dpport.lk_account.enabled    IS 'Включён';
