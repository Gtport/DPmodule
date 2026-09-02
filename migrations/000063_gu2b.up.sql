-- ============================================================================
--  000063_gu2b — приём уведомлений ГУ-2б о завершении грузовой операции
--  (источник факта выгрузки, решение владельца 17.08.2026; контракт отдачи —
--  docs/GU2B.md, крон — service.GU2BService).
--
--  В отличие от памяток ГУ-45 уведомления ХРАНИМ: по ним считается контроль
--  полноты сквозной нумерации (у провайдера уже была дыра №1293–1316 attis),
--  дедуп повторов «<72 ч = один факт» против прошлых тиков и материал для
--  включения перезаписи вех (gu2b.apply). Ключ документа — notification_id
--  провайдера: уникален и не циклится (в отличие от номеров памяток).
--
--  Время — Московское naive (жёсткий инвариант): timestamp without time zone.
--  Идемпотентно (IF NOT EXISTS), только добавляющая.
-- ============================================================================

SET search_path TO dpport;

-- Шапка уведомления. Колонки — как в нормализованных таблицах провайдера,
-- чтобы сверка с его корпусом шла без словаря переименований.
CREATE TABLE IF NOT EXISTS dpport.gu2b_notification (
    notification_id     bigint       PRIMARY KEY,      -- ключ документа у провайдера
    client              varchar(50)  NOT NULL,          -- клиент провайдера (attis/nmtp)
    number              bigint,                         -- сквозной номер клиента; NULL — нечисловой
    state_id            smallint,
    state               text         NOT NULL DEFAULT '', -- «Подписан» | «Заготовка» | «Испорчен»
    date_create         timestamp,                      -- создание = момент завершения грузовой операции
    station_code        text         NOT NULL DEFAULT '', -- код ИЗ ДОКУМЕНТА (5-значный, не настроечный)
    station_name        text         NOT NULL DEFAULT '', -- терминал матчится по имени станции
    org_okpo            text         NOT NULL DEFAULT '',
    org_name            text         NOT NULL DEFAULT '',
    place_transfer      text         NOT NULL DEFAULT '',
    way                 text         NOT NULL DEFAULT '',
    loc                 text         NOT NULL DEFAULT '',
    doc_last_oper       timestamp,                      -- подписание документа
    signer_name         text         NOT NULL DEFAULT '',
    signer_post         text         NOT NULL DEFAULT '',
    gateway_source      varchar(10)  NOT NULL DEFAULT '', -- asu | lk: чем провайдер добыл документ
    provider_updated_at timestamp,                      -- UPDATED_AT провайдера (опора инкремента)
    received_at         timestamp    NOT NULL DEFAULT now()
);

COMMENT ON TABLE  dpport.gu2b_notification IS 'Уведомления ГУ-2б о завершении грузовой операции (факт выгрузки) от провайдера';
COMMENT ON COLUMN dpport.gu2b_notification.number      IS 'Сквозной номер уведомления клиента — контроль полноты приёма';
COMMENT ON COLUMN dpport.gu2b_notification.date_create IS 'Создание уведомления = момент завершения грузовой операции (МСК)';

CREATE INDEX IF NOT EXISTS ix_gu2b_notification_client_number
    ON dpport.gu2b_notification (client, number);
CREATE INDEX IF NOT EXISTS ix_gu2b_notification_date_create
    ON dpport.gu2b_notification (date_create);

-- Вагоны уведомления. Признак выгрузки — здесь (operation_*), накладная строки
-- НЕ равна накладной прибытия (к рейсу вяжем замком по времени, не по ней).
CREATE TABLE IF NOT EXISTS dpport.gu2b_car (
    notification_id bigint      NOT NULL REFERENCES dpport.gu2b_notification (notification_id) ON DELETE CASCADE,
    car_order       smallint    NOT NULL,
    vagon           varchar(20) NOT NULL DEFAULT '',
    operation_id    smallint,
    operation_name  text        NOT NULL DEFAULT '',  -- «Выгрузка» | «БОП» | …
    operation_short text        NOT NULL DEFAULT '',  -- «ВЫГР» | …
    freight_code    text        NOT NULL DEFAULT '',
    freight_name    text        NOT NULL DEFAULT '',
    car_remark      text        NOT NULL DEFAULT '',
    invoice_id      bigint,
    invoice_number  text        NOT NULL DEFAULT '',
    PRIMARY KEY (notification_id, car_order)
);

COMMENT ON TABLE dpport.gu2b_car IS 'Вагоны уведомления ГУ-2б; признак выгрузки — operation_name/operation_short';

CREATE INDEX IF NOT EXISTS ix_gu2b_car_vagon ON dpport.gu2b_car (vagon);

-- Курсор инкремента: since дословно как пришёл LAST_UPDATE провайдера (правила
-- те же, что у pamyatka_cursor: в памяти держать нельзя — рестарт теряет
-- позицию). Пустая строка/нет строки → первый заход, полная перезаливка since=0.
CREATE TABLE IF NOT EXISTS dpport.gu2b_cursor (
    client     varchar(50) PRIMARY KEY,
    since      varchar(40) NOT NULL DEFAULT '',
    updated_at timestamp   NOT NULL DEFAULT now()
);

COMMENT ON TABLE  dpport.gu2b_cursor       IS 'Курсор инкремента уведомлений ГУ-2б по клиентам провайдера';
COMMENT ON COLUMN dpport.gu2b_cursor.since IS 'LAST_UPDATE последнего непустого ответа (дословно), с нахлёстом cursor_overlap';
