-- ============================================================================
--  GTport — полная инициализация схемы (создание с нуля)
--  БД: dpport   |   Схема: dpport
-- ----------------------------------------------------------------------------
--  Соглашения:
--   • Время — БЕЗ таймзоны (timestamp without time zone), единая шкала АСУ.
--   • Коды — text/char (ведущие нули сохраняются), не числовые типы.
--   • Строковые поля NOT NULL DEFAULT '' (Go-модель не использует *string).
-- ============================================================================

-- БД создаётся отдельно (CREATE DATABASE нельзя в транзакции):
--   CREATE DATABASE dpport;
-- затем подключиться к dpport и выполнить этот файл.

CREATE SCHEMA IF NOT EXISTS dpport;
SET search_path TO dpport;

-- ─────────────────────────────────────────────────────────────────────────
--  1. dislocation — оперативная дислокация (текущее состояние вагонов)
-- ─────────────────────────────────────────────────────────────────────────
CREATE TABLE dpport.dislocation (
    id                 text PRIMARY KEY,
    vagon              text NOT NULL DEFAULT '',
    invoice            text NOT NULL DEFAULT '',
    invoice_main       text NOT NULL DEFAULT '',
    "index"            text NOT NULL DEFAULT '',
    index_main         text NOT NULL DEFAULT '',
    index_last         text NOT NULL DEFAULT '',
    index_pp           text NOT NULL DEFAULT '',
    date_nach          timestamp,
    date_otpr          timestamp,
    code_station_nach  text NOT NULL DEFAULT '',
    station_nach       text NOT NULL DEFAULT '',
    doroga_nach        text NOT NULL DEFAULT '',
    str_nach           text NOT NULL DEFAULT '',
    zayavka            text NOT NULL DEFAULT '',
    gruzotpr_okpo      text NOT NULL DEFAULT '',
    gruzotpr           text NOT NULL DEFAULT '',
    code_stan_nazn     text NOT NULL DEFAULT '',
    code4_stan_nazn    text NOT NULL DEFAULT '',
    stan_nazn          text NOT NULL DEFAULT '',
    doroga_nazn        text NOT NULL DEFAULT '',
    str_nazn           text NOT NULL DEFAULT '',
    gruzpol_okpo       text NOT NULL DEFAULT '',
    gruzpol            text NOT NULL DEFAULT '',
    gruzpol_s          text NOT NULL DEFAULT '',
    naznach            text NOT NULL DEFAULT '',
    perestanovka       text NOT NULL DEFAULT '',
    code_cargo         text NOT NULL DEFAULT '',
    code_cargo_gng     text NOT NULL DEFAULT '',
    code_cargo_vygr    text NOT NULL DEFAULT '',
    cargo_s            text NOT NULL DEFAULT '',
    cargo_sms          text NOT NULL DEFAULT '',
    cargo_group        text NOT NULL DEFAULT '',
    ves                numeric,
    porozh_priznak     text NOT NULL DEFAULT '',
    freight_exact_name text NOT NULL DEFAULT '',
    gtd_number         text NOT NULL DEFAULT '',
    time_op            timestamp,
    date_op            timestamp,
    date_op_jd         timestamp,
    code_oper          text NOT NULL DEFAULT '',
    oper               text NOT NULL DEFAULT '',
    oper_s             text NOT NULL DEFAULT '',
    code_station_oper  text NOT NULL DEFAULT '',
    station_oper       text NOT NULL DEFAULT '',
    doroga_oper        text NOT NULL DEFAULT '',
    id_otprk           text NOT NULL DEFAULT '',
    uno                text NOT NULL DEFAULT '',
    latitude           text NOT NULL DEFAULT '',
    longitude          text NOT NULL DEFAULT '',
    temper             numeric,
    rasst_stan_nazn    integer,
    rasst_ob           integer,
    rasst_stan_op      integer,
    prost_dn           integer,
    prost_ch           integer,
    prost_min          integer,
    id_disl            text NOT NULL DEFAULT '',
    npp_vag            integer,
    status             integer,
    id_status5         text NOT NULL DEFAULT '',
    id_status4         text NOT NULL DEFAULT '',
    date_dostav        timestamp,
    delay              integer,
    delay_prog         integer,
    plan_jd            timestamp,
    plan_msk           timestamp,
    to_go              numeric,
    rasch_msk          timestamp,
    prog_msk           timestamp,
    mistake            numeric,
    rasch_jd           timestamp,
    prog_jd            timestamp,
    date_kon           timestamp,
    date_prib          timestamp,
    is_bam             boolean NOT NULL DEFAULT false,
    car_owner_name     text NOT NULL DEFAULT '',
    car_owner_okpo     text NOT NULL DEFAULT '',
    car_tenant_name    text NOT NULL DEFAULT '',
    car_tenant_okpo    text NOT NULL DEFAULT '',
    client             text NOT NULL DEFAULT '',
    sms_1              text NOT NULL DEFAULT '',
    sms_2              text NOT NULL DEFAULT '',
    sms_3              text NOT NULL DEFAULT '',
    sprav_1            text NOT NULL DEFAULT '',
    sprav_2            text NOT NULL DEFAULT '',
    sprav_3            text NOT NULL DEFAULT '',
    param_1            text NOT NULL DEFAULT '',
    param_2            text NOT NULL DEFAULT '',
    param_3            text NOT NULL DEFAULT '',
    n_param_1          text NOT NULL DEFAULT '',
    n_param_2          text NOT NULL DEFAULT '',
    n_param_3          text NOT NULL DEFAULT '',
    date_vigr          timestamp,
    place_vigr         text NOT NULL DEFAULT '',
    frost              integer,
    info_1             text NOT NULL DEFAULT '',
    info_2             text NOT NULL DEFAULT '',
    info_3             text NOT NULL DEFAULT '',
    color              text NOT NULL DEFAULT '',
    rod_vag_uch        text NOT NULL DEFAULT '',
    shipments          text NOT NULL DEFAULT '',
    history            integer NOT NULL DEFAULT 0,
    created_at         timestamp NOT NULL DEFAULT now(),
    updated_at         timestamp NOT NULL DEFAULT now()
);

CREATE INDEX ix_dislocation_vagon            ON dpport.dislocation (vagon);
CREATE INDEX ix_dislocation_code_station_op  ON dpport.dislocation (code_station_oper);
CREATE INDEX ix_dislocation_status           ON dpport.dislocation (status);

-- ─────────────────────────────────────────────────────────────────────────
--  2. vagon_history — curated-снимок рейса (для отчётов)
--     trip_key вычисляется БД и совпадает с models.TripKey(...) в Go.
-- ─────────────────────────────────────────────────────────────────────────
CREATE TABLE dpport.vagon_history (
    id                 text PRIMARY KEY,
    vagon              text NOT NULL DEFAULT '',
    trip_key           bigint GENERATED ALWAYS AS (vagon::bigint * 100000 + (date_nach_d::date - DATE '1970-01-01')) STORED,
    invoice_main       text NOT NULL DEFAULT '',
    invoice            text NOT NULL DEFAULT '',
    index_main         text NOT NULL DEFAULT '',
    index_pp           text NOT NULL DEFAULT '',
    date_nach_d        timestamp,  -- дата погрузки (ЖД-сутки)
    station_nach       text NOT NULL DEFAULT '',
    gruzotpr           text NOT NULL DEFAULT '',  -- имя (из обогащения)
    zayavka            text NOT NULL DEFAULT '',
    stan_nazn          text NOT NULL DEFAULT '',
    gruzpol_s          text NOT NULL DEFAULT '',
    naznach            text NOT NULL DEFAULT '',
    cargo_s            text NOT NULL DEFAULT '',
    cargo_group        text,
    freight_exact_name text NOT NULL DEFAULT '',  -- точное наименование
    gtd_number         text NOT NULL DEFAULT '',  -- номер ГТД
    ves                numeric,
    client             text NOT NULL DEFAULT '',
    rod_vag_uch        text NOT NULL DEFAULT '',  -- код рода вагона (НЕ собственник)
    car_owner_name     text NOT NULL DEFAULT '',  -- собственник (имя)
    car_owner_okpo     text NOT NULL DEFAULT '',  -- собственник (ОКПО)
    car_tenant_name    text NOT NULL DEFAULT '',  -- оператор (имя)
    car_tenant_okpo    text NOT NULL DEFAULT '',  -- оператор (ОКПО)
    status             integer,
    date_dostav        timestamp,
    plan_msk           timestamp,
    plan_jd            timestamp,
    otkl               text NOT NULL DEFAULT '',  -- отклонение факт/план
    delay              integer,  -- просрочка доставки, сутки
    date_prib          timestamp,  -- дата прибытия (ст.10 — расчётный DateKon)
    date_prib_d        timestamp,  -- дата прибытия (только дата)
    date_uv_prib       timestamp,  -- дата уведомления о прибытии
    nom_uv_prib        text NOT NULL DEFAULT '',  -- номер уведомления о прибытии
    date_pod           timestamp,  -- дата подачи на фронт
    date_uv_pod        timestamp,  -- дата уведомления о подаче
    nom_uv_pod         text NOT NULL DEFAULT '',  -- номер уведомления о подаче
    date_gu45_pod      timestamp,  -- дата ГУ-45 (памятка) на подачу — уточнить
    nom_gu45_pod       text NOT NULL DEFAULT '',  -- номер ГУ-45 на подачу
    date_pod_gu45      timestamp,  -- дата подачи по ГУ-45 — уточнить
    place_pod          text NOT NULL DEFAULT '',  -- место/фронт подачи
    date_vigr          timestamp,  -- дата-время выгрузки (статус 12)
    date_vigr_d        timestamp,  -- дата выгрузки (ЖД-сутки)
    date_vigr_gu45     timestamp,  -- дата ГУ-45 при выгрузке
    place_vigr         text NOT NULL DEFAULT '',  -- порт выгрузки (статус 12)
    date_ubor          timestamp,  -- дата уборки с фронта
    date_gu45_ubor     timestamp,  -- дата ГУ-45 на уборку — уточнить
    nom_gu45_ubor      text NOT NULL DEFAULT '',  -- номер ГУ-45 на уборку
    date_ubor_gu45     timestamp,  -- дата уборки по ГУ-45 — уточнить
    frost              integer,  -- признак заморозки
    shipments          text NOT NULL DEFAULT '',
    info_1             text NOT NULL DEFAULT '',
    info_2             text NOT NULL DEFAULT '',
    info_3             text NOT NULL DEFAULT '',
    sms_1              text NOT NULL DEFAULT '',
    sms_2              text NOT NULL DEFAULT '',
    sms_3              text NOT NULL DEFAULT '',
    color              text NOT NULL DEFAULT '',
    created_at         timestamp NOT NULL DEFAULT now(),
    updated_at         timestamp NOT NULL DEFAULT now(),
    CONSTRAINT uq_vagon_history_trip_key UNIQUE (trip_key)
);

CREATE INDEX ix_vagon_history_vagon       ON dpport.vagon_history (vagon);
CREATE INDEX ix_vagon_history_date_nach_d ON dpport.vagon_history (date_nach_d);
CREATE INDEX ix_vagon_history_date_prib_d ON dpport.vagon_history (date_prib_d);

-- ─────────────────────────────────────────────────────────────────────────
--  3. vagon_operation — история продвижения в пределах рейса (запрос 601)
--     Хранится только текущий рейс; перезапись = DELETE по trip_key + INSERT.
-- ─────────────────────────────────────────────────────────────────────────
CREATE TABLE dpport.vagon_operation (
    trip_key     bigint     NOT NULL REFERENCES dpport.vagon_history(trip_key) ON DELETE CASCADE,
    date_op      timestamp  NOT NULL,   -- без таймзоны
    kop_vmd      varchar(3),            -- код операции
    stan_op      char(6),               -- код станции (ведущие нули сохранены)
    index_poezd  varchar(15),           -- NULL, если поезда нет («000…0»)
    PRIMARY KEY (trip_key, date_op)
);
